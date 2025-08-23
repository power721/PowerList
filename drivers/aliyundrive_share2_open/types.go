package aliyundrive_share2_open

import (
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

type ErrorResp struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ShareTokenResp struct {
	ShareToken string    `json:"share_token"`
	ExpireTime time.Time `json:"expire_time"`
	ExpiresIn  int       `json:"expires_in"`
}

type ListResp struct {
	Items             []File `json:"items"`
	NextMarker        string `json:"next_marker"`
	PunishedFileCount int    `json:"punished_file_count"`
}

type File struct {
	ID           string    `json:"id"`
	DriveId      string    `json:"drive_id"`
	DomainId     string    `json:"domain_id"`
	FileId       string    `json:"file_id"`
	ShareId      string    `json:"share_id"`
	ContentHash  string    `json:"content_hash"`
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	ParentFileId string    `json:"parent_file_id"`
	Size         int64     `json:"size"`
	Thumbnail    string    `json:"thumbnail"`
}

func fileToObj(f File) *model.ObjThumb {
	return &model.ObjThumb{
		Object: model.Object{
			ID:       f.FileId,
			Name:     f.Name,
			Size:     f.Size,
			Modified: f.UpdatedAt,
			IsFolder: f.Type == "folder",
		},
		Thumbnail: model.Thumbnail{Thumbnail: f.Thumbnail},
	}
}

type ShareLinkResp struct {
	DownloadUrl string `json:"download_url"`
	Url         string `json:"url"`
	Thumbnail   string `json:"thumbnail"`
}

type Request struct {
}

type MyFile struct {
	FileId   string    `json:"file_id"`
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	UpdateAt time.Time `json:"UpdateAt"`
	utils.HashInfo
}

func (f MyFile) CreateTime() time.Time {
	return f.UpdateAt
}

func (f MyFile) GetHash() utils.HashInfo {
	return f.HashInfo
}

type VideoPreviewResponse struct {
	PlayInfo VideoPreviewPlayInfo `json:"video_preview_play_info"`
}

type VideoPreviewPlayInfo struct {
	Videos    []LiveTranscoding         `json:"live_transcoding_task_list"`
	Subtitles []LiveTranscodingSubtitle `json:"live_transcoding_subtitle_task_list"`
}

type LiveTranscoding struct {
	TemplateId string `json:"template_id"`
	Status     string `json:"status"`
	Url        string `json:"url"`
}

type LiveTranscodingSubtitle struct {
	Language string `json:"language"`
	Status   string `json:"status"`
	Url      string `json:"url"`
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
