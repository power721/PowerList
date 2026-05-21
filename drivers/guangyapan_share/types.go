package guangyapan_share

import (
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

type shareAccessTokenResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		AccessToken string `json:"accessToken"`
	} `json:"data"`
}

type shareListResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Total  int         `json:"total"`
		List   []shareFile `json:"list"`
		Cursor int         `json:"cursor"`
	} `json:"data"`
}

type shareFile struct {
	FileID    string `json:"fileId"`
	FileName  string `json:"fileName"`
	FileSize  int64  `json:"fileSize"`
	ParentID  string `json:"parentId"`
	ResType   int    `json:"resType"`
	FileType  int    `json:"fileType"`
	MineType  string `json:"mineType"`
	Ext       string `json:"ext"`
	Thumbnail string `json:"thumbnail"`
	CTime     int64  `json:"ctime"`
	UTime     int64  `json:"utime"`
}

type restoreShareResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		TaskID string `json:"taskId"`
	} `json:"data"`
}

type taskStatusResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Status int `json:"status"`
	} `json:"data"`
}

type taskInfoResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		FileID string `json:"fileId"`
	} `json:"data"`
}

func fileToObj(f shareFile) model.Obj {
	obj := model.Object{
		ID:       f.FileID,
		Name:     f.FileName,
		Size:     f.FileSize,
		Modified: unixOrZero(f.UTime),
		Ctime:    unixOrZero(f.CTime),
		IsFolder: f.ResType == 2,
	}
	if f.Thumbnail != "" {
		return &model.ObjThumb{
			Object:    obj,
			Thumbnail: model.Thumbnail{Thumbnail: f.Thumbnail},
		}
	}
	return &obj
}

func unixOrZero(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	return time.Unix(v, 0)
}
