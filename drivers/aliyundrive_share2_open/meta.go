package aliyundrive_share2_open

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
	OrderBy        string `json:"order_by" type:"select" options:"name,size,updated_at,created_at"`
	OrderDirection string `json:"order_direction" type:"select" options:"ASC,DESC"`
}

var config = driver.Config{
	Name:        "AliyunShare",
	NoUpload:    true,
	DefaultRoot: "root",
}

const baseId = 20000

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &AliyundriveShare2Open{
			base: "https://openapi.alipan.com",
		}
	})
	op.RegisterValidateFunc(func() error {
		return validateAliShares()
	})
}

func validateAliShares() error {
	storages := op.GetStorages("AliyunShare")
	log.Infof("validate %v ali shares", len(storages))
	for _, storage := range storages {
		ali := storage.(*AliyundriveShare2Open)
		if ali.ID < baseId {
			continue
		}
		err := ali.Validate()
		if err != nil {
			log.Warnf("[%v] 阿里分享错误: %v", ali.ID, err)
			ali.GetStorage().SetStatus(err.Error())
			op.MustSaveDriverStorage(ali)
		}
		time.Sleep(1500 * time.Millisecond)
	}
	return nil
}
