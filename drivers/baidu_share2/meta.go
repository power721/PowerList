package baidu_share

import (
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
)

type Addition struct {
	driver.RootPath
	Surl string `json:"surl"`
	Pwd  string `json:"pwd"`
}

var config = driver.Config{
	Name:        "BaiduShare2",
	LocalSort:   true,
	NoUpload:    true,
	DefaultRoot: "/",
	Alert:       "",
}

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &BaiduShare2{}
	})
}
