package _115_index

import (
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/index115"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

var _ model.Obj = (*FileObj)(nil)
var idx = 0

type FileObj struct {
	Size     int64
	Duration int
	Sha1     string
	Utm      time.Time
	FileName string
	isDir    bool
	FileID   string
}

func (f *FileObj) CreateTime() time.Time {
	return f.Utm
}

func (f *FileObj) GetHash() utils.HashInfo {
	return utils.NewHashInfo(utils.SHA1, f.Sha1)
}

func (f *FileObj) GetSize() int64 {
	return f.Size
}

func (f *FileObj) GetDuration() int {
	return f.Duration
}

func (f *FileObj) GetName() string {
	return f.FileName
}

func (f *FileObj) ModTime() time.Time {
	return f.Utm
}

func (f *FileObj) IsDir() bool {
	return f.isDir
}

func (f *FileObj) GetID() string {
	return f.FileID
}

func (f *FileObj) GetPath() string {
	return ""
}

func transFunc(sf index115.FileItem) (model.Obj, error) {
	var (
		utm    = time.Unix(sf.UpdatedAt, 0)
		isDir  = sf.IsDir
		fileID = sf.ShareCode + ":" + sf.ReceiveCode + ":" + sf.FileID + ":" + sf.SHA1
	)
	return &FileObj{
		Size:     sf.Size,
		Sha1:     sf.SHA1,
		Utm:      utm,
		FileName: sf.Name,
		isDir:    isDir,
		FileID:   fileID,
	}, nil
}
