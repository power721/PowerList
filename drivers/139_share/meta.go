package _139_share

import (
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
)

type Addition struct {
	ShareId  string `json:"share_id" required:"true"`
	SharePwd string `json:"share_pwd"`
	driver.RootID
}

var config = driver.Config{
	Name:              "Yun139Share",
	DefaultRoot:       "root",
	NoOverwriteUpload: true,
	NoUpload:          true,
}

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &Yun139Share{}
	})
}
