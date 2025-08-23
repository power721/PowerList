package _189_share

import (
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
)

type Addition struct {
	ShareId    string `json:"share_id" required:"true"`
	SharePwd   string `json:"share_pwd"`
	ShareToken string
	driver.RootID
}

var config = driver.Config{
	Name:              "189Share",
	OnlyProxy:         false,
	CheckStatus:       false,
	NoOverwriteUpload: false,
}

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &Cloud189Share{}
	})
}
