package thunder_share

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
	Name: "ThunderShare",
}

const baseId = 20000

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &ThunderShare{}
	})
	op.RegisterValidateFunc(func() error {
		return validateThunderShares()
	})
}

func validateThunderShares() error {
	storages := op.GetStorages("ThunderShare")
	log.Infof("validate %v Thunder shares", len(storages))
	for _, storage := range storages {
		driver := storage.(*ThunderShare)
		if driver.ID < baseId {
			continue
		}
		err := driver.Validate()
		if err != nil {
			log.Warnf("[%v] 迅雷分享错误: %v", driver.ID, err)
			driver.GetStorage().SetStatus(err.Error())
			op.MustSaveDriverStorage(driver)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}
