package baidu_share

import (
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	log "github.com/sirupsen/logrus"
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

const baseId = 20000

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &BaiduShare2{}
	})
	op.RegisterValidateFunc(func() error {
		return validateBaiduShares()
	})
}

func validateBaiduShares() error {
	storages := op.GetStorages("BaiduShare2")
	log.Infof("validate %v Baidu shares", len(storages))
	for _, storage := range storages {
		driver := storage.(*BaiduShare2)
		if driver.ID < baseId {
			continue
		}
		err := driver.Validate()
		if err != nil {
			log.Warnf("[%v] 百度分享错误: %v", driver.ID, err)
			driver.GetStorage().SetStatus(err.Error())
			op.MustSaveDriverStorage(driver)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil
}
