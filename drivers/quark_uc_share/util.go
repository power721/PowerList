package quark_uc_share

import (
	"context"
	"errors"
	quark "github.com/OpenListTeam/OpenList/v4/drivers/quark_uc"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	"github.com/OpenListTeam/OpenList/v4/pkg/cookie"
	"github.com/go-resty/resty/v2"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	log "github.com/sirupsen/logrus"
)

var Cookie = ""
var idx = 0

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

	u := d.conf.api + pathname
	req := base.RestyClient.R()
	req.SetHeaders(map[string]string{
		"Cookie":     Cookie,
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
	data := base.Json{
		"pwd_id":             d.ShareId,
		"passcode":           d.SharePwd,
		"share_for_transfer": true,
	}
	var errRes Resp
	var resp ShareTokenResp
	res, err := d.request("/share/sharepage/token", http.MethodPost, func(req *resty.Request) {
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

func (d *QuarkUCShare) saveFile(quark *quark.QuarkOrUC, id string) (model.Obj, error) {
	s := strings.Split(id, "-")
	fileId := s[0]
	fileTokenId := s[1]
	data := base.Json{
		"fid_list":       []string{fileId},
		"fid_token_list": []string{fileTokenId},
		"exclude_fids":   []string{},
		"to_pdir_fid":    quark.TempDirId,
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
	res, err := d.request("/share/sharepage/save", http.MethodPost, func(req *resty.Request) {
		req.SetBody(data).SetQueryParams(query)
	}, &resp)
	log.Debugf("saveFile: %v %+v response: %v", id, data, string(res))
	if err != nil {
		log.Warnf("save file failed: %v", err)
		return nil, err
	}
	if resp.Status != 200 {
		return nil, errors.New(resp.Message)
	}
	taskId := resp.Data.TaskId
	log.Debugf("save file task id: %v", taskId)

	newFileId, dirId, err := d.getSaveTaskResult(taskId)
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

func (d *QuarkUCShare) getSaveTaskResult(taskId string) (string, string, error) {
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
		res, err := d.request("/task", http.MethodGet, func(req *resty.Request) {
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

func (d *QuarkUCShare) getShareFiles(id string) ([]File, error) {
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
		res, err := d.request("/share/sharepage/detail", http.MethodGet, func(req *resty.Request) {
			req.SetQueryParams(query)
		}, &resp)
		name := d.getDriverName()
		log.Debugf("%s share get files: %s", name, string(res))
		if err != nil {
			if err.Error() == "分享的stoken过期" {
				d.getShareToken()
				return d.getShareFiles(id)
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
				d.getShareToken()
				return d.getShareFiles(id)
			}
			return nil, errors.New(resp.Message)
		}
	}

	return files, nil
}
