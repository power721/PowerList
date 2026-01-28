package _139_grouplink

import (
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
)

type Addition struct {
	ShareId  string `json:"shareId" required:"true"`
	SharePwd string `json:"sharePwd" required:"true"`
	RootID   string `json:"rootId" default:"root"`
}

var config = driver.Config{
	Name:        "139GroupLink",
	NoUpload:    true,
	NoOverwriteUpload: true,
	DefaultRoot: "root",
}

type Yun139GroupLink struct {
	Storage model.Storage
	Addition
	UserDomainId string 
}

func (d *Yun139GroupLink) GetAddition() driver.Additional { return &d.Addition }
func (d *Yun139GroupLink) Config() driver.Config { return config }
func (d *Yun139GroupLink) GetStorage() *model.Storage { return &d.Storage }
func (d *Yun139GroupLink) SetStorage(s model.Storage) { d.Storage = s }
func (d *Yun139GroupLink) GetRootId() string { return d.RootID }

func (d *Yun139GroupLink) GetDownloadUrl(fileId string) (string, error) {
	return d.getDownloadUrl(fileId)
}

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &Yun139GroupLink{}
	})
}
