package uc_share

import (
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
)

type Addition struct {
	ShareId    string `json:"share_id" required:"true"`
	SharePwd   string `json:"share_pwd"`
	ShareToken string
	driver.RootID
	OrderBy        string `json:"order_by" type:"select" options:"file_type,file_name,updated_at" default:"file_name"`
	OrderDirection string `json:"order_direction" type:"select" options:"asc,desc" default:"asc"`
}

var config = driver.Config{
	Name:              "UCShare",
	DefaultRoot:       "0",
	NoOverwriteUpload: true,
}

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &UcShare{}
	})
}
