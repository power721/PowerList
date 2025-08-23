package _189_share

import (
	"context"
	"errors"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/go-cache"
	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
	"strconv"
	"time"
)

// do others that not defined in Driver interface

var shareTokenCache = cache.NewMemCache(cache.WithShards[ShareInfo](128))
var limiter = rate.NewLimiter(rate.Every(3000*time.Millisecond), 1)

var idx = 0

func (d *Cloud189Share) Validate() error {
	_, err := d.getShareInfo()
	return err
}

func (d *Cloud189Share) getShareInfo() (ShareInfo, error) {
	tempShareInfo, exist := shareTokenCache.Get(d.ShareId)
	if exist {
		return tempShareInfo, nil
	}

	var shareInfo ShareInfo
	_, err := d.client.R().SetQueryParam("shareCode", d.ShareId).SetResult(&shareInfo).Get("https://cloud.189.cn/api/open/share/getShareInfoByCodeV2.action")

	if err != nil {
		log.Debugf("[%v] 获取天翼网盘分享信息失败: %v", d.ID, err)
		return shareInfo, err
	}

	if shareInfo.ShareId == 0 {
		var checkShareInfo ShareInfo
		_, err = d.client.R().SetQueryParams(map[string]string{
			"shareCode":  d.ShareId,
			"accessCode": d.SharePwd,
		}).SetResult(&checkShareInfo).Get("https://cloud.189.cn/api/open/share/checkAccessCode.action")
		if err != nil {
			log.Debugf("[%v] 获取天翼网盘分享ID失败: %v", d.ID, err)
			return shareInfo, err
		}
		shareInfo.ShareId = checkShareInfo.ShareId
	}

	if shareInfo.FileId != "" {
		shareTokenCache.Set(d.ShareId, shareInfo, cache.WithEx[ShareInfo](time.Minute*time.Duration(d.CacheExpiration)))
		return shareInfo, nil
	} else {
		log.Debugf("[%v] 获取天翼网盘分享信息为空: %v", d.ID, shareInfo)
		return shareInfo, errors.New("获取天翼网盘分享信息为空")
	}
}

func (d *Cloud189Share) getShareFiles(ctx context.Context, dir model.Obj) ([]FileObj, error) {
	shareInfo, err := d.getShareInfo()
	if err != nil {
		return nil, err
	}

	fileId := dir.GetID()
	if fileId == "0" || fileId == "" {
		fileId = shareInfo.FileId
	}

	log.Debugf("shareInfo=%v", shareInfo)
	log.Debugf("fileId=%v", fileId)

	var res []FileObj
	for pageNum := 1; ; pageNum++ {

		var resp Cloud189FilesResp
		_, err := d.client.R().SetQueryParams(map[string]string{
			"pageNum":        strconv.Itoa(pageNum),
			"pageSize":       "60",
			"fileId":         fileId,
			"shareDirFileId": fileId,
			"isFolder":       strconv.FormatBool(shareInfo.IsFolder),
			"shareId":        strconv.Itoa(shareInfo.ShareId),
			"shareMode":      strconv.Itoa(shareInfo.ShareMode),
			"iconOption":     "5",
			"orderBy":        "filename",
			"descending":     "false",
			"accessCode":     d.SharePwd,
		}).SetResult(&resp).Get("https://cloud.189.cn/api/open/share/listShareDir.action")

		if err != nil {
			log.Infof("获取天翼云分享文件:%s失败: %v", dir.GetName(), err)
			return nil, err
		}

		for _, item := range resp.FileListAO.FileList {
			res = append(res, FileObj{
				ObjThumb: model.ObjThumb{
					Object: model.Object{
						ID:       string(item.ID),
						Name:     item.Name,
						Size:     item.Size,
						Ctime:    time.Time(item.CreateDate),
						Modified: time.Time(item.LastOpTime),
						IsFolder: false,
					},
					Thumbnail: model.Thumbnail{Thumbnail: item.Icon.SmallUrl},
				},
				oldName: item.Name,
			})
		}

		for _, item := range resp.FileListAO.FolderList {
			res = append(res, FileObj{
				ObjThumb: model.ObjThumb{
					Object: model.Object{
						ID:       string(item.ID),
						Name:     item.Name,
						Size:     0,
						Ctime:    time.Time(item.CreateDate),
						Modified: time.Time(item.LastOpTime),
						IsFolder: true,
					},
				},
				oldName: item.Name,
			})
		}

		// 获取完毕跳出
		if resp.FileListAO.Count == 0 {
			break
		}

	}
	return res, nil

}
