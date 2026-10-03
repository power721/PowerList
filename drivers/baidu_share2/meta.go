package baidu_share

import (
	"strings"
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

// 全局熔断:-62/-65 是 IP 级风控,继续打只会加深限流。连续命中即中止本轮并进入冷却,
// 冷却期内整族跳过校验,保留各分享上一次的真实状态(不回写风控文案覆盖已恢复的分享)。
var baiduRateLimitCooldown time.Time

func isBaiduRateLimitErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "-62") || strings.Contains(msg, "-65")
}

func validateBaiduShares() error {
	if time.Now().Before(baiduRateLimitCooldown) {
		log.Infof("skip validate Baidu shares: rate limit cooldown until %v", baiduRateLimitCooldown)
		return nil
	}
	storages := op.GetStorages("BaiduShare2")
	log.Infof("validate %v Baidu shares", len(storages))
	consecutive := 0
	initialDelay := 1 * time.Second
	delay := initialDelay
	for _, storage := range storages {
		driver := storage.(*BaiduShare2)
		if driver.ID < baseId {
			continue
		}
		err := driver.Validate()
		if err != nil {
			log.Warnf("[%v] 百度分享错误: %v", driver.ID, err)
			if isBaiduRateLimitErr(err) {
				consecutive++
				delay *= 2
				if delay > 8*time.Second {
					delay = 8 * time.Second
				}
				if consecutive >= 5 {
					baiduRateLimitCooldown = time.Now().Add(30 * time.Minute)
					log.Warnf("Baidu share validation aborted after %d consecutive rate limits, cooldown 30m", consecutive)
					return nil
				}
			} else {
				consecutive = 0
			}
			driver.GetStorage().SetStatus(err.Error())
			op.MustSaveDriverStorage(driver)
		} else {
			consecutive = 0
			delay = initialDelay
		}
		time.Sleep(delay)
	}
	return nil
}

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &BaiduShare2{}
	})
	op.RegisterValidateFunc(func() error {
		return validateBaiduShares()
	})
}
