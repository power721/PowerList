package quark

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/go-resty/resty/v2"
)

const (
	quarkGrowthProductionURL = "https://drive-m.quark.cn/1/clouddrive"
	quarkCheckinMessageLimit = 256
	quarkCheckinInterval     = 24 * time.Hour
)

var (
	quarkGrowthBaseURL = quarkGrowthProductionURL
	quarkGrowthClient  = func() *resty.Client { return base.RestyClient }
)

type quarkGrowthInfoResponse struct {
	Resp
	Data *struct {
		CapSign *struct {
			SignDaily       bool  `json:"sign_daily"`
			SignDailyReward int64 `json:"sign_daily_reward"`
			SignProgress    int   `json:"sign_progress"`
			SignTarget      int   `json:"sign_target"`
		} `json:"cap_sign"`
	} `json:"data"`
}

type quarkGrowthSignResponse struct {
	Resp
	Data *struct {
		SignDailyReward int64 `json:"sign_daily_reward"`
	} `json:"data"`
}

type quarkCheckinResult struct {
	AlreadySigned bool
	RewardMiB     int64
	Progress      int
	Target        int
}

func safeQuarkCheckinMessage(message, cookie string) string {
	message = strings.TrimSpace(message)
	if cookie != "" {
		message = strings.ReplaceAll(message, cookie, "[redacted]")
	}
	if len(message) > quarkCheckinMessageLimit {
		message = message[:quarkCheckinMessageLimit] + "..."
	}
	return message
}

func (d *QuarkOrUC) growthRequest(pathname, method string, callback base.ReqCallback, result any) error {
	client := quarkGrowthClient()
	if client == nil {
		return errors.New("Quark check-in HTTP client is not initialized")
	}
	req := client.R().SetHeaders(map[string]string{
		"Content-Type": "application/json",
		"Cookie":       d.Cookie,
	}).SetQueryParams(map[string]string{
		"pr":           "ucpro",
		"fr":           "pc",
		"uc_param_str": "",
	}).SetResult(result)
	var apiErr Resp
	req.SetError(&apiErr)
	if callback != nil {
		callback(req)
	}
	res, err := req.Execute(method, quarkGrowthBaseURL+pathname)
	if err != nil {
		return fmt.Errorf("Quark check-in request failed (%T)", err)
	}
	if res == nil {
		return errors.New("Quark check-in returned no response")
	}
	if res.IsError() {
		message := safeQuarkCheckinMessage(apiErr.Message, d.Cookie)
		return fmt.Errorf("Quark check-in returned HTTP %d: %s", res.StatusCode(), message)
	}
	return nil
}

func (d *QuarkOrUC) checkin() (quarkCheckinResult, error) {
	var info quarkGrowthInfoResponse
	if err := d.growthRequest("/capacity/growth/info", http.MethodGet, nil, &info); err != nil {
		return quarkCheckinResult{}, err
	}
	if info.Status >= http.StatusBadRequest || info.Code != 0 {
		return quarkCheckinResult{}, fmt.Errorf("Quark growth info rejected: %s", safeQuarkCheckinMessage(info.Message, d.Cookie))
	}
	if info.Data == nil || info.Data.CapSign == nil {
		return quarkCheckinResult{}, errors.New("Quark growth info omitted cap_sign")
	}
	capSign := info.Data.CapSign
	if capSign.SignDaily {
		return quarkCheckinResult{
			AlreadySigned: true,
			RewardMiB:     capSign.SignDailyReward / (1024 * 1024),
			Progress:      capSign.SignProgress,
			Target:        capSign.SignTarget,
		}, nil
	}

	var sign quarkGrowthSignResponse
	if err := d.growthRequest("/capacity/growth/sign", http.MethodPost, func(req *resty.Request) {
		req.SetBody(base.Json{"sign_cyclic": true})
	}, &sign); err != nil {
		return quarkCheckinResult{}, err
	}
	if sign.Status >= http.StatusBadRequest || sign.Code != 0 {
		return quarkCheckinResult{}, fmt.Errorf("Quark growth sign rejected: %s", safeQuarkCheckinMessage(sign.Message, d.Cookie))
	}
	if sign.Data == nil {
		return quarkCheckinResult{}, errors.New("Quark growth sign omitted data")
	}
	return quarkCheckinResult{
		RewardMiB: sign.Data.SignDailyReward / (1024 * 1024),
		Progress:  capSign.SignProgress + 1,
		Target:    capSign.SignTarget,
	}, nil
}
