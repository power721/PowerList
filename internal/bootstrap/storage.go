package bootstrap

import (
	"context"
	_115_share "github.com/OpenListTeam/OpenList/v4/drivers/115_share"
	_123Share "github.com/OpenListTeam/OpenList/v4/drivers/123_share"
	_189_share "github.com/OpenListTeam/OpenList/v4/drivers/189_share"
	"github.com/OpenListTeam/OpenList/v4/drivers/aliyundrive_share2_open"
	baidu_share "github.com/OpenListTeam/OpenList/v4/drivers/baidu_share2"
	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/drivers/quark_uc_share"
	"github.com/OpenListTeam/OpenList/v4/drivers/thunder_share"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	log "github.com/sirupsen/logrus"
	"strconv"
	"sync"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/db"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

const baseId = 20000

func LoadStorages() {
	storages, err := db.GetEnabledStorages()
	if err != nil {
		utils.Log.Fatalf("failed get enabled storages: %+v", err)
	}
	log.Infof("total %v enabled storages", len(storages))
	conf.LazyLoad = setting.GetBool("ali_lazy_load")

	go func(storages []model.Storage) {
		for i := range storages {
			storage := storages[i]
			err := op.LoadStorage(context.Background(), storage)
			if err != nil {
				log.Errorf("[%d] failed get enabled storages [%s], %+v",
					i+1, storage.MountPath, err)
			} else {
				log.Infof("[%d] success load storage: [%s], driver: [%s]",
					i+1, storage.MountPath, storage.Driver)
			}
		}
		conf.SendStoragesLoadedSignal()
		log.Infof("=== load storages completed ===")
		if conf.LazyLoad {
			syncStatus(2)
			go Validate()
		} else {
			syncStatus(3)
		}
	}(storages)
}

func syncStatus(code int) {
	url := "http://127.0.0.1:4567/api/alist/status?code=" + strconv.Itoa(code)
	_, err := base.RestyClient.R().
		SetHeader("X-API-KEY", setting.GetStr("atv_api_key")).
		Post(url)
	if err != nil {
		log.Warnf("sync status failed: %v", err)
	}
}

func Validate() {
	var wg sync.WaitGroup
	wg.Add(1)
	go validateAliShares(&wg)
	wg.Add(1)
	go validate189Shares(&wg)
	wg.Add(1)
	go validate123Shares(&wg)
	wg.Add(1)
	go validate115Shares(&wg)
	wg.Add(1)
	go validateQuarkShares(&wg)
	wg.Add(1)
	go validateUcShares(&wg)
	wg.Add(1)
	go validateThunderShares(&wg)
	wg.Add(1)
	go validateBaiduShares(&wg)
	wg.Wait()
	log.Infof("=== validate storages completed ===")
	syncStatus(3)
}

func validateAliShares(wg *sync.WaitGroup) {
	defer wg.Done()
	storages := op.GetStorages("AliyunShare")
	log.Infof("validate %v ali shares", len(storages))
	for _, storage := range storages {
		ali := storage.(*aliyundrive_share2_open.AliyundriveShare2Open)
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
}

func validate189Shares(wg *sync.WaitGroup) {
	defer wg.Done()
	storages := op.GetStorages("189Share")
	log.Infof("validate %v 189 shares", len(storages))
	for _, storage := range storages {
		driver := storage.(*_189_share.Cloud189Share)
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
}

func validate123Shares(wg *sync.WaitGroup) {
	defer wg.Done()
	storages := op.GetStorages("123PanShare")
	log.Infof("validate %v 123 shares", len(storages))
	for _, storage := range storages {
		driver := storage.(*_123Share.Pan123Share)
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
}

func validate115Shares(wg *sync.WaitGroup) {
	defer wg.Done()
	storages := op.GetStorages("115 Share")
	log.Infof("validate %v 115 shares", len(storages))
	for _, storage := range storages {
		driver := storage.(*_115_share.Pan115Share)
		if driver.ID < baseId {
			continue
		}
		err := driver.Validate()
		if err != nil {
			log.Warnf("[%v] 115分享错误: %v", driver.ID, err)
			driver.GetStorage().SetStatus(err.Error())
			op.MustSaveDriverStorage(driver)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func validateQuarkShares(wg *sync.WaitGroup) {
	defer wg.Done()
	storages := op.GetStorages("QuarkShare")
	log.Infof("validate %v Quark shares", len(storages))
	for _, storage := range storages {
		driver := storage.(*quark_uc_share.QuarkUCShare)
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
}

func validateUcShares(wg *sync.WaitGroup) {
	defer wg.Done()
	storages := op.GetStorages("UCShare")
	log.Infof("validate %v UC shares", len(storages))
	for _, storage := range storages {
		driver := storage.(*quark_uc_share.QuarkUCShare)
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
}

func validateThunderShares(wg *sync.WaitGroup) {
	defer wg.Done()
	storages := op.GetStorages("ThunderShare")
	log.Infof("validate %v Thunder shares", len(storages))
	for _, storage := range storages {
		driver := storage.(*thunder_share.ThunderShare)
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
}

func validateBaiduShares(wg *sync.WaitGroup) {
	defer wg.Done()
	storages := op.GetStorages("BaiduShare2")
	log.Infof("validate %v Baidu shares", len(storages))
	for _, storage := range storages {
		driver := storage.(*baidu_share.BaiduShare2)
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
}
