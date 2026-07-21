package quark_uc_share

import (
	"context"
	"errors"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	quark "github.com/OpenListTeam/OpenList/v4/drivers/quark_uc"
	"github.com/OpenListTeam/OpenList/v4/drivers/quark_uc_tv"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	"github.com/OpenListTeam/OpenList/v4/pkg/cookie"
	"github.com/go-resty/resty/v2"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	log "github.com/sirupsen/logrus"
)

var Cookie = ""
var idx = 0
var idx2 = 0

type shareRequestBinding interface {
	doRequest(d *QuarkUCShare, pathname string, method string, callback base.ReqCallback, resp interface{}) ([]byte, error)
	tempDirID() string
}

type requestBinding struct {
	requestDriver *quark.QuarkOrUC
	cookie        string
	tempDirId     string
}

func bindRequestDriver(uc *quark.QuarkOrUC) requestBinding {
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

func (b requestBinding) matches(uc *quark.QuarkOrUC) bool {
	return b.requestDriver == uc
}

type requestTVBinding struct {
	cookie    string
	tempDirId string
}

func bindTVRequestDriver(uc *quark_uc_tv.QuarkUCTV) requestTVBinding {
	return requestTVBinding{
		cookie:    uc.Cookie,
		tempDirId: uc.TempDirId,
	}
}

func (b requestTVBinding) doRequest(d *QuarkUCShare, pathname string, method string, callback base.ReqCallback, resp interface{}) ([]byte, error) {
	return d.directRequest(b.cookie, pathname, method, callback, resp)
}

func (b requestTVBinding) tempDirID() string {
	return b.tempDirId
}

func (b requestTVBinding) matches(other *requestTVBinding) bool {
	return b.cookie == other.cookie && b.tempDirId == other.tempDirId
}

func (d *QuarkUCShare) getDriverName() string {
	name := "Quark"
	if d.config.Name == "UCShare" {
		name = "UC"
	}
	return name
}

func (d *QuarkUCShare) request(pathname string, method string, callback base.ReqCallback, resp interface{}) ([]byte, error) {
	name := d.getDriverName()
	driver := op.GetFirstDriver(name, idx)
	if driver != nil {
		uc := driver.(*quark.QuarkOrUC)
		return uc.Request(pathname, method, callback, resp)
	}

	return d.directRequest(Cookie, pathname, method, callback, resp)
}

func (d *QuarkUCShare) directRequest(cookieStr string, pathname string, method string, callback base.ReqCallback, resp interface{}) ([]byte, error) {
	u := d.conf.api + pathname
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
	__puus := cookie.GetCookie(res.Cookies(), "__puus")
	if __puus != nil {
		log.Debugf("__puus: %v", __puus)
		Cookie = cookie.SetStr(Cookie, "__puus", __puus.Value)
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
	var errRes Resp
	var resp ShareTokenResp
	res, err := d.requestWithBinding(binding, "/share/sharepage/token", http.MethodPost, func(req *resty.Request) {
		req.SetBody(data)
	}, &resp)
	log.Debugf("getShareToken: %v %v", d.ShareId, string(res))
	if err != nil {
		return err
	}
	if errRes.Code != 0 {
		return errors.New(errRes.Message)
	}
	d.ShareToken = resp.Data.ShareToken
	op.MustSaveDriverStorage(d)
	log.Debugf("getShareToken: %v %v", d.ShareId, d.ShareToken)
	return nil
}

func (d *QuarkUCShare) requestWithBinding(binding shareRequestBinding, pathname string, method string, callback base.ReqCallback, resp interface{}) ([]byte, error) {
	if binding != nil {
		return binding.doRequest(d, pathname, method, callback, resp)
	}
	return d.request(pathname, method, callback, resp)
}

func (d *QuarkUCShare) saveFile(binding shareRequestBinding, quark *quark.QuarkOrUC, id string) (model.Obj, error) {
	s := strings.Split(id, "-")
	fileId := s[0]
	fileTokenId := s[1]
	pid := s[2]
	for range 2 {
		data := base.Json{
			"fid_list":       []string{fileId},
			"fid_token_list": []string{fileTokenId},
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
		res, err := d.requestWithBinding(binding, "/share/sharepage/save", http.MethodPost, func(req *resty.Request) {
			req.SetBody(data).SetQueryParams(query)
		}, &resp)
		log.Debugf("saveFile: %v %+v response: %v, error: %v", id, data, string(res), err)
		if err != nil {
			if strings.Contains(err.Error(), "token校验异常") {
				fileTokenId, err = d.getFileToken(binding, pid, fileId)
				if err != nil {
					return nil, err
				}
				continue
			} else {
				log.Warnf("save file failed: %v", err)
				return nil, err
			}
		}
		if resp.Status != 200 {
			return nil, errors.New(resp.Message)
		}
		taskId := resp.Data.TaskId
		log.Debugf("save file task id: %v", taskId)

		newFileId, dirId, err := d.getSaveTaskResult(binding, taskId)
		if err != nil {
			return nil, err
		}
		log.Debugf("new file id: %v dirId: %v", newFileId, dirId)
		file, err := quark.GetTempFile(dirId, newFileId)
		if err != nil {
			log.Warnf("get temp file failed: %v", err)
			return nil, err
		}
		log.Debugf("new file: %+v", file)
		return file, nil
	}
	return nil, errors.New("save file failed")
}

func (d *QuarkUCShare) saveTvFile(ctx context.Context, binding shareRequestBinding, quark *quark_uc_tv.QuarkUCTV, id string) (model.Obj, error) {
	s := strings.Split(id, "-")
	fileId := s[0]
	fileTokenId := s[1]
	pid := s[2]
	for range 2 {
		data := base.Json{
			"fid_list":       []string{fileId},
			"fid_token_list": []string{fileTokenId},
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
		res, err := d.requestWithBinding(binding, "/share/sharepage/save", http.MethodPost, func(req *resty.Request) {
			req.SetBody(data).SetQueryParams(query)
		}, &resp)
		log.Debugf("saveFile: %v %+v response: %v, error: %v", id, data, string(res), err)
		if err != nil {
			if strings.Contains(err.Error(), "token校验异常") {
				fileTokenId, err = d.getFileToken(binding, pid, fileId)
				if err != nil {
					return nil, err
				}
				continue
			} else {
				log.Warnf("save file failed: %v", err)
				return nil, err
			}
		}
		if resp.Status != 200 {
			return nil, errors.New(resp.Message)
		}
		taskId := resp.Data.TaskId
		log.Debugf("save file task id: %v", taskId)

		newFileId, dirId, err := d.getSaveTaskResult(binding, taskId)
		if err != nil {
			return nil, err
		}
		log.Debugf("new file id: %v dirId: %v", newFileId, dirId)
		file, err := quark.GetTempFile(ctx, dirId, newFileId)
		if err != nil {
			log.Warnf("get temp file failed: %v", err)
			return nil, err
		}
		log.Debugf("new file: %+v", file)
		return file, nil
	}
	return nil, errors.New("save file failed")
}

func (d *QuarkUCShare) getSaveTaskResult(binding shareRequestBinding, taskId string) (string, string, error) {
	time.Sleep(200 * time.Millisecond)
	for retry := 1; retry <= 60; {
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
		res, err := d.requestWithBinding(binding, "/task", http.MethodGet, func(req *resty.Request) {
			req.SetQueryParams(query)
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
		time.Sleep(200 * time.Millisecond)
		retry++
	}
	return "", "", errors.New("get task result timeout")
}

func (d *QuarkUCShare) getDownloadUrl(ctx context.Context, quark *quark.QuarkOrUC, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	go d.deleteDelay(quark, file.GetID())
	return quark.Link(ctx, file, args)
}

func (d *QuarkUCShare) getTvDownloadUrl(ctx context.Context, quark *quark_uc_tv.QuarkUCTV, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	go d.deleteDelayTv(ctx, quark, file.GetID())
	return quark.Link(ctx, file, args)
}

func (d *QuarkUCShare) deleteDelay(quark *quark.QuarkOrUC, fileId string) {
	delayTime := setting.GetInt(conf.DeleteDelayTime, 900)
	if delayTime == 0 {
		return
	}
	if delayTime < 5 {
		delayTime = 5
	}

	name := d.getDriverName()
	log.Infof("[%v] Delete %s temp file %v after %v seconds.", quark.ID, name, fileId, delayTime)
	time.Sleep(time.Duration(delayTime) * time.Second)
	d.deleteFile(quark, fileId)
}

func (d *QuarkUCShare) deleteDelayTv(ctx context.Context, quark *quark_uc_tv.QuarkUCTV, fileId string) {
	delayTime := setting.GetInt(conf.DeleteDelayTime, 900)
	if delayTime == 0 {
		return
	}
	if delayTime < 5 {
		delayTime = 5
	}

	name := d.getDriverName()
	log.Infof("[%v] Delete %s temp file %v after %v seconds.", quark.ID, name, fileId, delayTime)
	time.Sleep(time.Duration(delayTime) * time.Second)
	d.deleteFileTv(ctx, quark, fileId)
}

func (d *QuarkUCShare) deleteFile(quark *quark.QuarkOrUC, fileId string) {
	name := d.getDriverName()
	log.Infof("[%v] Delete %s temp file: %v", quark.ID, name, fileId)
	data := base.Json{
		"action_type":  1,
		"exclude_fids": []string{},
		"filelist":     []string{fileId},
	}
	var resp PlayResp
	res, err := quark.Request("/file/delete", http.MethodPost, func(req *resty.Request) {
		req.SetBody(data)
	}, &resp)
	log.Debugf("[%v] Delete %s temp file: %v %v", quark.ID, name, fileId, string(res))
	if err != nil {
		log.Warnf("[%v] Delete %s temp file failed: %v %v", quark.ID, name, fileId, err)
	} else if resp.Status != 200 {
		log.Warnf("[%v] Delete %s temp file failed: %v %v", quark.ID, name, fileId, resp.Message)
	}
}

func (d *QuarkUCShare) deleteFileTv(ctx context.Context, quark *quark_uc_tv.QuarkUCTV, fileId string) {
	name := d.getDriverName()
	log.Infof("[%v] Delete %s temp file: %v", quark.ID, name, fileId)
	data := base.Json{
		"action_type":  1,
		"exclude_fids": []string{},
		"filelist":     []string{fileId},
	}
	var resp PlayResp
	res, err := quark.Request(ctx, "/file/delete", http.MethodPost, func(req *resty.Request) {
		req.SetBody(data)
	}, &resp)
	log.Debugf("[%v] Delete %s temp file: %v %v", quark.ID, name, fileId, string(res))
	if err != nil {
		log.Warnf("[%v] Delete %s temp file failed: %v %v", quark.ID, name, fileId, err)
	} else if resp.Status != 200 {
		log.Warnf("[%v] Delete %s temp file failed: %v %v", quark.ID, name, fileId, resp.Message)
	}
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
		res, err := d.requestWithBinding(binding, "/share/sharepage/detail", http.MethodGet, func(req *resty.Request) {
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
	_, err := d.directRequest(reqCookie, "/file/download", http.MethodPost, func(req *resty.Request) {
		req.SetBody(body)
	}, &resp)
	// fid_token 失效时,按 pid 重新换取 share_fid_token 后重试一次(复用 saveFile 的回退策略)。
	if err != nil && strings.Contains(err.Error(), "token校验异常") && pid != "" {
		if newToken, e := d.getFileToken(nil, pid, fileId); e == nil && newToken != "" {
			body["fids_token"] = []string{newToken}
			_, err = d.directRequest(reqCookie, "/file/download", http.MethodPost, func(req *resty.Request) {
				req.SetBody(body)
			}, &resp)
		}
	}
	if err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 || resp.Data[0].DownloadUrl == "" {
		return nil, errors.New("empty share download url")
	}
	downloadUrl := resp.Data[0].DownloadUrl
	// 后端代理用此 Header(魔法 Referer)绕过 checkplay。
	header := http.Header{
		"User-Agent":      []string{d.conf.ua},
		"Referer":         []string{downloadUrl + "\\ "},
		"Accept-Encoding": []string{"identity"},
	}
	log.Infof("[%v] 免转存直链 %v %v", d.getDriverName(), file.GetName(), file.GetSize())
	// 客户端代理(alist-tvbox 等)直接用原始 URL 且自带 header,无法应用 link.Header。
	// 追加片段标记 #x-referer=raw,供客户端解析后改用魔法 Referer;片段不会发往 CDN。
	return &model.Link{
		URL:    downloadUrl + "#x-referer=raw",
		Header: header,
	}, nil
}
