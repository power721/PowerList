package baidu_share

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/drivers/baidu_netdisk"
	"github.com/OpenListTeam/OpenList/v4/internal/cache"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	"github.com/OpenListTeam/OpenList/v4/pkg/cookie"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
)

var idx = 0
var baiduShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)

// 瞬时错误:多为风控或 sekey(BDCLND)过期所致,清 Token 重新 Validate 后重试一次可自愈。
// -21(分享被删除/违规)是永久错误,不在此列。
var baiduTransientErrnos = map[int64]bool{-9: true, -62: true}

func isBaiduTransientErrno(errno int64) bool {
	return baiduTransientErrnos[errno]
}

// 转存频控熔断:-70(转存太快)/-65(操作太快)是账号级长效限流,继续打只会加深限流
// (2026-09-27 线上实证:一轮 strm 刮削风暴约 50 次转存即触发,窗口跨 3 小时周期未放开,
// 风暴内百余次尝试全部无效白发)。命中即进入冷却,冷却期内转存兜底快速失败零请求;
// 免转存主路径不受影响;SaveTo 是用户显式动作,不经此熔断。
var baiduTransferCooldown time.Time

const baiduTransferCooldownDuration = 10 * time.Minute

func isBaiduTransferRateLimitErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "errno=-70") || strings.Contains(msg, "errno=-65")
}

// baiduErrnoMessage 把常见 errno 翻译成可读文案。-21/105 的文案命中 alist-tvbox 的失效分享
// 清理关键字,让真死链能被自动清掉;-9 是混合态:share/list 的 show_msg 是「提取码验证失败」
// (sekey 过期,重验证可自愈),但分享页同一 errno 显示「分享的文件已经被取消了」(线上实证),
// 无法从 errno 单值区分死活,故文案含「提取码验证失败」命中 atv 的会话过期正则归瞬时
// (streak 连击兜底退役),且不含「已取消」等失效字样连续串,防瞬时形态误判死。
// -19/-62/-65 统一翻成带「请稍后」的限流文案:原始 body 的中文 show_msg 是 \uXXXX 转义,
// alist-tvbox 的限流正则匹配不到转义串,翻译后的明文才能被正确归类为限流而非死链。
// 105 = 分享页 404(分享不存在,线上实证 err_msg 恒空)。
func baiduErrnoMessage(errno int64, body string) string {
	switch errno {
	case -21:
		return "分享已取消或因违规无法访问(errno=-21)"
	case 105:
		return "分享不存在或文件已被删除(errno=105)"
	case -9:
		return "分享提取码验证失败,可能已被取消或会话过期(errno=-9)"
	case -19:
		return "访问频率太快,请稍后重试(errno=-19)"
	case -62:
		return "触发百度风控,请稍后重试(errno=-62)"
	case -65:
		return "操作过于频繁,请稍后重试(errno=-65)"
	default:
		return body
	}
}

// baiduAccountCookie 取第一个百度网盘账号的 Cookie。verify/开分享页带上账号 Cookie
// 可显著降低 -62 风控(裸 netdisk UA 从服务器 IP 高频访问极易触发),并使 sekey 与账号同源。
// 声明为 var 便于单测替换(测试里 op 未初始化,GetFirstDriver 缓存未命中落 db 查询会死锁)。
var baiduAccountCookie = func() string {
	storage := op.GetFirstDriver("BaiduNetdisk", 0)
	if storage == nil {
		return ""
	}
	bd, ok := storage.(*baidu_netdisk.BaiduNetdisk)
	if !ok {
		return ""
	}
	return bd.Cookie
}

// baiduShareDirectEnabled 是否启用百度分享免转存(DLNA 签名直链为主、转存兜底)。默认开(对齐
// 夸克/UC 免转存默认):strm 刮削风暴走免转存可消掉全部转存频控压力;关掉则直接走转存。
// 声明为 var 便于单测替换(测试里 op 未初始化,直接 setting.GetBool 会死锁)。
var baiduShareDirectEnabled = func() bool {
	return setting.GetBool(conf.BaiduShareDirect)
}

