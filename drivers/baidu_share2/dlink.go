package baidu_share

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/drivers/baidu_netdisk"
	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/cache"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/pkg/cookie"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	log "github.com/sirupsen/logrus"
)

// 免转存直链:
// 用账号 BDUSS 做 DLNA 签名,直接调 /share/list?origin=dlna 拿分享文件 dlink,
// 跟一次 302 得 d.pcs.baidu.com 上的最终 CDN 直链。不把文件转存进任何个人账号。
// 最终直链仅凭 DLNA UA 即可播:免 Cookie、Range 完美、不限速、无 100 秒试看限制。
// 失败时返回 error,交由 driver.Link() 回退到转存(save+delete)兜底。
const (
	DLNAUA       = "netdisk;P2SP;2.2.91.136;android-android;"
	baiduDevUID  = "73CED981D0F186D12BC18CAE1684FFD5|VSRCQTF6W"
	baiduChannel = "android_12_zhao_bd-netdisk_1024266h"
	baiduVersion = "11.30.2"
	baiduSaltA   = "ebrcUYiuxaZv2XGu7KIYKxUrqfnOfpDF"
	baiduSaltB   = baiduDevUID + baiduVersion + "ae5821440fab5e1a61a025f014bd8972"

	baiduWebUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"
	// 登录账号 uid(签名要用),从 mbd 接口解析。fields=["uid"] 已 URL 编码。
	baiduUIDURL = "https://mbd.baidu.com/userx/v1/info/get?appname=baiduboxapp&fields=%5B%22uid%22%5D&client&clientfrom&lang=zh-cn&tpl&ttt"
)

var uidRegexp = regexp.MustCompile(`"uid"\s*:\s*"?([0-9]+)"?`)

// baiduUIDCache 按账号 ID 缓存 uid(签名用),避免每次取链都打 mbd 接口。
var baiduUIDCache = cache.NewKeyedCache[string](30 * time.Minute)

