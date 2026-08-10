package quark_uc_share

import (
	"context"
	crand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_123rapid "github.com/OpenListTeam/OpenList/v4/drivers/123_rapid"
	quark "github.com/OpenListTeam/OpenList/v4/drivers/quark_uc"
	"github.com/OpenListTeam/OpenList/v4/drivers/quark_uc_tv"
	"github.com/OpenListTeam/OpenList/v4/internal/cache"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	"github.com/OpenListTeam/OpenList/v4/pkg/cookie"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/go-resty/resty/v2"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	log "github.com/sirupsen/logrus"
)

// 包级共享状态:分享驱动可以有多个实例,Link/List 又被 HTTP 处理器并发调用,
// 所以账号轮询下标用原子量、兜底 Cookie 用读写锁保护,避免数据竞争。
var (
	accountIdx   atomic.Int64 // 网盘账号轮询下标
	tvAccountIdx atomic.Int64 // TV 账号轮询下标

	cookieMu    sync.RWMutex
	shareCookie string // 无账号驱动时的兜底 Cookie,随响应里的 __puus 滚动更新
)

func getShareCookie() string {
	cookieMu.RLock()
	defer cookieMu.RUnlock()
	return shareCookie
}

func setShareCookie(value string) {
	cookieMu.Lock()
	defer cookieMu.Unlock()
	shareCookie = value
}

// updateShareCookiePuus 把响应里刷新的 __puus 写回兜底 Cookie。
func updateShareCookiePuus(value string) {
	cookieMu.Lock()
	defer cookieMu.Unlock()
	shareCookie = cookie.SetStr(shareCookie, "__puus", value)
}

// shareFileID 是 fileToObj 拼出的 "{fid}-{share_fid_token}-{pdir_fid}"。
// 旧代码直接 strings.Split(id, "-") 取 s[1]/s[2],遇到不带 token 的 ID 会 panic。
type shareFileID struct {
	FileID   string
	FidToken string
	ParentID string
}

func parseShareFileID(id string) (shareFileID, error) {
	parts := strings.SplitN(id, "-", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return shareFileID{}, fmt.Errorf("invalid share file id: %q", id)
	}
	parsed := shareFileID{FileID: parts[0], FidToken: parts[1]}
	if len(parts) == 3 {
		parsed.ParentID = parts[2]
	}
	return parsed, nil
}

// shareRequestBinding 把「用哪个账号发请求 / 转存到哪 / 怎么取直链 / 怎么删临时文件」收敛到一个接口,
// 网盘账号(quark_uc)和 TV 账号(quark_uc_tv)各实现一份,转存-取链-删除的主流程因此只保留一份实现。
type shareRequestBinding interface {
	doRequest(d *QuarkUCShare, pathname string, method string, callback base.ReqCallback, resp interface{}) ([]byte, error)
	tempDirID() string
	accountID() uint
	accountLabel(d *QuarkUCShare) string
	getTempFile(ctx context.Context, dirID, fileID string) (model.Obj, error)
	deleteTempFile(ctx context.Context, fileID string) error
	link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error)
	// cookieValue 返回该绑定账号的 Cookie,供走 drive-pc 端点的分享请求使用。
	cookieValue() string
}

type requestBinding struct {
	requestDriver *quark.QuarkOrUC
	cookie        string
	tempDirId     string
}

func bindRequestDriver(uc *quark.QuarkOrUC) requestBinding {
	uc.EnsureTempDir() // 转存前确保临时目录 ID 已初始化(Init 时可能未成功)
	return requestBinding{
		requestDriver: uc,
		cookie:        uc.Cookie,
		tempDirId:     uc.TempDirId,
	}
}

func (b requestBinding) doRequest(d *QuarkUCShare, pathname string, method string, callback base.ReqCallback, resp interface{}) ([]byte, error) {
	if b.requestDriver != nil {
		return b.requestDriver.Request(pathname, method, callback, resp)
	}
	return d.directRequest(b.cookie, pathname, method, callback, resp)
}

func (b requestBinding) tempDirID() string {
	return b.tempDirId
}

func (b requestBinding) accountID() uint {
	if b.requestDriver == nil {
		return 0
	}
	return b.requestDriver.ID
}

func (b requestBinding) accountLabel(d *QuarkUCShare) string {
	return d.getDriverName()
}

func (b requestBinding) getTempFile(_ context.Context, dirID, fileID string) (model.Obj, error) {
	if b.requestDriver == nil {
		return nil, errors.New("no netdisk account bound")
	}
	return b.requestDriver.GetTempFile(dirID, fileID)
}

func (b requestBinding) deleteTempFile(_ context.Context, fileID string) error {
	if b.requestDriver == nil {
		return errors.New("no netdisk account bound")
	}
	var resp PlayResp
	res, err := b.requestDriver.Request("/file/delete", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"action_type":  1,
			"exclude_fids": []string{},
			"filelist":     []string{fileID},
		})
	}, &resp)
	log.Debugf("delete temp file %v: %v", fileID, string(res))
	if err != nil {
		return err
	}
	if resp.Status != 200 {
		return errors.New(resp.Message)
	}
	return nil
}

func (b requestBinding) link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	if b.requestDriver == nil {
		return nil, errors.New("no netdisk account bound")
	}
	return b.requestDriver.Link(ctx, file, args)
}

func (b requestBinding) matches(uc *quark.QuarkOrUC) bool {
	return b.requestDriver == uc
}

func (b requestBinding) cookieValue() string {
	return b.cookie
}

type requestTVBinding struct {
	requestDriver *quark_uc_tv.QuarkUCTV
	cookie        string
	tempDirId     string
	// forceStream: 非 SVIP 账号强制走转码流(streaming),避免原画直链被限速。
	forceStream bool
}

func bindTVRequestDriver(uc *quark_uc_tv.QuarkUCTV) requestTVBinding {
	return requestTVBinding{
		requestDriver: uc,
		cookie:        uc.Cookie,
		tempDirId:     uc.TempDirId,
	}
}

func (b requestTVBinding) doRequest(d *QuarkUCShare, pathname string, method string, callback base.ReqCallback, resp interface{}) ([]byte, error) {
	return d.directRequest(b.cookie, pathname, method, callback, resp)
}

func (b requestTVBinding) tempDirID() string {
	return b.tempDirId
}

func (b requestTVBinding) accountID() uint {
	if b.requestDriver == nil {
		return 0
	}
	return b.requestDriver.ID
}

func (b requestTVBinding) accountLabel(d *QuarkUCShare) string {
	if d.config.Name == "UCShare" {
		return "UCTV"
	}
	return "QuarkTV"
}