var resolveBaiduShareLink = func(ctx context.Context, d *BaiduShare2, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	count := op.GetDriverCount("BaiduNetdisk")
	var lastErr error
	for i := 0; i < count; i++ {
		link, err := d.link(ctx, file, args)
		if err == nil {
			return link, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

type BaiduShare2 struct {
	model.Storage
	Addition
	client *resty.Client

	ShareId string
	ShareUk string
	Token   string

	// pageTitle 记录 getInfo 最后一次开页的 <title>,供降级页守卫文案携带:
	// 安全验证=风控 / 请输入提取码=提取码态异常 / 正常标题但无 shareid=页面改版。
	pageTitle string
}

func (d *BaiduShare2) Config() driver.Config {
	return config
}

func (d *BaiduShare2) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *BaiduShare2) Init(ctx context.Context) error {
	// UA 分工(对齐 my.jar 复刻与 web 前端语义):verify/分享页/share list 是 web 端点,默认
	// 浏览器 UA——netdisk UA 开 HTML 页指纹反常,易落风控桶(2026-10-02/03 实证);转存/DLNA
	// 等客户端端点在各自请求上显式覆盖 netdisk/DLNA UA。
	d.client = resty.New().
		SetBaseURL("https://pan.baidu.com").
		SetHeader("User-Agent", baiduWebUA).
		SetHeader("Referer", "https://pan.baidu.com")

	if conf.LazyLoad && !conf.StoragesLoaded {
		return nil
	}

	return d.Validate()
}

func (d *BaiduShare2) Drop(ctx context.Context) error {
	return nil
}

func (d *BaiduShare2) Validate() error {
	if d.Pwd != "" {
		api := "/share/verify?channel=chunlei&clienttype=0&web=1&app_id=250528&surl=" + d.Surl[1:]
		data := map[string]string{
			"pwd": d.Pwd,
		}
		respJson := struct {
			Errno   int64  `json:"errno"`
			Message string `json:"err_msg"`
			Token   string `json:"randsk"`
		}{}
		accountCookie := baiduAccountCookie()
		if accountCookie != "" {
			// 带账号 Cookie 开分享页,降低 -62 风控概率
			res0, err := d.client.R().SetHeader("Cookie", accountCookie).Get("/s/" + d.Surl)
			if err == nil {
				if bdclnd := cookie.GetStr(mergeCookies(accountCookie, res0.Cookies()), "BDCLND"); bdclnd != "" {
					accountCookie = cookie.SetStr(accountCookie, "BDCLND", bdclnd)
				}
			}
		}
		res, err := d.client.R().
			SetFormData(data).
			SetHeader("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8").
			SetHeader("Cookie", accountCookie).
			SetResult(&respJson).
			Post(api)
		if err != nil {
			return err
		}
		if respJson.Errno == -62 {
			// 风控:稍等重试一次
			time.Sleep(2 * time.Second)
			res, err = d.client.R().
				SetFormData(data).
				SetHeader("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8").
				SetHeader("Cookie", accountCookie).
				SetResult(&respJson).
				Post(api)
			if err != nil {
				return err
			}
		}
		log.Debugf("Baidu share verify response: %v", respJson)
		if respJson.Errno != 0 {
			msg := respJson.Message
			if msg == "" {
				msg = res.String()
			}
			return errors.New(baiduErrnoMessage(respJson.Errno, msg))
		}
		d.Token = respJson.Token
		log.Debugf("Baidu Share Token: %v", d.Token)
	}

	return d.getInfo()
}

// baiduDegradedPageRetryDelay 无 shareid 降级页的重开间隔:对齐 verify 对 -62 的 2s 退避
// 先例,降级页多为瞬时风控/边缘缓存。声明为 var 便于单测提速。
var baiduDegradedPageRetryDelay = 2 * time.Second

var baiduTitleRe = regexp.MustCompile(`<title>\s*([^<]*)</title>`)

func baiduPageTitle(page string) string {
	if m := baiduTitleRe.FindStringSubmatch(page); m != nil {
		return m[1]
	}
	return ""
}

// shareDeadTitle 死链页 title 分界:死链 HTTP 仍 200 且 errno 三形态同码,HTML title 是唯一
// 可靠分界(线上实证死链 title「百度网盘-链接不存在」);文案命中 alist-tvbox 的死链与清理
// 关键字,让真死链当场判死。非死链返回空串。
func shareDeadTitle(title string) string {
	if strings.Contains(title, "不存在") || strings.Contains(title, "取消") ||
		strings.Contains(title, "删除") || strings.Contains(title, "过期") || strings.Contains(title, "违规") {
		return fmt.Sprintf("分享不存在或已失效: %s", title)
	}
	return ""
}

// openSharePage 开一次分享页:ua 为空用 client 默认(web),响应 Set-Cookie 的 BDCLND
// 回填 d.Token(sekey 轮换)。
func (d *BaiduShare2) openSharePage(api, hdr, ua string) (string, error) {
	req := d.client.R().SetHeader("Cookie", hdr)
	if ua != "" {
		req = req.SetHeader("User-Agent", ua)
	}
	res, err := req.Get(api)
	if err != nil {
		return "", err
	}
	if BDCLND := cookie.GetCookie(res.Cookies(), "BDCLND"); BDCLND != nil {
		d.Token = BDCLND.Value
	}
	return res.String(), nil
}

func (d *BaiduShare2) getInfo() error {
	api := "/s/" + d.Surl
	// 开页带账号 Cookie 但绝不带 BDCLND(2026-10-02 线上实证回归:带 BDCLND 落到的分享
	// 文件页是 JS 壳,shareid 只作为字段名字符串出现、无任何值,数据靠浏览器 XHR 拉取;
	// 匿名开到的输码页/分享页才嵌有 shareid 值,两种形态:JSON 裸数字 "shareid":123 与
	// JS 字面量 shareid:"123")。降级页(风控墙等,无 shareid)由 web UA 重开兜底。
	hdr := baiduAccountCookie()
	page, err := d.openSharePage(api, hdr, "")
	if err != nil {
		return err
	}
	title := baiduPageTitle(page)
	if msg := shareDeadTitle(title); msg != "" {
		return errors.New(msg)
	}
	// 兼容 quoted/Unquoted 两种嵌法;文件壳页里的字段名列表(如 "share_uk","shareid"])
	// 后随 ']' 不匹配 [:=],不会误命中
	reID := regexp.MustCompile(`shareid"?\s*[:=]\s*"?(\d+)`)
	reUK := regexp.MustCompile(`share_uk"?\s*[:=]\s*"?(\d+)`)
	shareID, shareUk := "", ""
	if m := reID.FindStringSubmatch(page); len(m) >= 2 {
		shareID = m[1]
	}
	if m := reUK.FindStringSubmatch(page); len(m) >= 2 {
		shareUk = m[1]
	}
	if shareID == "" || shareUk == "" {
		// 降级页多为瞬时的边缘缓存/风控:隔 2s 二次开页(显式 web UA,同 client 默认),
		// 别让半初始化态粘滞;取链期的真正兜底是 List 收割(见 List),此处仅 Init 期第一击
		time.Sleep(baiduDegradedPageRetryDelay)
		if page2, err2 := d.openSharePage(api, hdr, baiduWebUA); err2 == nil {
			title = baiduPageTitle(page2)
			if msg := shareDeadTitle(title); msg != "" {
				return errors.New(msg)
			}
			if m := reID.FindStringSubmatch(page2); len(m) >= 2 && shareID == "" {
				shareID = m[1]
			}
			if m := reUK.FindStringSubmatch(page2); len(m) >= 2 && shareUk == "" {
				shareUk = m[1]
			}
		}
	}
	d.pageTitle = title
	if shareID != "" {
		d.ShareId = shareID
		log.Debugf("Share ID: %v", d.ShareId)
	} else {
		// 须带存储上下文与页面标题:匿名告警在数百个聚合分享里无法定位是哪个挂载、撞的哪堵墙
		log.Warnf("[%v] shareid not found on /s/%v (degraded page? title=%q)", d.ID, d.Surl, d.pageTitle)
	}
	if shareUk != "" {
		d.ShareUk = shareUk
		log.Debugf("Share UK: %v", d.ShareUk)
	} else {
		log.Warnf("[%v] share_uk not found on /s/%v (degraded page? title=%q)", d.ID, d.Surl, d.pageTitle)
	}

	log.Debugf("Share Token: %v", d.Token)
	return nil
}

func (d *BaiduShare2) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	if d.Token == "" {
		// 校验失败且仍无 Token(多为 -62 风控拿不到 randsk)时直接透出 verify 的真实错误:
		// 空 sekey 去 /share/list 必吃 -9「提取码验证失败」,白挨一发还把根因文案遮住
		// (2026-09-24 线上实证:4 发 verify 全 -62,最终文案却是 -9)。verify 已拿到 Token
		// 而 getInfo 瞬时失败的形态仍继续列目录(保留旧宽松行为)。
		if verr := d.Validate(); verr != nil && d.Token == "" {
			return nil, verr
		}
	}
	reqDir := dir.GetPath()
	isRoot := "0"
	if reqDir == d.RootFolderPath {
		reqDir = path.Join("/", reqDir)
	}
	if reqDir == "/" {
		isRoot = "1"
		reqDir = ""
	}
	objs := []model.Obj{}
	var err error
	var page = 1
	more := true
	revalidated := false
	for more && err == nil {
		respJson := struct {
			Errno int64 `json:"errno"`
			List  []struct {
				Fsid  json.Number `json:"fs_id"`
				Isdir json.Number `json:"isdir"`
				Path  string      `json:"path"`
				Name  string      `json:"server_filename"`
				Mtime json.Number `json:"server_mtime"`
				Size  json.Number `json:"size"`
			} `json:"list"`
		}{}
		query := map[string]string{
			"app_id":     "250528",
			"channel":    "chunlei",
			"clienttype": "0",
			"desc":       "0",
			"showempty":  "0",
			"web":        "1",
			"view_mode":  "1",
			"num":        "100",
			"order":      "name",
			"root":       isRoot,
			"dir":        reqDir,
			"shorturl":   d.Surl[1:],
			"page":       fmt.Sprint(page),
		}
		log.Debugf("Baidu Share List: %v", page)
		res, e := d.client.R().
			SetCookie(&http.Cookie{Name: "BDCLND", Value: d.Token}).
			SetResult(&respJson).
			SetQueryParams(query).
			Get("/share/list")
		err = e
		log.Debugf("%v result: %v", reqDir, res.String())
		more = false
		if err == nil {
			if res.IsSuccess() && respJson.Errno == 0 {
				page++
				// 收割 share_id/uk(2026-10-03 实测/my.jar 复刻):/share/list 响应顶层即带这对值,
				// 与 HTML 嵌值逐位一致;播放必经目录浏览,收割后取链时 ShareId 必然已就位——降级页/
				// 模板改版/风控墙不再影响取链,HTML 解析(getInfo)降级为 Init 期第一击。
				if d.ShareId == "" {
					if sid := utils.Json.Get(res.Body(), "share_id").ToString(); sid != "" {
						d.ShareId = sid
						log.Debugf("Share ID harvested from /share/list: %v", d.ShareId)
					}
				}
				if d.ShareUk == "" {
					if uk := utils.Json.Get(res.Body(), "uk").ToString(); uk != "" {
						d.ShareUk = uk
						log.Debugf("Share UK harvested from /share/list: %v", d.ShareUk)
					}
				}
				for _, v := range respJson.List {
					size, _ := v.Size.Int64()
					mtime, _ := v.Mtime.Int64()
					objs = append(objs, &model.Object{
						ID:       v.Fsid.String(),
						Path:     v.Path,
						Name:     v.Name,
						Size:     size,
						Modified: time.Unix(mtime, 0),
						IsFolder: v.Isdir.String() == "1",
					})
				}
				if len(respJson.List) >= 100 {
					more = true
				}
			} else {
				listErr := fmt.Errorf("%s", baiduErrnoMessage(respJson.Errno, res.String()))
				// 瞬时错误(-9 sekey 过期/-62 风控):清 Token 重新 Validate 后重试本页一次;
				// 重验证失败时透出 verify 的真实错误(多为 -62 风控,翻译文案带「请稍后」能被
				// alist-tvbox 限流正则正确归类),别用列表的旧 -9 文案遮住根因
				if !revalidated && isBaiduTransientErrno(respJson.Errno) {
					revalidated = true
					d.Token = ""
					verr := d.Validate()
					if verr == nil {
						log.Infof("Baidu share list errno=%d, re-validated token and retrying page %d", respJson.Errno, page)
						more = true
						continue
					}
					listErr = verr
				}
				err = listErr
			}
		}
	}
	return objs, err
}

func (d *BaiduShare2) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	key := file.GetID()
	if link, ok := baiduShareLinkCache.Get(key); ok {
		return link, nil
	}

	// 免转存(原画(无限),DLNA 签名直链)为主、转存(save+delete)兜底,两条路互为补充。
	// 免转存直链不限速、免 Cookie、省空间省等待、零转存零频控;失败时回退转存,保证可用性。
	// 开关默认开(对齐夸克/UC 免转存默认):关掉则回退纯转存(分支前行为)。
	var link *model.Link
	var err error
	if baiduShareDirectEnabled() {
		link, err = resolveShareDirectLink(d, file)
	}
	if err != nil || link == nil {
		if err != nil {
			log.Warnf("百度免转存失败,回退转存: %v", err)
		}
		if time.Now().Before(baiduTransferCooldown) {
			return nil, fmt.Errorf("百度转存频控冷却中(至 %v),请稍后重试(errno=-70)",
				baiduTransferCooldown.Format(time.TimeOnly))
		}
		link, err = resolveBaiduShareLink(ctx, d, file, args)
		if err != nil && isBaiduTransferRateLimitErr(err) {
			baiduTransferCooldown = time.Now().Add(baiduTransferCooldownDuration)
			log.Warnf("百度转存触发频控,转存兜底冷却 %v", baiduTransferCooldownDuration)
		}
	}
	if err == nil && link != nil {
		baiduShareLinkCache.Set(key, link)
	}
	return link, err
}

