package _139_share

import (
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"time"
)

type Folder struct {
	Name      string `json:"caName"`
	Path      string `json:"path"`
	UpdatedAt string `json:"udTime"`
}

type File struct {
	Name      string `json:"coName"`
	Path      string `json:"path"`
	Type      int    `json:"coType"`
	Size      int64  `json:"coSize"`
	UpdatedAt string `json:"udTime"`
	IsDir     bool
	Time      time.Time
}

type ListResp struct {
	Success bool     `json:"success"`
	Code    string   `json:"code"`
	Desc    string   `json:"desc"`
	Data    ListData `json:"data"`
}

type ListData struct {
	Count   int64    `json:"nodNum"`
	Next    string   `json:"nextPageCursor"`
	Folders []Folder `json:"caLst"`
	Files   []File   `json:"coLst"`
}

type LinkResp struct {
	Success bool     `json:"success"`
	Code    string   `json:"code"`
	Desc    string   `json:"desc"`
	Data    LinkData `json:"data"`
}

type LinkData struct {
	Url     string  `json:"redrUrl"`
	ExtInfo ExtInfo `json:"extInfo"`
}

type ExtInfo struct {
	Url string `json:"cdnDownloadUrl"`
}

func fileToObj(f File) *model.ObjThumb {
	return &model.ObjThumb{
		Object: model.Object{
			ID:       f.Path,
			Name:     f.Name,
			Modified: f.Time,
			Size:     f.Size,
			IsFolder: f.IsDir,
		},
	}
}
