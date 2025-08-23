package baidu_share

import (
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"strconv"
	"time"
)

type File struct {
	Name     string    `json:"FileName"`
	Path     string    `json:"Path"`
	Size     int64     `json:"Size"`
	UpdateAt time.Time `json:"UpdateAt"`
	FileId   int64     `json:"FileId"`
	Type     int       `json:"Type"`
}

func (f File) CreateTime() time.Time {
	return f.UpdateAt
}

func (f File) GetHash() utils.HashInfo {
	return utils.HashInfo{}
}

func (f File) GetPath() string {
	return f.Path
}

func (f File) GetSize() int64 {
	return f.Size
}

func (f File) GetName() string {
	return f.Name
}

func (f File) ModTime() time.Time {
	return f.UpdateAt
}

func (f File) IsDir() bool {
	return f.Type == 1
}

func (f File) GetID() string {
	return strconv.FormatInt(f.FileId, 10)
}