func (d *BaiduShare2) link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	storage := op.GetFirstDriver("BaiduNetdisk", idx)
	idx++
	if storage == nil {
		return nil, errors.New("找不到百度网盘帐号")
	}
	bd := storage.(*baidu_netdisk.BaiduNetdisk)
	log.Infof("[%v] 获取百度文件直链 %v %v %v", bd.ID, file.GetName(), file.GetID(), file.GetSize())

	if d.Token == "" {
		// 同 List 入口:校验失败且仍无 Token 时直接透出 verify 的真实错误,
		// 别拿空 sekey 去 /share/transfer 白挨一发转存报错
		if verr := d.Validate(); verr != nil && d.Token == "" {
			return nil, verr
		}
	}
	f, err := d.saveFile(file.GetID(), bd)
	if err != nil {
		return nil, err
	}

	go d.delete(f, bd)

	link, err := bd.Link(ctx, f, args)
	log.Debugf("Baidu link: %v %v %v", f.GetID(), f.GetPath(), link)
	return link, err
}

func (d *BaiduShare2) saveFile(fid string, bd *baidu_netdisk.BaiduNetdisk) (model.Obj, error) {
	files, err := d.transferShare(bd, "/"+conf.TempDirName, []string{fid})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("baidu transfer response missing extra.list")
	}
	return files[0], nil
}

