package quark_uc_tv

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/internal/token"
	"github.com/OpenListTeam/OpenList/v4/pkg/cookie"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
)

func (d *QuarkUCTV) getTempFolder(ctx context.Context) {
	files, err := d.GetFiles(ctx, "0")
	if err != nil {
		log.Warnf("get files error: %v", err)
	}

	for _, file := range files {
		if file.GetName() == conf.TempDirName {
			d.TempDirId = file.GetID()
			log.Infof("%v temp folder id: %v", d.config.Name, d.TempDirId)
			d.cleanTempFolder(ctx)
			return
		}
	}

	d.createTempFolder()
}

func (d *QuarkUCTV) createTempFolder() {
	data := base.Json{
		"dir_init_lock": false,
		"dir_path":      "",
		"file_name":     conf.TempDirName,
		"pdir_fid":      "0",
	}
	res, err := d.request2("/file", http.MethodPost, func(req *resty.Request) {
		req.SetBody(data)
	}, nil)
	fid := utils.Json.Get(res, "data", "fid").ToString()
	if fid != "" {
		d.TempDirId = fid
	}
	log.Infof("create temp folder: %v", string(res))
	if err != nil {
		log.Warnf("create temp folder error: %v", err)
	}
}

func (d *QuarkUCTV) cleanTempFolder(ctx context.Context) {
	if d.TempDirId == "0" {
		return
	}

	files, err := d.GetFiles(ctx, d.TempDirId)
	if err != nil {
		log.Warnf("get files error: %v", err)
	}

	for _, file := range files {
		go d.deleteFile(file.GetID())
	}
}

func (d *QuarkUCTV) GetTempFile(ctx context.Context, dirId, id string) (model.Obj, error) {
	var dir = &Files{
		Fid: dirId,
	}
	var args = model.ListArgs{}
	for i := 0; i < 3; i++ {
		files, err := d.List(ctx, dir, args)
		if err != nil {
			return nil, err
		}

		for _, file := range files {
			if file.GetID() == id {
				return file, nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	return nil, errors.New("file not found")
}

func (d *QuarkUCTV) deleteFile(fileId string) error {
	data := base.Json{
		"action_type":  1,
		"exclude_fids": []string{},
		"filelist":     []string{fileId},
	}
	res, err := d.request2("/file/delete", http.MethodPost, func(req *resty.Request) {
		req.SetBody(data)
	}, nil)
	log.Debugf("deleteFile: %v %v", fileId, string(res))
	if err != nil {
		log.Warnf("Delete %v temp file failed: %v %v", d.Config().Name, fileId, err)
		return err
	}
	return nil
}

func (d *QuarkUCTV) GetFiles(ctx context.Context, parent string) ([]model.Obj, error) {
	files := make([]model.Obj, 0)
	pageIndex := int64(0)
	pageSize := int64(100)
	desc := "1"
	orderBy := "3"
	if d.OrderDirection == "asc" {
		desc = "0"
	}
	if d.OrderBy == "file_name" {
		orderBy = "1"
	}
	for {
		var filesData FilesData
		_, err := d.request(ctx, "/file", http.MethodGet, func(req *resty.Request) {
			req.SetQueryParams(map[string]string{
				"method":     "list",
				"parent_fid": parent,
				"order_by":   orderBy,
				"desc":       desc,
				"category":   "",
				"source":     "",
				"ex_source":  "",
				"list_all":   "0",
				"page_size":  strconv.FormatInt(pageSize, 10),
				"page_index": strconv.FormatInt(pageIndex, 10),
			})
		}, &filesData)
		if err != nil {
			return nil, err
		}
		for i := range filesData.Data.Files {
			files = append(files, &filesData.Data.Files[i])
		}
		if pageIndex*pageSize >= filesData.Data.TotalCount {
			break
		} else {
			pageIndex++
		}
	}
	return files, nil
}

func (d *QuarkUCTV) request2(pathname string, method string, callback base.ReqCallback, resp interface{}) ([]byte, error) {
	u := d.conf.api + pathname
	req := base.RestyClient.R()
	req.SetHeaders(map[string]string{
		"Cookie":  d.Cookie,
		"Accept":  "application/json, text/plain, */*",
		"Referer": d.conf.referer,
	})
	req.SetQueryParam("pr", d.conf.pr)
	req.SetQueryParam("fr", "pc")
	if d.config.Name == "UC" {
		req.SetQueryParam("sys", "darwin")
		req.SetQueryParam("ve", "1.8.6")
	}
	if callback != nil {
		callback(req)
	}
	if resp != nil {
		req.SetResult(resp)
	}
	var e Resp2
	req.SetError(&e)
	res, err := req.Execute(method, u)
	if err != nil {
		return nil, err
	}
	__puus := cookie.GetCookie(res.Cookies(), "__puus")
	if __puus != nil {
		log.Debugf("update __puus: %v", __puus)
		d.Cookie = cookie.SetStr(d.Cookie, "__puus", __puus.Value)
		d.SaveCookie(d.Cookie)
	} else {
		c := res.Request.Header.Get("Cookie")
		v1 := cookie.GetStr(d.Cookie, "__puus")
		v2 := cookie.GetStr(c, "__puus")
		if v2 != "" && v1 != v2 {
			d.Cookie = cookie.SetStr(d.Cookie, "__puus", v2)
			log.Debugf("update cookie: %v %v %v", d.Cookie, v1, v2)
			d.SaveCookie(d.Cookie)
		}
	}

	if e.Status >= 400 || e.Code != 0 {
		return nil, errors.New(e.Message)
	}
	return res.Body(), nil
}

func (d *QuarkUCTV) SaveCookie(cookie string) {
	var key = conf.QUARK
	if d.config.Name == "UC" {
		key = conf.UC
	}
	d.Cookie = cookie
	op.MustSaveDriverStorage(d)
	token.SaveAccountToken(key, d.Cookie, int(d.ID))
}
