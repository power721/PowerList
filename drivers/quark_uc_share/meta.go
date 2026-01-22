package quark_uc_share

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
	OrderBy        string `json:"order_by" type:"select" options:"file_type,file_name,updated_at" default:"file_name"`
	OrderDirection string `json:"order_direction" type:"select" options:"asc,desc" default:"asc"`
}

type Conf struct {
	ua      string
	referer string
	api     string
	pr      string
}

const baseId = 20000

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &QuarkUCShare{
			config: driver.Config{
				Name:              "QuarkShare",
				DefaultRoot:       "0",
				NoOverwriteUpload: true,
			},
			conf: Conf{
				ua:      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) quark-cloud-drive/2.5.20 Chrome/100.0.4896.160 Electron/18.3.5.4-b478491100 Safari/537.36 Channel/pckk_other_ch",
				referer: "https://pan.quark.cn",
				api:     "https://drive.quark.cn/1/clouddrive",
				pr:      "ucpro",
			},
		}
	})
	op.RegisterDriver(func() driver.Driver {
		return &QuarkUCShare{
			config: driver.Config{
				Name:              "UCShare",
				DefaultRoot:       "0",
				NoOverwriteUpload: true,
			},
			conf: Conf{
				ua:      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) uc-cloud-drive/2.5.20 Chrome/100.0.4896.160 Electron/18.3.5.4-b478491100 Safari/537.36 Channel/pckk_other_ch",
				referer: "https://drive.uc.cn",
				api:     "https://pc-api.uc.cn/1/clouddrive",
				pr:      "UCBrowser",
			},
		}
	})
	op.RegisterValidateFunc(func() error {
		return validateQuarkShares()
	})
	op.RegisterValidateFunc(func() error {
		return validateUcShares()
	})
}

func validateQuarkShares() error {
	storages := op.GetStorages("QuarkShare")
	log.Infof("validate %v Quark shares", len(storages))
	for _, storage := range storages {
		driver := storage.(*QuarkUCShare)
		if driver.ID < baseId {
			continue
		}
		err := driver.Validate()
		if err != nil {
			log.Warnf("[%v] 夸克分享错误: %v", driver.ID, err)
			driver.GetStorage().SetStatus(err.Error())
			op.MustSaveDriverStorage(driver)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

func validateUcShares() error {
	storages := op.GetStorages("UCShare")
	log.Infof("validate %v UC shares", len(storages))
	for _, storage := range storages {
		driver := storage.(*QuarkUCShare)
		if driver.ID < baseId {
			continue
		}
		err := driver.Validate()
		if err != nil {
			log.Warnf("[%v] UC分享错误: %v", driver.ID, err)
			driver.GetStorage().SetStatus(err.Error())
			op.MustSaveDriverStorage(driver)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}