// ensureShareIds 半初始化自愈(2026-09-27 线上实证):getInfo 开页吃到降级页时 shareid/share_uk
// 抓空而 Validate 仍成功(列表正常),转存却每发必报 errno=2「参数错误」且无自愈路径——Token
// 不再过期就不会重新 Validate,空 shareid 会粘滞到进程重启。缺参时先重新 Validate 补齐(顺带
// 刷新 sekey),仍缺则透出带页面标题的明确文案(标题即现场分界:安全验证=风控 / 请输入提取码=
// 提取码态异常 / 正常标题但无 shareid=页面改版),别拿空 shareid 打百度换回模糊报错。
func (d *BaiduShare2) ensureShareIds() error {
	if d.ShareId != "" && d.ShareUk != "" {
		return nil
	}
	verr := d.Validate()
	if d.ShareId == "" || d.ShareUk == "" {
		if verr != nil {
			return verr
		}
		title := d.pageTitle
		if title == "" {
			title = "无<title>"
		}
		return fmt.Errorf("分享页未解析到 shareid/share_uk(页面标题:%s),请稍后重试", title)
	}
	return nil
}

// transferShare 调 /share/transfer 把分享对象(fs_id 列表)批量转存到目标账号的指定目录,
// 返回新建对象(fs_id 与落盘路径)。目标目录须已存在(官方接口语义);目录对象由网盘侧整棵递归转存。
func (d *BaiduShare2) transferShare(bd *baidu_netdisk.BaiduNetdisk, dstPath string, ids []string) ([]File, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	// 半初始化守卫:空 shareid 打 /share/transfer 只会换回 errno=2「参数错误」(见 ensureShareIds)
	if err := d.ensureShareIds(); err != nil {
		return nil, err
	}
	Cookie := cookie.SetStr(bd.Cookie, "BDCLND", d.Token)
	// sekey 归一化(baiduDlnaSekey 的反方向:查询参数路径要「原始形态」):带提取码分享的
	// Token 是 verify 返回的原始 base64 randsk(字符集 [A-Za-z0-9+/=],含字面 '+'、不含 '%'),
	// 无提取码分享取自 BDCLND cookie(已 URL 编码,含 %)。SetQueryParams 会对值再编码一次,
	// 故前者原样上链、后者先解码一次;绝不能无条件 QueryUnescape——它把 randsk 里的字面 '+'
	// 当空格吃掉破坏 base64,该分享转存全数 errno=2「参数错误」且无自愈(2026-10-02 红色用例
	// 坐实:ABC+DEF 上链后服务端收到 ABC DEF;约 3/4 的 randsk 抽签含 '+')。
	sekey := d.Token
	if strings.Contains(sekey, "%") {
		if decoded, derr := url.QueryUnescape(sekey); derr == nil {
			sekey = decoded
		}
	}
	data := map[string]string{
		"fsidlist": "[" + strings.Join(ids, ",") + "]",
		"path":     dstPath,
	}
	query := map[string]string{
		"app_id":     "250528",
		"channel":    "chunlei",
		"clienttype": "0",
		"web":        "1",
		"async":      "1",
		"ondup":      "newcopy",
		"shareid":    d.ShareId,
		"from":       d.ShareUk,
		"sekey":      sekey,
	}

	res, err := d.client.R().
		SetFormData(data).
		SetHeader("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8").
		SetHeader("Cookie", Cookie).
		SetHeader("Referer", "https://pan.baidu.com").
		SetHeader("User-Agent", "netdisk").
		SetQueryParams(query).
		Post("/share/transfer")

	if err != nil {
		return nil, err
	}

	if res.IsSuccess() {
		log.Debugf("response: %v", res.String())
	}

	// 错误文案须带 errno:原始 show_msg(如「参数错误」「转存太快,请稍后再试」)不含错误码,
	// 线上排障无法从日志区分根因(2026-09-27 实证 231 发「参数错误」查不到 errno)
	if errno := utils.Json.Get(res.Body(), "errno").ToInt(); errno != 0 {
		msg := utils.Json.Get(res.Body(), "show_msg").ToString()
		if msg == "" {
			msg = fmt.Sprintf("errno=%d", errno)
		} else {
			msg = fmt.Sprintf("%s(errno=%d)", msg, errno)
		}
		return nil, errors.New(msg)
	}

	files := []File{}
	list := utils.Json.Get(res.Body(), "extra", "list")
	for i := 0; i < list.Size(); i++ {
		item := list.Get(i)
		files = append(files, File{
			FileId: item.Get("to_fs_id").ToInt64(),
			Path:   item.Get("to").ToString(),
		})
	}
	return files, nil
}

