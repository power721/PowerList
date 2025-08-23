package quark_share

import (
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"time"
)

type Resp struct {
	Status  int    `json:"status"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type ShareTokenResp struct {
	Data ShareTokenData `json:"data"`
	Resp
}

type ShareTokenData struct {
	ShareToken string `json:"stoken"`
}

type ListMetadata struct {
	Total int `json:"_total"`
}

type ListResp struct {
	Data     ListData     `json:"data"`
	Metadata ListMetadata `json:"metadata"`
	Resp
}

type ListData struct {
	Files []File `json:"list"`
}

type SaveResp struct {
	Data TaskData `json:"data"`
	Resp
}

type TaskData struct {
	TaskId string `json:"task_id"`
}

type SaveTaskResp struct {
	Data SaveTaskData `json:"data"`
	Resp
}

type SaveTaskData struct {
	SaveAs SaveAsData `json:"save_as"`
}

type SaveAsData struct {
	Fid   []string `json:"save_as_top_fids"`
	DirId string   `json:"to_pdir_fid"`
}

type PlayResp struct {
	Data PlayData `json:"data"`
	Resp
}

type PlayData struct {
	VideoList []Video `json:"video_list"`
}

type DownResp struct {
	Resp
	Data []struct {
		DownloadUrl string `json:"download_url"`
	} `json:"data"`
}

type Video struct {
	Resolution string    `json:"resolution"`
	Format     string    `json:"supports_format"`
	Info       VideoInfo `json:"video_info"`
}

type VideoInfo struct {
	Url        string `json:"url"`
	Resolution string `json:"resolution"`
}

type File struct {
	ID        string `json:"fid"`
	FID       string `json:"share_fid_token"`
	Name      string `json:"file_name"`
	Type      int    `json:"file_type"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
	Size      int64  `json:"size"`
}

func fileToObj(f File) *model.ObjThumb {
	return &model.ObjThumb{
		Object: model.Object{
			ID:       f.ID + "-" + f.FID,
			Name:     f.Name,
			Size:     f.Size,
			Modified: time.UnixMilli(f.UpdatedAt),
			IsFolder: f.Type == 0,
		},
	}
}

type SortResp struct {
	Resp
	Data struct {
		List []File `json:"list"`
	} `json:"data"`
	Metadata struct {
		Size  int    `json:"_size"`
		Page  int    `json:"_page"`
		Count int    `json:"_count"`
		Total int    `json:"_total"`
		Way   string `json:"way"`
	} `json:"metadata"`
}

type Request struct {
}

type MyFile struct {
	FileId   string    `json:"file_id"`
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	UpdateAt time.Time `json:"UpdateAt"`
}

type VideoPreviewResponse struct {
	PlayInfo VideoPreviewPlayInfo `json:"video_preview_play_info"`
}

type VideoPreviewPlayInfo struct {
	Videos []LiveTranscoding `json:"live_transcoding_task_list"`
}

type LiveTranscoding struct {
	TemplateId string `json:"template_id"`
	Status     string `json:"status"`
	Url        string `json:"url"`
}

func (f MyFile) CreateTime() time.Time {
	return f.UpdateAt
}

func (f MyFile) GetHash() utils.HashInfo {
	return utils.HashInfo{}
}

func (f MyFile) GetPath() string {
	return ""
}

func (f MyFile) GetSize() int64 {
	return f.Size
}

func (f MyFile) GetName() string {
	return f.Name
}

func (f MyFile) ModTime() time.Time {
	return f.UpdateAt
}

func (f MyFile) IsDir() bool {
	return false
}

func (f MyFile) GetID() string {
	return f.FileId
}
