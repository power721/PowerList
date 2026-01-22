package _123Share

import (
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	log "github.com/sirupsen/logrus"
)

type Addition struct {
	ShareKey string `json:"share_id" required:"true"`
	SharePwd string `json:"share_pwd"`
	driver.RootID
	//OrderBy        string `json:"order_by" type:"select" options:"file_name,size,update_at" default:"file_name"`
	//OrderDirection string `json:"order_direction" type:"select" options:"asc,desc" default:"asc"`
	AccessToken string `json:"accesstoken" type:"text"`
}

var config = driver.Config{
	Name:        "123PanShare",
	LocalSort:   true,
	NoUpload:    true,
	DefaultRoot: "0",
	PreferProxy: true,
}

const baseId = 20000

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &Pan123Share{}
	})
	op.RegisterValidateFunc(func() error {
		return validate123Shares()
	})
}

func validate123Shares() error {
	storages := op.GetStorages("123PanShare")
	log.Infof("validate %v 123 shares", len(storages))
	for _, storage := range storages {
		driver := storage.(*Pan123Share)
		if driver.ID < baseId {
			continue
		}
		err := driver.Validate()
		if err != nil {
			log.Warnf("[%v] 123分享错误: %v", driver.ID, err)
			driver.GetStorage().SetStatus(err.Error())
			op.MustSaveDriverStorage(driver)
		}
		time.Sleep(800 * time.Millisecond)
	}
	return nil
}
