package _189_share

import (
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	log "github.com/sirupsen/logrus"
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

const baseId = 20000

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &Cloud189Share{}
	})
	op.RegisterValidateFunc(func() error {
		return validate189Shares()
	})
}

func validate189Shares() error {
	storages := op.GetStorages("189Share")
	log.Infof("validate %v 189 shares", len(storages))
	for _, storage := range storages {
		driver := storage.(*Cloud189Share)
		if driver.ID < baseId {
			continue
		}
		err := driver.Validate()
		if err != nil {
			log.Warnf("[%v] 天翼分享错误: %v", driver.ID, err)
			driver.GetStorage().SetStatus(err.Error())
			op.MustSaveDriverStorage(driver)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}
