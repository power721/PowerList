package aliyundrive_share2_open

import (
	"context"
	"errors"
	"fmt"
	_115 "github.com/OpenListTeam/OpenList/v4/drivers/115"
	"github.com/OpenListTeam/OpenList/v4/drivers/aliyundrive_open"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	"github.com/OpenListTeam/OpenList/v4/internal/stream"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/go-resty/resty/v2"
	"github.com/power721/115driver/pkg/driver"
	"net/http"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	log "github.com/sirupsen/logrus"
)

const (
	// CanaryHeaderKey CanaryHeaderValue for lifting rate limit restrictions
	CanaryHeaderKey   = "X-Canary"
	CanaryHeaderValue = "client=web,app=share,version=v2.3.1"
)

var idx = 0
var idx2 = 0

func getAliOpenDriver(idx int) (*aliyundrive_open.AliyundriveOpen, error) {
	if setting.GetBool(conf.DriverRoundRobin) {
		storages := op.GetStorages("AliyundriveOpen")
		if len(storages) == 0 {
			return nil, errors.New("没有阿里云盘帐号")
		}

		var vips []*aliyundrive_open.AliyundriveOpen
		for _, storage := range storages {
			ali := storage.(*aliyundrive_open.AliyundriveOpen)
			if ali.IsVip {
				vips = append(vips, ali)
			}
		}

		if len(vips) > 0 {
			return vips[idx%len(vips)], nil
		}
	}

	storage := op.GetFirstDriver("AliyundriveOpen", idx)
	if storage == nil {
		return nil, errors.New("找不到阿里云盘帐号")
	}
	ali := storage.(*aliyundrive_open.AliyundriveOpen)
	return ali, nil
}

func (d *AliyundriveShare2Open) Validate() error {
	return d.getShareToken()
}

func (d *AliyundriveShare2Open) getShareToken() error {
	data := base.Json{
		"share_id": d.ShareId,
	}
	if d.SharePwd != "" {
		data["share_pwd"] = d.SharePwd
	}
	var e ErrorResp
	var resp ShareTokenResp
	_, err := base.RestyClient.R().
		SetResult(&resp).SetError(&e).SetBody(data).
		Post("https://api.alipan.com/v2/share_link/get_share_token")
	if err != nil {
		return err
	}
	if e.Code != "" {
		return errors.New(e.Message)
	}
	d.ShareToken = resp.ShareToken
	log.Debug("getShareToken", d.ShareId, d.ShareToken)
	return nil
}

func (d *AliyundriveShare2Open) saveFile(ali *aliyundrive_open.AliyundriveOpen, fileId string) (string, error) {
	data := base.Json{
		"requests": []base.Json{
			{
				"body": base.Json{
					"file_id":           fileId,
					"share_id":          d.ShareId,
					"auto_rename":       true,
					"to_parent_file_id": ali.TempDirId,
					"to_drive_id":       ali.DriveId,
				},
				"headers": base.Json{
					"Content-Type": "application/json",
				},
				"id":     "0",
				"method": "POST",
				"url":    "/file/copy",
			},
		},
		"resource": "file",
	}

	res, err := d.request(ali, "https://api.alipan.com/adrive/v4/batch", http.MethodPost, func(req *resty.Request) {
		req.SetBody(data)
	})

	if err != nil {
		log.Errorf("[%v] 保存文件失败: %v", ali.ID, err)
		if strings.Contains(err.Error(), "share_id doesn't match. share_token") {
			log.Infof("[%v] getShareToken: %v", ali.ID, d.ShareId)
			d.getShareToken()
		}
		return "", err
	}

	msg := utils.Json.Get(res, "responses", 0, "body", "message").ToString()
	if msg != "" {
		log.Infof("[%v] 请求结果 : %v", ali.ID, string(res))
		log.Errorf("[%v] 保存文件失败 : %v", ali.ID, msg)
		if strings.Contains(msg, "share_id doesn't match. share_token") {
			log.Infof("[%v] getShareToken: %v", ali.ID, d.ShareId)
			d.getShareToken()
		}
		return "", errors.New(msg)
	}

	newFile := utils.Json.Get(res, "responses", 0, "body", "file_id").ToString()
	return newFile, nil
}

