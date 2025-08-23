package baidu_share

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/OpenListTeam/OpenList/v4/drivers/baidu_netdisk"
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
	"net/http"
	"net/url"
	"path"
	"regexp"
	"time"
)

var idx = 0

type BaiduShare2 struct {
	model.Storage
	Addition
	client *resty.Client

	ShareId string
	ShareUk string
	Token   string
}

func (d *BaiduShare2) Config() driver.Config {
	return config
}

func (d *BaiduShare2) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *BaiduShare2) Init(ctx context.Context) error {
	d.client = resty.New().
		SetBaseURL("https://pan.baidu.com").
		SetHeader("User-Agent", "netdisk").
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
		res, err := d.client.R().
			SetFormData(data).
			SetHeader("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8").
			SetResult(&respJson).
			Post(api)
		if err != nil {
			return err
		}
		log.Debugf("Baidu share verify response: %v", respJson)
		if respJson.Errno != 0 {
			msg := respJson.Message
			if msg == "" {
				msg = res.String()
			}
			return errors.New(msg)
		}
		d.Token = respJson.Token
		log.Debugf("Baidu Share Token: %v", d.Token)
	}

	return d.getInfo()
}

func (d *BaiduShare2) getInfo() error {
	api := "/s/" + d.Surl
	res, err := d.client.R().
		Get(api)
	if err != nil {
		return err
	}
	BDCLND := cookie.GetCookie(res.Cookies(), "BDCLND")
	if BDCLND != nil {
		d.Token = BDCLND.Value
	}

	re := regexp.MustCompile(`shareid:\s*"(\d+)"`)
	matches := re.FindStringSubmatch(res.String())
	if len(matches) >= 2 {
		d.ShareId = matches[1]
		log.Debugf("Share ID: %v", d.ShareId)
	} else {
		log.Warn("Share ID not found")
	}

	re = regexp.MustCompile(`share_uk:\s*"(\d+)"`)
	matches = re.FindStringSubmatch(res.String())
	if len(matches) >= 2 {
		d.ShareUk = matches[1]
		log.Debugf("Share UK: %v", d.ShareUk)
	} else {
		log.Warn("Share UK not found")
	}

	log.Debugf("Share Token: %v", d.Token)
	return nil
}

func (d *BaiduShare2) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	if d.Token == "" {
		d.Validate()
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
			} else {
				err = fmt.Errorf("%s", res.Body())
			}
		}
	}
	return objs, err
}

func (d *BaiduShare2) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	count := op.GetDriverCount("BaiduNetdisk")
	var err error
	for i := 0; i < count; i++ {
		link, err := d.link(ctx, file, args)
		if err == nil {
			return link, nil
		}
	}
	return nil, err
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
		d.Validate()
	}
	f, err := d.saveFile(file.GetID(), bd)
	if err != nil {
		return nil, err
	}

	go d.delete(ctx, f, bd)

	link, err := bd.Link(ctx, f, args)
	log.Debugf("Baidu link: %v %v %v", f.GetID(), f.GetPath(), link)
	return link, err
}

func (d *BaiduShare2) saveFile(fid string, bd *baidu_netdisk.BaiduNetdisk) (model.Obj, error) {
	Cookie := cookie.SetStr(bd.Cookie, "BDCLND", d.Token)
	decoded, err := url.QueryUnescape(d.Token)
	data := map[string]string{
		"fsidlist": fmt.Sprintf("[%v]", fid),
		"path":     "/" + conf.TempDirName,
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
		"sekey":      decoded,
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

	if utils.Json.Get(res.Body(), "errno").ToInt() != 0 {
		return nil, errors.New(utils.Json.Get(res.Body(), "show_msg").ToString())
	}

	file := File{
		FileId: utils.Json.Get(res.Body(), "extra", "list", 0, "to_fs_id").ToInt64(),
		Path:   utils.Json.Get(res.Body(), "extra", "list", 0, "to").ToString(),
	}
	return file, nil
}

func (d *BaiduShare2) delete(ctx context.Context, file model.Obj, bd *baidu_netdisk.BaiduNetdisk) {
	delayTime := setting.GetInt(conf.DeleteDelayTime, 900)
	if delayTime == 0 {
		return
	}

	if delayTime < 5 {
		delayTime = 5
	}

	log.Infof("[%v] Delete Baidu temp file %v after %v seconds.", bd.ID, file.GetID(), delayTime)
	time.Sleep(time.Duration(delayTime) * time.Second)
	bd.Remove(ctx, file)
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

var _ driver.Driver = (*BaiduShare2)(nil)