func baiduSha1(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// 签名: sha1( sha1(BDUSS) + uid + saltA + time + saltB )。
func baiduDlnaRand(bduss, uid, t string) string {
	return baiduSha1(baiduSha1(bduss) + uid + baiduSaltA + t + baiduSaltB)
}

// baiduDlnaSekey 把 sekey 归一化为「单次 URL 编码」形态,再交由 resty 的 SetQueryParam 编码一次上链。
// d.Token 来源不一:无提取码分享取自 BDCLND cookie(已 URL 编码,含 %),带提取码分享取自 verify 的 randsk
// (原始 base64,字符集 [A-Za-z0-9+/=],不含 %)。/share/list?origin=dlna 期望接收编码后的 sekey。
// 故:已编码(含 %)原样用;原始 randsk 用 QueryEscape 编码一次。两种来源上链后服务端拿到一致的编码 sekey。
// 注意:不能用 QueryUnescape 归一化——它把 randsk 里的字面 '+' 当作空格,破坏 base64 的 '+'。
func baiduDlnaSekey(token string) string {
	if strings.Contains(token, "%") {
		return token
	}
	return url.QueryEscape(token)
}

// fetchBaiduUID 用 web UA + 账号 Cookie 从 mbd 接口取当前登录账号 uid(签名用),按账号缓存。
func fetchBaiduUID(bd *baidu_netdisk.BaiduNetdisk) (string, error) {
	key := fmt.Sprintf("%v", bd.ID)
	if uid, ok := baiduUIDCache.Get(key); ok && uid != "" {
		return uid, nil
	}
	resp, err := base.NoRedirectClient.R().
		SetHeaders(map[string]string{
			"User-Agent": baiduWebUA,
			"Accept":     "application/json, text/plain, */*",
		}).
		SetHeader("Cookie", bd.Cookie).
		Get(baiduUIDURL)
	if err != nil {
		return "", fmt.Errorf("百度原画(无限) uid 请求失败: %w", err)
	}
	m := uidRegexp.FindStringSubmatch(resp.String())
	uid := ""
	if len(m) >= 2 {
		uid = m[1]
	}
	if uid == "" {
		return "", errors.New("百度原画(无限) 未能解析 uid,请检查 Cookie")
	}
	baiduUIDCache.Set(key, uid)
	return uid, nil
}

// pickDlink 深度遍历响应体,找 dlink/downloadurl/download_url/url 字段中的 http 直链。
// 严格模式只认这几个字段;找不到再退化为任意 http 字符串(对齐 JS baiduPickHttpUrl)。
func pickDlink(body []byte) string {
	var v interface{}
	if err := json.Unmarshal(body, &v); err != nil {
		return ""
	}
	if s := walkDlink(v, true); s != "" {
		return s
	}
	return walkDlink(v, false)
}

func walkDlink(node interface{}, strict bool) string {
	isHTTP := func(s string) bool {
		return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
	}
	switch n := node.(type) {
	case []interface{}:
		for _, item := range n {
			if f := walkDlink(item, strict); f != "" {
				return f
			}
		}
	case map[string]interface{}:
		for k, val := range n {
			if s, ok := val.(string); ok {
				lk := strings.ToLower(k)
				nameHit := lk == "dlink" || lk == "downloadurl" || lk == "download_url" || lk == "url"
				if (!strict || nameHit) && isHTTP(s) {
					return s
				}
			}
			if f := walkDlink(val, strict); f != "" {
				return f
			}
		}
	case string:
		if !strict && isHTTP(n) {
			return n
		}
	}
	return ""
}

// followDlnaRedirect 一次性跟随一层 302,把 dlink 换成 d.pcs.baidu.com 上的最终直链。
// 跟不到(非 302 / 无 Location)返回空串,由调用方用原 dlink 兜底(原 dlink 本身也可播)。
func followDlnaRedirect(dlink string) string {
	resp, err := base.NoRedirectClient.R().
		SetHeader("User-Agent", DLNAUA).
		Get(dlink)
	if err != nil {
		return ""
	}
	return resp.Header().Get("Location")
}

// mergeCookies 把响应 Set-Cookie 合并进 cookie 字符串(同名覆盖),用于跨请求保持账号+分享会话。
func mergeCookies(base string, cs []*http.Cookie) string {
	out := base
	for _, c := range cs {
		out = cookie.SetStr(out, c.Name, c.Value)
	}
	return out
}

// fetchFreshSekey 每次取链时,用账号 Cookie 开分享页拿新鲜的 BDCLND(sekey)。
// 关键:必须用账号 Cookie 开页/verify,使 BDCLND 会话与 DLNA 签名所用账号同源
// (参考 cloud-drive.js: baiduOpenSharePage/baiduVerifySharePassword 均带 accountCookie)。
// 带提取码先 /share/verify 建立会话,响应 cookie 合并后带入分享页 GET。
// 返回 BDCLND(URL 编码形态);拿不到返回错误,交由调用方回退 d.Token。
func (d *BaiduShare2) fetchFreshSekey(accountCookie string) (string, error) {
	hdr := accountCookie
	if d.Pwd != "" {
		verifyResp := struct {
			Errno int64 `json:"errno"`
		}{}
		res, err := d.client.R().
			SetHeader("Cookie", hdr).
			SetFormData(map[string]string{"pwd": d.Pwd}).
			SetHeader("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8").
			SetResult(&verifyResp).
			Post("/share/verify?channel=chunlei&clienttype=0&web=1&app_id=250528&surl=" + d.Surl[1:])
		if err != nil {
			return "", fmt.Errorf("分享验证请求失败: %w", err)
		}
		if verifyResp.Errno != 0 {
			return "", fmt.Errorf("分享验证失败(errno=%d): %s", verifyResp.Errno, res.String())
		}
		hdr = mergeCookies(hdr, res.Cookies())
	}
	res, err := d.client.R().SetHeader("Cookie", hdr).Get("/s/" + d.Surl)
	if err != nil {
		return "", fmt.Errorf("分享页请求失败: %w", err)
	}
	hdr = mergeCookies(hdr, res.Cookies())
	if bdclnd := cookie.GetStr(hdr, "BDCLND"); bdclnd != "" {
		return bdclnd, nil
	}
	return "", errors.New("未能获取分享 BDCLND")
}

// resolveShareDirectLink 免转存取链:DLNA 签名接口直接换分享 dlink → 302 到 CDN,不转存。
// 需账号 Cookie 里的 BDUSS + 分享专属 sekey(d.Token)。声明为 var 以便测试替换。
var resolveShareDirectLink = func(d *BaiduShare2, file model.Obj) (*model.Link, error) {
	storage := op.GetFirstDriver("BaiduNetdisk", idx)
	idx++
	if storage == nil {
		return nil, errors.New("找不到百度网盘帐号")
	}
	bd := storage.(*baidu_netdisk.BaiduNetdisk)

	bduss := cookie.GetStr(bd.Cookie, "BDUSS")
	if bduss == "" {
		return nil, errors.New("百度 Cookie 缺少 BDUSS,免转存不可用")
	}
	if d.ShareId == "" || d.ShareUk == "" || d.Token == "" {
		if err := d.Validate(); err != nil {
			return nil, err
		}
	}
	// sekey 优先每次用账号 Cookie 开分享页取新鲜 BDCLND(防过期/形态不一致/会话不同源);
	// 失败回退 d.Token(再经 baiduDlnaSekey 归一化)。
	sekey, serr := d.fetchFreshSekey(bd.Cookie)
	sekeyFresh := serr == nil && sekey != ""
	if !sekeyFresh {
		log.Warnf("获取新鲜 BDCLND 失败,回退 d.Token: %v", serr)
		sekey = d.Token
	}
	uid, err := fetchBaiduUID(bd)
	if err != nil {
		return nil, err
	}
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	rand := baiduDlnaRand(bduss, uid, t)

	// DLNA 请求带「账号 Cookie + BDCLND(sekey)」,与签名账号同源(参考 JS baiduDlnaHeaders 用合并 cookie)。
	dlnaCookie := cookie.SetStr(bd.Cookie, "BDCLND", sekey)
	res, err := d.client.R().
		SetHeader("User-Agent", DLNAUA).
		SetHeader("Accept", "application/json, text/plain, */*").
		SetHeader("Cookie", dlnaCookie).
		SetQueryParams(map[string]string{
			"shareid":    d.ShareId,
			"uk":         d.ShareUk,
			"fid":        file.GetID(),
			"sekey":      baiduDlnaSekey(sekey),
			"origin":     "dlna",
			"devuid":     baiduDevUID,
			"clienttype": "1",
			"channel":    baiduChannel,
			"version":    baiduVersion,
			"time":       t,
			"rand":       rand,
		}).
		Get("/share/list")
	if err != nil {
		return nil, fmt.Errorf("百度原画(无限) share/list 请求失败: %w", err)
	}
	body := res.Body()
	errno := utils.Json.Get(body, "errno").ToInt()
	if errno == 0 {
		errno = utils.Json.Get(body, "error_code").ToInt()
	}
	if errno != 0 {
		msg := utils.Json.Get(body, "show_msg").ToString()
		if msg == "" {
			msg = utils.Json.Get(body, "errmsg").ToString()
		}
		if msg == "" {
			msg = utils.Json.Get(body, "error_msg").ToString()
		}
		if msg == "" {
			msg = strconv.Itoa(errno)
		}
		return nil, fmt.Errorf("百度原画(无限) 请求失败: %s (errno=%d sekey: fresh=%v len=%d encoded=%v)",
			msg, errno, sekeyFresh, len(sekey), strings.Contains(sekey, "%"))
	}
	dlink := pickDlink(body)
	if dlink == "" {
		return nil, errors.New("百度原画(无限) 未返回直链")
	}
	finalURL := followDlnaRedirect(dlink)
	if finalURL == "" {
		finalURL = dlink
	}
	// UA 由 alist-tvbox 对 BAIDU 直接下发 DLNA UA;后端代理则由 link.Header 生效。URL 无需内嵌标记。
	log.Infof("[%v] 百度免转存直链 %v %v %v", bd.ID, file.GetName(), file.GetID(), file.GetSize())
	return &model.Link{
		URL:    finalURL,
		Header: http.Header{"User-Agent": []string{DLNAUA}},
	}, nil
}