func (d *AliyundriveShare2Open) getOpenLink(ali *aliyundrive_open.AliyundriveOpen, file model.Obj) (*model.Link, string, error) {
	res, err := ali.Request("/adrive/v1.0/openFile/getDownloadUrl", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"drive_id":   ali.DriveId,
			"file_id":    file.GetID(),
			"expire_sec": 14400,
		})
	})

	go d.deleteDelay(ali, file.GetID())

	if err != nil {
		log.Errorf("[%v] getOpenLink failed: %v", ali.ID, err)
		return nil, "", err
	}
	url := utils.Json.Get(res, "url").ToString()
	if url == "" {
		if utils.Ext(file.GetName()) != "livp" {
			return nil, "", errors.New("get download url failed: " + string(res))
		}
		url = utils.Json.Get(res, "streamsUrl", "mov").ToString()
	}
	hash := utils.Json.Get(res, "content_hash").ToString()

	exp := 895 * time.Second
	return &model.Link{
		URL:        url + fmt.Sprintf("#storageId=%d", ali.ID),
		Expiration: &exp,
		Header: http.Header{
			"Referer":    []string{"https://www.alipan.com/"},
			"User-Agent": []string{conf.UserAgent},
		},
		Concurrency: ali.Concurrency,
		PartSize:    ali.ChunkSize * utils.KB,
	}, hash, nil
}

func (d *AliyundriveShare2Open) getDownloadUrl(ali *aliyundrive_open.AliyundriveOpen, fileId string) (string, string, error) {
	log.Infof("[%v] getDownloadUrl %v %v", ali.ID, ali.DriveId, fileId)
	res, err := ali.Request("/adrive/v1.0/openFile/getDownloadUrl", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"drive_id":   ali.DriveId,
			"file_id":    fileId,
			"expire_sec": 14400,
		})
	})

	if err != nil {
		log.Errorf("[%v] getDownloadUrl failed: %v", ali.ID, err)
		return "", "", err
	}
	url := utils.Json.Get(res, "url").ToString()
	if url == "" {
		url = utils.Json.Get(res, "streamsUrl", "mov").ToString()
	}

	hash := utils.Json.Get(res, "content_hash").ToString()

	return url, hash, nil
}

func (d *AliyundriveShare2Open) deleteDelay(ali *aliyundrive_open.AliyundriveOpen, fileId string) {
	delayTime := setting.GetInt(conf.DeleteDelayTime, 900)
	if delayTime == 0 {
		return
	}

	log.Infof("[%v] Delete aliyun temp file %v after %v seconds.", ali.ID, fileId, delayTime)
	time.Sleep(time.Duration(delayTime) * time.Second)
	d.deleteOpen(ali, fileId)
}

func (d *AliyundriveShare2Open) deleteOpen(ali *aliyundrive_open.AliyundriveOpen, fileId string) {
	log.Infof("[%v] Delete Aliyun temp file: %v", ali.ID, fileId)
	_, err := ali.Request("/adrive/v1.0/openFile/delete", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{
			"drive_id": ali.DriveId,
			"file_id":  fileId,
		})
	})
	if err != nil {
		log.Warnf("[%v] 删除文件%v失败： %v", ali.ID, fileId, err)
	}
}

func (d *AliyundriveShare2Open) request(ali *aliyundrive_open.AliyundriveOpen, url, method string, callback base.ReqCallback) ([]byte, error) {
	var e ErrorResp
	req := base.RestyClient.R().
		SetError(&e).
		SetHeader("content-type", "application/json").
		SetHeader("Referer", "https://www.alipan.com/").
		SetHeader("User-Agent", conf.UserAgent).
		SetHeader("Authorization", "Bearer\t"+ali.AccessToken2).
		SetHeader(CanaryHeaderKey, CanaryHeaderValue).
		SetHeader("x-share-token", d.ShareToken)
	if callback != nil {
		callback(req)
	} else {
		req.SetBody("{}")
	}
	resp, err := req.Execute(method, url)
	if err != nil {
		log.Warnf("[%v] 请求失败: %v", ali.ID, err)
		return nil, err
	}
	if e.Code != "" {
		log.Warnf("[%v] 请求失败: %v %v", ali.ID, e.Code, e.Message)
		if e.Code == "AccessTokenInvalid" || e.Code == "ShareLinkTokenInvalid" {
			if e.Code == "AccessTokenInvalid" {
				err = ali.RefreshAliToken(true)
			} else {
				err = d.getShareToken()
			}
			if err != nil {
				return nil, err
			}
			return d.request(ali, url, method, callback)
		} else {
			return nil, errors.New(e.Code + ": " + e.Message)
		}
	}
	return resp.Body(), nil
}