func (b requestTVBinding) getTempFile(ctx context.Context, dirID, fileID string) (model.Obj, error) {
	if b.requestDriver == nil {
		return nil, errors.New("no tv account bound")
	}
	return b.requestDriver.GetTempFile(ctx, dirID, fileID)
}

func (b requestTVBinding) deleteTempFile(ctx context.Context, fileID string) error {
	if b.requestDriver == nil {
		return errors.New("no tv account bound")
	}
	var resp PlayResp
	res, err := b.requestDriver.Request(ctx, "/file/delete", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"action_type":  1,
			"exclude_fids": []string{},
			"filelist":     []string{fileID},
		})
	}, &resp)
	log.Debugf("delete tv temp file %v: %v", fileID, string(res))
	if err != nil {
		return err
	}
	if resp.Status != 200 {
		return errors.New(resp.Message)
	}
	return nil
}

func (b requestTVBinding) link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	if b.requestDriver == nil {
		return nil, errors.New("no tv account bound")
	}
	uc := b.requestDriver
	original := uc.Addition.VideoLinkMethod
	if b.forceStream {
		uc.Addition.VideoLinkMethod = "streaming"
	}
	link, err := uc.Link(ctx, file, args)
	method := uc.Addition.VideoLinkMethod
	uc.Addition.VideoLinkMethod = original
	// 转码流交给客户端直连:代理会破坏 m3u8 相对地址。
	if link != nil && method == "streaming" {
		link.URL += "#proxy=0"
	}
	return link, err
}

func (b requestTVBinding) matches(other *requestTVBinding) bool {
	return b.cookie == other.cookie && b.tempDirId == other.tempDirId
}

func (b requestTVBinding) cookieValue() string {
	return b.cookie
}

func (d *QuarkUCShare) getDriverName() string {
	name := "Quark"
	if d.config.Name == "UCShare" {
		name = "UC"
	}
	return name
}

// rapidTo123Enabled 按驱动类型(夸克/UC)选对应的「秒传到123」开关。
func rapidTo123Enabled(d *QuarkUCShare) bool {
	if d == nil {
		return false
	}
	if d.getDriverName() == "UC" {
		return setting.GetBool(conf.UCTo123)
	}
	return setting.GetBool(conf.QuarkTo123)
}

// shareDirectEnabled 按驱动类型(夸克/UC)选对应的「免转存(share-direct)」开关,默认开。
// 关时 Link() 跳过 resolveShareDirectLink 兜底,只用转存/多账号取链。声明为 var 便于单测替换(测试里 op 未初始化,直接 setting.GetBool 会死锁)。
var shareDirectEnabled = func(d *QuarkUCShare) bool {
	if d == nil {
		return true
	}
	if d.getDriverName() == "UC" {
		return setting.GetBool(conf.UCShareDirect)
	}
	return setting.GetBool(conf.QuarkShareDirect)
}

// rapidQuarkUCTo123 按 MD5 把夸克/UC 文件秒传到 123。声明为 var 便于单测替换。
var rapidQuarkUCTo123 = func(name, md5 string, size int64) *model.Link {
	if len(md5) != utils.MD5.Width {
		return nil
	}
	link, err := _123rapid.RapidTo123(context.Background(), _123rapid.Source{
		HashType: utils.MD5,
		Hash:     md5,
		Name:     name,
		Size:     size,
	})
	if err != nil || link == nil {
		log.Infof("[quark-uc-share] rapid to 123 skipped: %v", err)
		return nil
	}
	log.Infof("[quark-uc-share] 使用123秒传直链: %s", name)
	return link
}

func (d *QuarkUCShare) request(pathname string, method string, callback base.ReqCallback, resp interface{}) ([]byte, error) {
	name := d.getDriverName()
	driver := op.GetFirstDriver(name, int(accountIdx.Load()))
	if driver != nil {
		uc := driver.(*quark.QuarkOrUC)
		return uc.Request(pathname, method, callback, resp)
	}

	return d.directRequest(getShareCookie(), pathname, method, callback, resp)
}

func (d *QuarkUCShare) directRequest(cookieStr string, pathname string, method string, callback base.ReqCallback, resp interface{}) ([]byte, error) {
	return d.requestAt(d.conf.api, cookieStr, pathname, method, callback, resp)
}

// pcApi 返回夸克/UC 分享操作的 PC 客户端端点。夸克 sharepage/save、sharepage/token、
// file/download 必须走 drive-pc.quark.cn:drive.quark.cn 的 token 接口宽松(能发 stoken),
// 但 save/download 严格校验 stoken,会返回 "token [st invalid,code:50052]"。
// 对齐不夜 cloud-drive.js quarkApiUrl=drive-pc.quark.cn。UC 的 conf.api 已是 pc-api.uc.cn。
func (d *QuarkUCShare) pcApi() string {
	if d.config.Name == "UCShare" {
		return d.conf.api
	}
	return "https://drive-pc.quark.cn/1/clouddrive"
}

// requestSharePc 用 PC 端点 + binding(账号)Cookie(无 binding 用兜底 Cookie)发分享请求。
func (d *QuarkUCShare) requestSharePc(binding shareRequestBinding, pathname string, method string, callback base.ReqCallback, resp interface{}) ([]byte, error) {
	cookieStr := getShareCookie()
	if binding != nil {
		if c := binding.cookieValue(); c != "" {
			cookieStr = c
		}
	}
	return d.requestAt(d.pcApi(), cookieStr, pathname, method, callback, resp)
}

func (d *QuarkUCShare) requestAt(api, cookieStr, pathname string, method string, callback base.ReqCallback, resp interface{}) ([]byte, error) {
	u := api + pathname
	req := base.RestyClient.R()
	req.SetHeaders(map[string]string{
		"Cookie":     cookieStr,
		"Accept":     "application/json, text/plain, */*",
		"User-Agent": d.conf.ua,
		"Referer":    d.conf.referer,
	})
	req.SetQueryParam("pr", d.conf.pr)
	req.SetQueryParam("entry", "ft")
	req.SetQueryParam("fr", "pc")
	if callback != nil {
		callback(req)
	}
	if resp != nil {
		req.SetResult(resp)
	}
	var e Resp
	req.SetError(&e)
	res, err := req.Execute(method, u)
	if err != nil {
		return nil, err
	}
	// 429 限流: sleep 1s 后重试一次(对齐不夜【官盘】夸克网盘.js)。夜间夸克高峰限流时避免直接报错
	// → 触发免转存↔转存互相兜底(转存任务轮询最多 12s)造成的拖慢。
	if res.StatusCode() == http.StatusTooManyRequests {
		time.Sleep(time.Second)
		res, err = req.Execute(method, u)
		if err != nil {
			return nil, err
		}
	}
	__puus := cookie.GetCookie(res.Cookies(), "__puus")
	if __puus != nil {
		log.Debugf("__puus: %v", __puus)
		updateShareCookiePuus(__puus.Value)
	}
	if e.Status >= 400 || e.Code != 0 {
		return nil, errors.New(e.Message)
	}
	return res.Body(), nil
}