// SaveTo 把分享对象(文件或目录)服务端转存到百度网盘账号存储的目标目录,实现 driver.ShareSaver 契约。
// 目标账号由 dstStorage 明确指定;一次请求批量转存,不经服务器字节中转。
func (d *BaiduShare2) SaveTo(ctx context.Context, dstStorage driver.Driver, dstDir model.Obj, objs []model.Obj) ([]string, error) {
	bd, ok := dstStorage.(*baidu_netdisk.BaiduNetdisk)
	if !ok {
		return nil, errors.New("目标存储不是百度网盘账号驱动,不支持服务端转存")
	}
	if d.Token == "" {
		if err := d.Validate(); err != nil {
			return nil, err
		}
	}
	ids := make([]string, 0, len(objs))
	for _, obj := range objs {
		ids = append(ids, obj.GetID())
	}
	files, err := d.transferShare(bd, dstDir.GetPath(), ids)
	if err != nil {
		return nil, fmt.Errorf("转存 %d 个对象到 %v 失败: %w", len(objs), dstDir.GetPath(), err)
	}
	saved := make([]string, 0, len(files))
	for _, f := range files {
		saved = append(saved, f.GetID())
	}
	log.Infof("[BaiduShare2] 服务端转存 %d 个对象到 %v(账号 %v)", len(objs), dstDir.GetPath(), bd.ID)
	return saved, nil
}