func (d *AliyundriveShare2Open) getFiles(fileId string) ([]File, error) {
	files := make([]File, 0)
	data := base.Json{
		"limit":           200,
		"order_by":        d.OrderBy,
		"order_direction": d.OrderDirection,
		"parent_file_id":  fileId,
		"share_id":        d.ShareId,
		"marker":          "",
	}
	retry := 0
	for {
		var e ErrorResp
		var resp ListResp
		res, err := base.RestyClient.R().
			SetHeader("x-share-token", d.ShareToken).
			SetHeader(CanaryHeaderKey, CanaryHeaderValue).
			SetResult(&resp).SetError(&e).SetBody(data).
			Post("https://api.alipan.com/adrive/v3/file/list")
		if err != nil {
			return nil, err
		}
		log.Debugf("aliyundrive share get files: %s", res.String())
		if e.Code != "" {
			log.Warnf("[%v] aliyundrive share get files error: %v", d.ID, e)
			if e.Code == "AccessTokenInvalid" || e.Code == "ShareLinkTokenInvalid" {
				err = d.getShareToken()
				if err != nil {
					return nil, err
				}
				return d.getFiles(fileId)
			}
			if e.Code != "ParamFlowException" || retry > 9 {
				return nil, errors.New(e.Message)
			}
		}
		if e.Code == "" {
			data["marker"] = resp.NextMarker
			files = append(files, resp.Items...)
			if resp.NextMarker == "" {
				break
			}
		} else {
			retry++
			log.Infof("[%v] retry get files: %v", d.ID, retry)
		}
	}
	return files, nil
}

func (d *AliyundriveShare2Open) saveTo115(ctx context.Context, pan115 *_115.Pan115, file model.Obj, link *model.Link, args model.LinkArgs) (*model.Link, error) {
	if ok, err := pan115.UploadAvailable(); err != nil || !ok {
		return link, err
	}
	log.Debugf("save file to 115 cloud: file=%v dir=%v", file.GetID(), pan115.TempDirId)
	fs := &stream.FileStream{
		Obj: file,
		Ctx: ctx,
	}

	ss, err := stream.NewSeekableStream(fs, link)
	if err != nil {
		log.Warnf("NewSeekableStream failed: %v", err)
		return link, err
	}

	preHash := "2EF7BDE608CE5404E97D5F042F95F89F1C232871"
	fullHash := fs.GetHash().GetHash(utils.SHA1)
	log.Infof("[%v] id=%v name=%v size=%v hash=%v", pan115.ID, fs.GetID(), fs.GetName(), fs.GetSize(), fullHash)
	res, err := pan115.RapidUpload(fs.GetSize(), fs.GetName(), pan115.TempDirId, preHash, fullHash, ss)
	if err != nil {
		log.Warnf("[%v] 115 upload failed: %v", pan115.ID, err)
		return link, err
	}
	log.Debugf("115.RapidUpload: %v", res)
	go d.delayDelete115(pan115, fullHash)
	for i := 0; i < 5; i++ {
		var f = &_115.FileObj{
			File: driver.File{
				PickCode: res.PickCode,
			},
		}
		link115, err := pan115.Link(ctx, f, args)
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		log.Infof("[%v] 使用115链接: %v", pan115.ID, link115.URL)
		return &model.Link{
			URL:         link115.URL,
			Header:      link115.Header,
			Expiration:  link115.Expiration,
			Concurrency: pan115.Concurrency,
			PartSize:    pan115.ChunkSize * utils.KB,
		}, nil
	}
	log.Warnf("获取115链接超时，使用阿里链接")
	return link, err
}

func (d *AliyundriveShare2Open) delayDelete115(pan115 *_115.Pan115, fileId string) {
	delayTime := setting.GetInt(conf.DeleteDelayTime, 900)
	if delayTime == 0 {
		return
	}

	if delayTime < 5 {
		delayTime = 5
	}

	log.Infof("[%v] Delete 115 temp file %v after %v seconds.", pan115.ID, fileId, delayTime)
	time.Sleep(time.Duration(delayTime) * time.Second)
	pan115.DeleteTempFile(fileId)
}