func (d *QuarkUCShare) GetFiles(parent string) ([]File, error) {
	files := make([]File, 0)
	page := 1
	size := 100
	query := map[string]string{
		"pdir_fid":     parent,
		"_size":        strconv.Itoa(size),
		"_fetch_total": "1",
	}
	if d.OrderBy != "none" {
		query["_sort"] = "file_type:asc," + d.OrderBy + ":" + d.OrderDirection
	}
	for {
		query["_page"] = strconv.Itoa(page)
		var resp SortResp
		_, err := d.request("/file/sort", http.MethodGet, func(req *resty.Request) {
			req.SetQueryParams(query)
		}, &resp)
		if err != nil {
			return nil, err
		}
		files = append(files, resp.Data.List...)
		if page*size >= resp.Metadata.Total {
			break
		}
		page++
	}
	return files, nil
}

func (d *QuarkUCShare) Validate() error {
	return d.getShareToken()
}

func (d *QuarkUCShare) getShareToken() error {
	return d.getShareTokenWithBinding(nil)
}

func (d *QuarkUCShare) getShareTokenWithBinding(binding shareRequestBinding) error {
	data := base.Json{
		"pwd_id":             d.ShareId,
		"passcode":           d.SharePwd,
		"share_for_transfer": true,
	}
	var resp ShareTokenResp
	res, err := d.requestSharePc(binding, "/share/sharepage/token", http.MethodPost, func(req *resty.Request) {
		req.SetBody(data)
	}, &resp)
	log.Debugf("getShareToken: %v %v", d.ShareId, string(res))
	if err != nil {
		return err
	}
	if resp.Data.ShareToken == "" {
		if resp.Message != "" {
			return errors.New(resp.Message)
		}
		return errors.New("empty share token")
	}
	// 只在 stoken 真的变了时落库,避免每次刷新都写一次 DB。
	if d.ShareToken != resp.Data.ShareToken {
		d.ShareToken = resp.Data.ShareToken
		op.MustSaveDriverStorage(d)
	}
	log.Debugf("getShareToken: %v %v", d.ShareId, d.ShareToken)
	return nil
}

func (d *QuarkUCShare) requestWithBinding(binding shareRequestBinding, pathname string, method string, callback base.ReqCallback, resp interface{}) ([]byte, error) {
	if binding != nil {
		return binding.doRequest(d, pathname, method, callback, resp)
	}
	return d.request(pathname, method, callback, resp)
}

// savedFileCache 缓存「分享内文件 → 已转存的临时文件对象」。
// 临时文件在 DeleteDelayTime 秒后才会被删,期间重播同一集不必再转存一次
// (省掉 save + task 轮询,约 0.5~12s,也少占一次网盘配额)。
// 缓存的是 quark_uc/quark_uc_tv 的具体文件类型:它们的 Link() 内部有类型断言,不能用自造的 model.Object 替代。
var savedFileCache = cache.NewKeyedCache[model.Obj](30 * time.Minute)

// savedFileTTL 让缓存一定早于临时文件被删除时过期;删得太快(<=30s)则不缓存。
func savedFileTTL() time.Duration {
	delay := setting.GetInt(conf.DeleteDelayTime, 900)
	if delay == 0 { // 0 = 不删除临时文件
		return 30 * time.Minute
	}
	if delay <= 30 {
		return 0
	}
	if ttl := time.Duration(delay-30) * time.Second; ttl < 30*time.Minute {
		return ttl
	}
	return 30 * time.Minute
}

func savedFileKey(d *QuarkUCShare, binding shareRequestBinding, fileID string) string {
	return fmt.Sprintf("%s:%d:%s:%s", d.getDriverName(), binding.accountID(), d.ShareId, fileID)
}

// saveShareFile 转存分享文件到账号临时目录,命中缓存时直接复用上次的转存结果。
// 第二个返回值表示结果来自缓存,调用方取链失败时据此清缓存重试。
func (d *QuarkUCShare) saveShareFile(ctx context.Context, binding shareRequestBinding, id string) (model.Obj, bool, error) {
	parsed, err := parseShareFileID(id)
	if err != nil {
		return nil, false, err
	}
	key := savedFileKey(d, binding, parsed.FileID)
	ttl := savedFileTTL()
	if ttl > 0 {
		if obj, ok := savedFileCache.Get(key); ok {
			log.Debugf("[%v] 复用已转存文件 %v -> %v", binding.accountID(), parsed.FileID, obj.GetID())
			savedFileCache.SetWithTTL(key, obj, ttl)
			return obj, true, nil
		}
	}
	obj, err := d.doSaveShareFile(ctx, binding, parsed)
	if err != nil {
		return nil, false, err
	}
	if ttl > 0 {
		savedFileCache.SetWithTTL(key, obj, ttl)
	}
	return obj, false, nil
}

