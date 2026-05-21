package guangyapan_share

import (
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	log "github.com/sirupsen/logrus"
)

type Addition struct {
	driver.RootID
	ShareID          string `json:"share_id" required:"true" help:"光鸭分享ID或完整分享链接"`
	ShareAccessToken string
	DeviceID         string `json:"device_id" help:"Optional custom device id (32 hex chars), auto-generated when empty"`
	PageSize         int    `json:"page_size" type:"number" default:"200"`
	OrderBy          int    `json:"order_by" type:"number" default:"0"`
	SortType         int    `json:"sort_type" type:"number" default:"0"`
}

var config = driver.Config{
	Name:        "GuangYaPanShare",
	NoUpload:    true,
	DefaultRoot: "",
}

const baseID = 20000

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &GuangYaPanShare{}
	})
	op.RegisterValidateFunc(func() error {
		return validateGuangYaPanShares()
	})
}

func validateGuangYaPanShares() error {
	storages := op.GetStorages("GuangYaPanShare")
	log.Infof("validate %v GuangYaPan shares", len(storages))
	for _, storage := range storages {
		share := storage.(*GuangYaPanShare)
		if share.ID < baseID {
			continue
		}
		err := share.Validate()
		if err != nil {
			log.Warnf("[%v] 光鸭分享错误: %v", share.ID, err)
			share.GetStorage().SetStatus(err.Error())
			op.MustSaveDriverStorage(share)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}