func (d *BaiduShare2) delete(file model.Obj, bd *baidu_netdisk.BaiduNetdisk) {
	delayTime := setting.GetInt(conf.DeleteDelayTime, 900)
	if delayTime == 0 {
		return
	}

	if delayTime < 5 {
		delayTime = 5
	}

	log.Infof("[%v] Delete Baidu temp file %v after %v seconds.", bd.ID, file.GetID(), delayTime)
	time.Sleep(time.Duration(delayTime) * time.Second)
	bd.Delete(file)
}

func (d *BaiduShare2) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) error {
	return errs.NotSupport
}

func (d *BaiduShare2) Move(ctx context.Context, srcObj, dstDir model.Obj) error {
	return errs.NotSupport
}

func (d *BaiduShare2) Rename(ctx context.Context, srcObj model.Obj, newName string) error {
	return errs.NotSupport
}

func (d *BaiduShare2) Copy(ctx context.Context, srcObj, dstDir model.Obj) error {
	return errs.NotSupport
}

func (d *BaiduShare2) Remove(ctx context.Context, obj model.Obj) error {
	return errs.NotSupport
}

func (d *BaiduShare2) Put(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) error {
	return errs.NotSupport
}

var (
	_ driver.Driver     = (*BaiduShare2)(nil)
	_ driver.ShareSaver = (*BaiduShare2)(nil)
)