func (d *QuarkUCShare) doSaveShareFile(ctx context.Context, binding shareRequestBinding, parsed shareFileID) (model.Obj, error) {
	fidToken := parsed.FidToken
	stRefreshed := false
	// 上限 3 次:fid_token 刷新 + stoken 刷新各占一次重试余量。
	for attempt := 0; attempt < 3; attempt++ {
		data := base.Json{
			"fid_list":       []string{parsed.FileID},
			"fid_token_list": []string{fidToken},
			"exclude_fids":   []string{},
			"to_pdir_fid":    binding.tempDirID(),
			"pwd_id":         d.ShareId,
			"stoken":         d.ShareToken,
			"pdir_fid":       "0",
			"pdir_save_all":  false,
			"scene":          "link",
		}
		query := map[string]string{
			"pr":           d.conf.pr,
			"fr":           "pc",
			"uc_param_str": "",
			"__dt":         strconv.Itoa(rand.Int()),
			"__t":          strconv.FormatInt(time.Now().Unix(), 10),
		}
		var resp SaveResp
		res, err := d.requestSharePc(binding, "/share/sharepage/save", http.MethodPost, func(req *resty.Request) {
			req.SetContext(ctx).SetBody(data).SetQueryParams(query)
		}, &resp)
		log.Debugf("saveFile: %v response: %v, error: %v", parsed.FileID, string(res), err)
		if err != nil {
			msg := err.Error()
			log.Warnf("[save-debug] fid=%s fidToken=%s stoken=%q to_pdir=%s err=%s",
				parsed.FileID, fidToken, d.ShareToken, binding.tempDirID(), msg)
			// fid_token 过期:按父目录重新换一个 share_fid_token 再试一次。
			if strings.Contains(msg, "token校验异常") && parsed.ParentID != "" {
				fidToken, err = d.getFileToken(binding, parsed.ParentID, parsed.FileID)
				if err != nil {
					return nil, err
				}
				continue
			}
			// stoken 过期(code:50052 "st invalid"):刷新分享 stoken 后重试一次。
			// stoken 是 share 级、账号无关,刷新后所有账号共享新 stoken。
			if strings.Contains(msg, "st invalid") && !stRefreshed {
				if e := d.getShareTokenWithBinding(binding); e == nil {
					stRefreshed = true
					log.Infof("[save] stoken 失效(50052),已刷新重试 %s", parsed.FileID)
					continue
				}
			}
			log.Warnf("save file failed: %v", err)
			return nil, err
		}
		if resp.Status != 200 {
			return nil, errors.New(resp.Message)
		}
		log.Debugf("save file task id: %v", resp.Data.TaskId)
		newFileID, dirID, err := d.getSaveTaskResult(ctx, binding, resp.Data.TaskId)
		if err != nil {
			return nil, err
		}
		log.Debugf("new file id: %v dirId: %v", newFileID, dirID)
		file, err := binding.getTempFile(ctx, dirID, newFileID)
		if err != nil {
			log.Warnf("get temp file failed: %v", err)
			return nil, err
		}
		log.Debugf("new file: %+v", file)
		return file, nil
	}
	return nil, errors.New("save file failed")
}

func (d *QuarkUCShare) getSaveTaskResult(ctx context.Context, binding shareRequestBinding, taskId string) (string, string, error) {
	const (
		firstDelay = 200 * time.Millisecond
		maxDelay   = time.Second
		budget     = 12 * time.Second
	)
	deadline := time.Now().Add(budget)
	delay := firstDelay
	for retry := 1; ; retry++ {
		if err := sleepCtx(ctx, delay); err != nil {
			return "", "", err
		}
		query := map[string]string{
			"pr":           d.conf.pr,
			"fr":           "pc",
			"uc_param_str": "",
			"retry_index":  strconv.Itoa(retry),
			"task_id":      taskId,
			"__dt":         strconv.Itoa(rand.Int()),
			"__t":          strconv.FormatInt(time.Now().Unix(), 10),
		}
		var resp SaveTaskResp
		res, err := d.requestSharePc(binding, "/task", http.MethodGet, func(req *resty.Request) {
			req.SetContext(ctx).SetQueryParams(query)
		}, &resp)
		log.Debugf("getSaveTaskResult: %v %v", taskId, string(res))
		if err != nil {
			log.Warnf("get save task result failed: %v", err)
			return "", "", err
		}
		if resp.Status != 200 {
			return "", "", errors.New(resp.Message)
		}
		if len(resp.Data.SaveAs.Fid) > 0 {
			return resp.Data.SaveAs.Fid[0], resp.Data.SaveAs.DirId, nil
		}
		if time.Now().After(deadline) {
			return "", "", errors.New("get task result timeout")
		}
		if delay < maxDelay {
			delay += firstDelay
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// saveAndLink 转存(或复用上次转存)后取直链。复用的临时文件已被删掉时清缓存重转存一次。
func (d *QuarkUCShare) saveAndLink(ctx context.Context, binding shareRequestBinding, id string, args model.LinkArgs) (*model.Link, error) {
	var lastErr error
	for range 2 {
		saveStart := time.Now()
		file, reused, err := d.saveShareFile(ctx, binding, id)
		saveElapsed := time.Since(saveStart)
		if err != nil {
			return nil, err
		}
		log.Debugf("[saveAndLink] 账号 %d fid=%s 转存 耗时=%v reused=%v", binding.accountID(), file.GetID(), saveElapsed, reused)
		key := ""
		if parsed, perr := parseShareFileID(id); perr == nil {
			key = savedFileKey(d, binding, parsed.FileID)
		}
		d.scheduleTempFileDelete(binding, file.GetID(), key)
		// 网盘转存的文件走 speedup token 提速直链(dl-c 提速通道,实测 8x);
		// TV 账号或 speedup 失败时回退 binding.link 的普通直链。
		var link *model.Link
		linkStart := time.Now()
		if rb, ok := binding.(requestBinding); ok {
			link, err = d.speedupLink(ctx, rb, file, args)
		} else {
			link, err = binding.link(ctx, file, args)
		}
		log.Debugf("[saveAndLink] 账号 %d fid=%s 取链 耗时=%v", binding.accountID(), file.GetID(), time.Since(linkStart))
		if err == nil {
			return link, nil
		}
		lastErr = err
		if !reused {
			return nil, err
		}
		log.Debugf("复用的临时文件取链失败,重新转存: %v", err)
		if key != "" {
			savedFileCache.Delete(key)
		}
	}
	return nil, lastErr
}

// speedup token:把转存后的文件经「夸克聊天会话」换一个下载提速 token,
// 再带进 file/download 拿 dl-c 提速通道直链(实测 ~14MB/s vs 普通 ~1.7MB/s)。
// 链路: batch_send(文件名+转存fid) → store_msg_id → acquire_dl_token → data.token。
const speedupConversation = "300000003429402383"

type speedupEntry struct {
	token  string
	expire int64 // unix ms
}

var speedupCache sync.Map // savedFid -> speedupEntry

func genUUID() string {
	b := make([]byte, 16)
	_, _ = crand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func jmap(m map[string]interface{}, key string) map[string]interface{} {
	if v, ok := m[key]; ok {
		if mm, ok := v.(map[string]interface{}); ok {
			return mm
		}
	}
	return nil
}

func jstr(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// socialRequest 发到 drive-social-api.quark.cn(聊天提速接口),走 PC 客户端身份。
func truncStr(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8] + "..."
	}
	return s
}

func urlHost(u string) string {
	if i := strings.Index(u, "://"); i >= 0 {
		rest := u[i+3:]
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			return rest[:j]
		}
		return rest
	}
	return u
}

func (d *QuarkUCShare) socialRequest(cookie, pathname, body string) (map[string]interface{}, error) {
	u := "https://drive-social-api.quark.cn/1/clouddrive" + pathname + "?pr=ucpro&fr=pc&sys=win32&ve=3.15.0"
	req := base.RestyClient.R()
	req.SetHeaders(map[string]string{
		"Cookie":       cookie,
		"User-Agent":   d.conf.ua,
		"Referer":      d.conf.referer + "/",
		"Content-Type": "application/json; charset=utf-8",
	})
	req.SetBody(body)
	var resp map[string]interface{}
	req.SetResult(&resp)
	var e Resp
	req.SetError(&e)
	res, err := req.Execute(http.MethodPost, u)
	if err != nil {
		log.Warnf("[speedup] %s 请求失败: %v", pathname, err)
		return nil, err
	}
	log.Debugf("[speedup] %s http=%d body=%s", pathname, res.StatusCode(), truncStr(string(res.Body()), 400))
	if e.Code != 0 {
		log.Warnf("[speedup] %s 业务失败 code=%d msg=%s", pathname, e.Code, e.Message)
		return nil, errors.New(e.Message)
	}
	return resp, nil
}

// getSpeedupToken 取(并缓存)某个转存文件的下载提速 token。
func (d *QuarkUCShare) getSpeedupToken(cookie, savedFid, fileName string) (string, error) {
	if v, ok := speedupCache.Load(savedFid); ok {
		if e := v.(speedupEntry); e.token != "" && time.Now().UnixMilli() < e.expire-60000 {
			log.Infof("[speedup] 命中缓存 token=%s fid=%s", short(e.token), savedFid)
			return e.token, nil
		}
	}
	log.Infof("[speedup] 开始取 token: fid=%s name=%s", savedFid, fileName)
	if cookie == "" {
		log.Warnf("[speedup] cookie 为空,无法走聊天会话提速(需登录态)")
		return "", errors.New("speedup: empty cookie")
	}
	fname, _ := json.Marshal(fileName)
	body1 := fmt.Sprintf(`{"conversations":[{"conversation_id":%q,"conversation_type":3,"file_list":[{"client_extra":{"device_model":"TVBOX","group_id":%q,"local_msg_id":%q},"content":%s,"fid":%q}],"merge_file":0}],"return_msg_as_list":1}`,
		speedupConversation, genUUID(), genUUID(), string(fname), savedFid)
	r1, err := d.socialRequest(cookie, "/chat/conv/msg/batch_send", body1)
	if err != nil {
		return "", err
	}
	storeMsgID := ""
	if data := jmap(r1, "data"); data != nil {
		if arr, ok := data["send_msg_list"].([]interface{}); ok && len(arr) > 0 {
			if item, ok := arr[0].(map[string]interface{}); ok {
				storeMsgID = jstr(item, "store_msg_id")
			}
		}
	}
	if storeMsgID == "" {
		log.Warnf("[speedup] batch_send 未返回 store_msg_id,resp=%s", truncStr(fmt.Sprint(r1), 300))
		return "", errors.New("speedup: empty store_msg_id")
	}
	log.Infof("[speedup] batch_send ok store_msg_id=%s", storeMsgID)
	body2 := fmt.Sprintf(`{"conversation_id":%q,"conversation_type":3,"msg_id":%q}`, speedupConversation, storeMsgID)
	r2, err := d.socialRequest(cookie, "/chat/conv/file/acquire_dl_token", body2)
	if err != nil {
		return "", err
	}
	data := jmap(r2, "data")
	token := jstr(data, "token")
	if token == "" {
		log.Warnf("[speedup] acquire_dl_token 未返回 token,resp=%s", truncStr(fmt.Sprint(r2), 300))
		return "", errors.New("speedup: empty token")
	}
	var expire int64
	if data != nil {
		if f, ok := data["expired_timestamp"].(float64); ok {
			expire = int64(f)
		}
	}
	if expire == 0 {
		expire = time.Now().Add(30 * time.Minute).UnixMilli()
	}
	speedupCache.Store(savedFid, speedupEntry{token: token, expire: expire})
	log.Infof("[speedup] acquire_dl_token ok token=%s expire=%d", short(token), expire)
	return token, nil
}

// speedupDownload 走 drive-pc(PC 客户端)端点取带 token 的提速直链。
// 关键:必须用 drive-pc.quark.cn;quark_uc 默认的 drive.quark.cn 对带 speedup token 的请求
// 会返回 "download file size limit",只有 PC 客户端端点接受 speedup token(my.jar 也走 drive-pc)。
func (d *QuarkUCShare) speedupDownload(cookie, savedFid, token string) (string, error) {
	u := "https://drive-pc.quark.cn/1/clouddrive/file/download?pr=ucpro&fr=pc"
	req := base.RestyClient.R()
	req.SetHeaders(map[string]string{
		"Cookie":       cookie,
		"User-Agent":   d.conf.ua,
		"Referer":      d.conf.referer + "/",
		"Content-Type": "application/json",
	})
	req.SetBody(base.Json{"fids": []string{savedFid}, "token": token})
	var resp DownResp
	req.SetResult(&resp)
	var e Resp
	req.SetError(&e)
	res, err := req.Execute(http.MethodPost, u)
	if err != nil {
		return "", err
	}
	log.Debugf("[speedup] file/download(drive-pc) http=%d body=%s", res.StatusCode(), truncStr(string(res.Body()), 300))
	if e.Code != 0 {
		return "", errors.New(e.Message)
	}
	if len(resp.Data) == 0 || resp.Data[0].DownloadUrl == "" {
		return "", errors.New("empty speedup download url")
	}
	return resp.Data[0].DownloadUrl, nil
}

// speedupLink 带提速 token 取直链;任何一步失败回退 binding.link 的普通直链。
func (d *QuarkUCShare) speedupLink(ctx context.Context, rb requestBinding, savedFile model.Obj, args model.LinkArgs) (*model.Link, error) {
	// dl-c 提速是非会员通道:非会员的普通直链(dl-pc)被限速 ~1MB/s,speedup 换 dl-c(~14MB/s);
	// SVIP 的普通直链本就不限速,speedup 对其只回 dl-pc,故跳过省一次聊天会话消息(对齐 my.jar r(isSvip))。
	if rb.requestDriver != nil && rb.requestDriver.VIP {
		log.Infof("[speedup] SVIP 账号,普通直链不限速,跳过 speedup %s", savedFile.GetName())
		return rb.link(ctx, savedFile, args)
	}
	savedFid := savedFile.GetID()
	token, err := d.getSpeedupToken(rb.cookie, savedFid, savedFile.GetName())
	if err != nil {
		log.Warnf("[speedup] token 获取失败,回退普通直链: %v", err)
		return rb.link(ctx, savedFile, args)
	}
	downloadUrl, err := d.speedupDownload(rb.cookie, savedFid, token)
	if err != nil {
		log.Warnf("[speedup] 带 token 的 file/download 失败,回退普通直链: %v", err)
		return rb.link(ctx, savedFile, args)
	}
	host := urlHost(downloadUrl)
	// dl-c-* = 提速通道; dl-pc-* = 普通限速通道。
	log.Infof("[speedup] 提速直链 host=%s (dl-c=加速/dl-pc=普通) fid=%s", host, savedFid)
	uc := rb.requestDriver
	return &model.Link{
		URL: downloadUrl,
		Header: http.Header{
			"Cookie":     []string{rb.cookie},
			"Referer":    []string{d.conf.referer},
			"User-Agent": []string{d.conf.ua},
		},
		Concurrency: uc.Concurrency,
		PartSize:    uc.ChunkSize * utils.KB,
	}, nil
}

type pendingDelete struct {
	deadline atomic.Int64 // unix nano
	cacheKey string
}

// pendingDeletes: "<账号ID>:<临时文件ID>" -> *pendingDelete
var pendingDeletes sync.Map

// scheduleTempFileDelete 延迟删除转存出来的临时文件。
// 同一个临时文件只保留一个等待中的 goroutine,重复取链只把删除时间往后推:
// 既避免长时间播放中途文件被删,也避免旧实现里「每次取链都 go 一个睡 900s 的 goroutine」的堆积。
func (d *QuarkUCShare) scheduleTempFileDelete(binding shareRequestBinding, fileID, cacheKey string) {
	delaySec := setting.GetInt(conf.DeleteDelayTime, 900)
	if delaySec == 0 {
		return
	}
	if delaySec < 5 {
		delaySec = 5
	}
	deadline := time.Now().Add(time.Duration(delaySec) * time.Second).UnixNano()
	key := fmt.Sprintf("%d:%s", binding.accountID(), fileID)
	entry := &pendingDelete{cacheKey: cacheKey}
	entry.deadline.Store(deadline)
	if existing, loaded := pendingDeletes.LoadOrStore(key, entry); loaded {
		existing.(*pendingDelete).deadline.Store(deadline)
		return
	}

	label := binding.accountLabel(d)
	log.Infof("[%v] Delete %s temp file %v after %v seconds.", binding.accountID(), label, fileID, delaySec)
	go func() {
		for {
			wait := time.Until(time.Unix(0, entry.deadline.Load()))
			if wait <= 0 {
				break
			}
			time.Sleep(wait)
		}
		pendingDeletes.Delete(key)
		if entry.cacheKey != "" {
			savedFileCache.Delete(entry.cacheKey)
		}
		log.Infof("[%v] Delete %s temp file: %v", binding.accountID(), label, fileID)
		// 用 Background:请求的 ctx 早就随响应结束被取消了。
		if err := binding.deleteTempFile(context.Background(), fileID); err != nil {
			log.Warnf("[%v] Delete %s temp file failed: %v %v", binding.accountID(), label, fileID, err)
		}
	}()
}

func (d *QuarkUCShare) getShareFiles(id string) ([]File, error) {
	return d.getShareFilesWithBinding(nil, id)
}

func (d *QuarkUCShare) getShareFilesWithBinding(binding shareRequestBinding, id string) ([]File, error) {
	log.Debugf("getShareFiles: %v", id)
	s := strings.Split(id, "-")
	fileId := s[0]
	files := make([]File, 0)
	page := 1
	for {
		query := map[string]string{
			"pr":            d.conf.pr,
			"fr":            "pc",
			"pwd_id":        d.ShareId,
			"stoken":        d.ShareToken,
			"pdir_fid":      fileId,
			"force":         "0",
			"_page":         strconv.Itoa(page),
			"_size":         "50",
			"_fetch_banner": "0",
			"_fetch_share":  "0",
			"_fetch_total":  "1",
			"_sort":         "file_type:asc," + d.OrderBy + ":" + d.OrderDirection,
		}
		log.Debugf("getShareFiles query: %v", query)
		var resp ListResp
		res, err := d.requestSharePc(binding, "/share/sharepage/detail", http.MethodGet, func(req *resty.Request) {
			req.SetQueryParams(query)
		}, &resp)
		name := d.getDriverName()
		log.Debugf("%s share get files: %s", name, string(res))
		if err != nil {
			if err.Error() == "分享的stoken过期" {
				if err := d.getShareTokenWithBinding(binding); err != nil {
					return nil, err
				}
				return d.getShareFilesWithBinding(binding, id)
			}
			return nil, err
		}
		if resp.Message == "ok" {
			files = append(files, resp.Data.Files...)
			if len(files) >= resp.Metadata.Total {
				break
			}
			page++
		} else {
			if resp.Message == "分享的stoken过期" {
				if err := d.getShareTokenWithBinding(binding); err != nil {
					return nil, err
				}
				return d.getShareFilesWithBinding(binding, id)
			}
			return nil, errors.New(resp.Message)
		}
	}

	return files, nil
}

func (d *QuarkUCShare) getFileToken(binding shareRequestBinding, pid, fid string) (string, error) {
	files, err := d.getShareFilesWithBinding(binding, pid)
	if err != nil {
		return "", err
	}
	for _, f := range files {
		if f.ID == fid {
			return f.FID, nil
		}
	}
	return "", errors.New("file not found")
}

// accountIsSVIP 判断当前驱动类型的主账号(master)是否为 SVIP(SUPER_VIP),用于路由原画 vs 免转存。
// 直接走 GetMasterDriver(name, prefix, 0) 取主账号:不受 DriverRoundRobin 开关影响
// (GetFirstDriver 在轮询模式下 prefix 传空,会跳过 master 查询、退回到 drivers[0])。
// 主账号才是 save+下载原画路径实际使用的账号。无主账号返回 false。
// quark.QuarkOrUC.VIP 在账号 Init() 时由 getVipInfo() 按 member_type 含 "SUPER_VIP" 置位。
// 声明为 var 以便测试替换(避免单测里 op 未初始化导致死锁)。
// accountIsSVIP 判断当前驱动类型主账号是否为 SVIP(SUPER_VIP):有主账号且 SVIP 返回 true。
// 路由:有 SVIP 主账号 → 转存(save+download 原画,全速,超大文件/ISO 可靠);否则 → 免转存(share-direct)。
// quark.QuarkOrUC.VIP 在账号 Init() 时由 getVipInfo() 按 member_type 含 "SUPER_VIP" 置位。
// 声明为 var 以便测试替换(避免单测里 op 未初始化导致 GetMasterDriver 死锁)。
var accountIsSVIP = func(d *QuarkUCShare) bool {
	name := d.getDriverName()
	prefix := conf.UC
	if name == "Quark" {
		prefix = conf.QUARK
	}
	storage := op.GetMasterDriver(name, prefix, 0)
	if storage == nil {
		return false
	}
	uc, ok := storage.(*quark.QuarkOrUC)
	return ok && uc.VIP
}

// multiSourceEnabled 多账号分片并行下载总开关。声明为 var 便于单测替换。
var multiSourceEnabled = func(d *QuarkUCShare) bool {
	return setting.GetBool(conf.QuarkMultiAccountProxy)
}

// multiSourceMax 取同时使用的最大账号数,0=不限。默认 3。
func multiSourceMax() int {
	m := setting.GetInt(conf.QuarkMultiAccountMax, 3)
	if m < 0 {
		m = 0
	}
	return m
}

// collectMultiAccountLinks 用 goroutine 对每个网盘账号并发 saveAndLink 取链。
// 关键:必须用 op.GetStorages 遍历每个账号再各自 saveAndLink —— 不能用 d.link(),
// 因为 d.link() 内部 GetFirstDriver 在 DriverRoundRobin 关闭时总返回 master 账号,
// 并发调用只会取到同一条链。
// 启播策略:所有账号同时跑,第一个成功后只额外等 firstSourceGrace(2s)收集更多源就返回,
// 不等最慢的账号 —— 这样启播≈首源时间+2s,慢账号不拖垮启播。总数受 multiSourceMax() 限制。
// 声明为 var 便于单测替换(避免单测里 op 未初始化)。
var collectMultiAccountLinks = func(ctx context.Context, d *QuarkUCShare, file model.Obj, args model.LinkArgs) []*model.Link {
	collectStart := time.Now()
	name := d.getDriverName()
	storages := op.GetStorages(name)
	if len(storages) < 2 {
		return nil
	}
	max := multiSourceMax()
	n := len(storages)
	if max > 0 && n > max {
		n = max
	}
	if n < len(storages) {
		storages = storages[:n]
	}

	collectCtx, cancel := context.WithTimeout(ctx, multiSourceCollectTimeout())
	defer cancel()

	results := make(chan *model.Link, len(storages))
	var wg sync.WaitGroup
	for _, st := range storages {
		uc, ok := st.(*quark.QuarkOrUC)
		if !ok {
			continue
		}
		wg.Add(1)
		go func(uc *quark.QuarkOrUC) {
			defer wg.Done()
			if collectCtx.Err() != nil {
				return
			}
			acctStart := time.Now()
			binding := bindRequestDriver(uc)
			l, e := d.saveAndLink(collectCtx, binding, file.GetID(), args)
			elapsed := time.Since(acctStart)
			if e == nil && l != nil && l.URL != "" {
				log.Infof("[multi-source] 账号 %d 取链完成 耗时=%v", uc.ID, elapsed)
				select {
				case results <- l:
				case <-collectCtx.Done():
				}
			} else {
				log.Warnf("[multi-source] 账号 %d 取链失败 耗时=%v err=%v", uc.ID, elapsed, e)
			}
		}(uc)
	}
	go func() { wg.Wait(); close(results) }()

	// 首个成功后,额外等 firstSourceGrace 收集更多源;之后或全部完成或总超时,立即返回。
	var links []*model.Link
	afterFirst := time.NewTimer(multiSourceCollectTimeout()) // 占位,首源前不会触发
	afterFirst.Stop()
	firstDone := false
	for {
		select {
		case l, ok := <-results:
			if !ok {
				log.Infof("[multi-source] collect: driver=%s accounts=%d collected=%d 总耗时=%v(全部结束)", name, len(storages), len(links), time.Since(collectStart))
				return links
			}
			links = append(links, l)
			if !firstDone {
				firstDone = true
				afterFirst.Reset(multiSourceFirstGrace())
			}
		case <-afterFirst.C:
			log.Infof("[multi-source] collect: driver=%s accounts=%d collected=%d 总耗时=%v(首源+%v窗口)", name, len(storages), len(links), time.Since(collectStart), multiSourceFirstGrace())
			cancel() // 停止仍在跑的慢账号
			return links
		case <-collectCtx.Done():
			log.Infof("[multi-source] collect: driver=%s accounts=%d collected=%d 总耗时=%v(超时)", name, len(storages), len(links), time.Since(collectStart))
			return links
		}
	}
}

// multiSourceCollectTimeout 多账号取链的总超时,避免启播卡死。默认 8s。
func multiSourceCollectTimeout() time.Duration {
	return 8 * time.Second
}

// multiSourceFirstGrace 首个账号取链成功后,额外等待收集更多源的窗口。默认 2s。
func multiSourceFirstGrace() time.Duration {
	return 2 * time.Second
}

// sourceFromLink 从单账号取链结果构造 LinkSource:去掉 #x-referer=raw 片段标记,
// 保留 URL + Header(后端代理直接用 link.Header,不走魔法 Referer 片段契约)。
func sourceFromLink(l *model.Link, d *QuarkUCShare) model.LinkSource {
	u := l.URL
	// #x-referer=raw 是给客户端代理的片段标记(见 share-direct-referer-marker 契约),
	// 后端代理直接用 link.Header,片段发往 CDN 也是无害但无意义,去掉更干净。
	if i := strings.Index(u, "#"); i >= 0 {
		u = u[:i]
	}
	return model.LinkSource{URL: u, Header: l.Header}
}

// sourcesFromLink 把多个账号取链结果转成去重的 LinkSource 列表(供 link.MultiSource)。
func sourcesFromLinks(links []*model.Link, d *QuarkUCShare) []model.LinkSource {
	srcs := make([]model.LinkSource, 0, len(links))
	seen := map[string]bool{}
	for _, l := range links {
		if l == nil {
			continue
		}
		s := sourceFromLink(l, d)
		if seen[s.URL] {
			continue
		}
		seen[s.URL] = true
		srcs = append(srcs, s)
	}
	return srcs
}

// masterCookie 取当前驱动类型主账号(master)的 Cookie。
// 夸克 share /file/download 需账号上下文(参考脚本 quarkRequestShareDownload 用 drive.fetch 带账号),
// 匿名请求会失败 → 回退转存。故夸克取链需带主账号 Cookie;UC 匿名即可。无账号返回 ""。
func (d *QuarkUCShare) masterCookie() string {
	name := d.getDriverName()
	prefix := conf.UC
	if name == "Quark" {
		prefix = conf.QUARK
	}
	storage := op.GetMasterDriver(name, prefix, 0)
	if storage == nil {
		return ""
	}
	uc, ok := storage.(*quark.QuarkOrUC)
	if !ok {
		return ""
	}
	return uc.Cookie
}

// resolveShareDirectLink 免转存取链:直接用分享凭据调 /file/download 换直链,
// 不把文件 save 到任何个人账号。匿名(仅 stoken),无账号也可用。
// 失败时由 Link() 上层回退到 save+delete。声明为 var 以便测试替换(同 resolveQuarkUCShareLink)。
var resolveShareDirectLink = func(d *QuarkUCShare, file model.Obj) (*model.Link, error) {
	// 文件 ID 形如 {fid}-{share_fid_token}-{pdir_fid}(见 fileToObj)。
	parts := strings.SplitN(file.GetID(), "-", 3)
	if len(parts) < 2 {
		return nil, errors.New("invalid share file id: " + file.GetID())
	}
	fileId, fidToken := parts[0], parts[1]
	pid := ""
	if len(parts) >= 3 {
		pid = parts[2]
	}
	if d.ShareToken == "" {
		if err := d.getShareToken(); err != nil {
			return nil, err
		}
	}
	// 取链请求 Cookie:UC 走匿名(已验证可行);夸克 share /file/download 需账号上下文,走主账号 Cookie。
	isUC := d.getDriverName() == "UC"
	reqCookie := ""
	if !isUC {
		reqCookie = d.masterCookie()
	}
	body := base.Json{
		"fids":            []string{fileId},
		"fids_token":      []string{fidToken},
		"pwd_id":          d.ShareId,
		"stoken":          d.ShareToken,
		"speedup_session": "",
	}
	var resp DownResp
	_, err := d.requestAt(d.pcApi(), reqCookie, "/file/download", http.MethodPost, func(req *resty.Request) {
		req.SetBody(body)
	}, &resp)
	// fid_token 失效时,按 pid 重新换取 share_fid_token 后重试一次(复用 saveFile 的回退策略)。
	if err != nil && strings.Contains(err.Error(), "token校验异常") && pid != "" {
		if newToken, e := d.getFileToken(nil, pid, fileId); e == nil && newToken != "" {
			body["fids_token"] = []string{newToken}
			_, err = d.requestAt(d.pcApi(), reqCookie, "/file/download", http.MethodPost, func(req *resty.Request) {
				req.SetBody(body)
			}, &resp)
		}
	}
	// stoken 失效(code:50052 "st invalid"):刷新分享 stoken 后重试一次。
	// 夸克夜间 stoken 易过期,刷新一次可救回,避免免转存兜底整体失败。
	if err != nil && strings.Contains(err.Error(), "st invalid") {
		if e := d.getShareToken(); e == nil {
			body["stoken"] = d.ShareToken
			log.Infof("[share-direct] stoken 失效(50052),已刷新重试 %s", fileId)
			_, err = d.requestAt(d.pcApi(), reqCookie, "/file/download", http.MethodPost, func(req *resty.Request) {
				req.SetBody(body)
			}, &resp)
		}
	}
	// 空直链自愈: 强制刷新 stoken + 按 pid 重换 fid_token 后再试一次(对齐不夜 cloud-drive.js:4906-4923)。
	// 夸克夜间 stoken/fid_token 易临时失效,刷新一次可救回,避免落入转存 12s 轮询兜底。
	if err == nil && (len(resp.Data) == 0 || resp.Data[0].DownloadUrl == "") && pid != "" {
		if te := d.getShareToken(); te == nil {
			if newToken, e := d.getFileToken(nil, pid, fileId); e == nil && newToken != "" {
				body["fids_token"] = []string{newToken}
				body["stoken"] = d.ShareToken
				_, err = d.requestAt(d.pcApi(), reqCookie, "/file/download", http.MethodPost, func(req *resty.Request) {
					req.SetBody(body)
				}, &resp)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 || resp.Data[0].DownloadUrl == "" {
		return nil, errors.New("empty share download url")
	}
	downloadUrl := resp.Data[0].DownloadUrl
	// 123 优先:开关开启且响应带回 md5 时,按 MD5 秒传到 123;失败/未命中回退下面的免转存直链。
	if rapidTo123Enabled(d) {
		md5 := ""
		if len(resp.Data) > 0 {
			md5 = resp.Data[0].Md5
		}
		if link := rapidQuarkUCTo123(file.GetName(), md5, file.GetSize()); link != nil {
			return link, nil
		}
	}
	// 夸克 + 有主账号: alist-tvbox 服务器代理会注入 link.Header,用账号 Cookie + 正常 Referer 最稳,
	// 夜间夸克收紧 checkplay 也不被拦(对齐不夜 cloud-drive.js:4683-4696,以及自家 quark_uc.getDownloadLink
	// 的 drivers/quark_uc/util.go:155-159)。不再依赖魔法 Referer 片段标记。
	if !isUC {
		if mk := d.masterCookie(); mk != "" {
			log.Infof("[%v] 免转存直链(账号Cookie) %v %v", d.getDriverName(), file.GetName(), file.GetSize())
			return &model.Link{
				URL: downloadUrl,
				Header: http.Header{
					"User-Agent": []string{d.conf.ua},
					"Referer":    []string{d.conf.referer},
					"Cookie":     []string{mk},
				},
				Concurrency: 16,
				PartSize:    512 * utils.KB,
			}, nil
		}
	}
	// UC / 夸克无主账号: 维持免转存直链 + 魔法 Referer。UC 匿名直链本就走魔法 Referer;
	// 夸克无主账号时只能匿名,作兜底。后端代理用此 Header(魔法 Referer)绕过 checkplay。
	header := http.Header{
		"User-Agent":      []string{d.conf.ua},
		"Referer":         []string{downloadUrl + "\\ "},
		"Accept-Encoding": []string{"identity"},
	}
	log.Infof("[%v] 免转存直链 %v %v", d.getDriverName(), file.GetName(), file.GetSize())
	// 客户端代理(alist-tvbox 等)直接用原始 URL 且自带 header,无法应用 link.Header。
	// 追加片段标记 #x-referer=raw,供客户端解析后改用魔法 Referer;片段不会发往 CDN。
	return &model.Link{
		URL:         downloadUrl + "#x-referer=raw",
		Header:      header,
		Concurrency: 16,
		PartSize:    512 * utils.KB,
	}, nil
}
