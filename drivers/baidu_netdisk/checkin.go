package baidu_netdisk

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/go-resty/resty/v2"
)

const (
	baiduActivityProductionURL = "https://pan.baidu.com"
	baiduActivityMessageLimit  = 256
)

var (
	baiduActivityBaseURL = baiduActivityProductionURL
	baiduActivityClient  = func() *resty.Client { return base.RestyClient }
)

type baiduActivityData struct {
	Points   *int   `json:"points"`
	Score    *int   `json:"score"`
	AskID    *int64 `json:"ask_id"`
	Answer   *int64 `json:"answer"`
	ErrorMsg string `json:"error_msg"`
	ShowMsg  string `json:"show_msg"`
}

type baiduActivityResponse struct {
	ErrorCode int                `json:"error_code"`
	Errno     int                `json:"errno"`
	ErrorMsg  string             `json:"error_msg"`
	ShowMsg   string             `json:"show_msg"`
	Points    *int               `json:"points"`
	Score     *int               `json:"score"`
	AskID     *int64             `json:"ask_id"`
	Answer    *int64             `json:"answer"`
	Data      *baiduActivityData `json:"data"`
}

type baiduActivityResult struct {
	AlreadyComplete bool
	Points          int
}

func (r baiduActivityResponse) activityMessage() string {
	if r.ErrorMsg != "" {
		return r.ErrorMsg
	}
	if r.ShowMsg != "" {
		return r.ShowMsg
	}
	if r.Data != nil {
		if r.Data.ErrorMsg != "" {
			return r.Data.ErrorMsg
		}
		return r.Data.ShowMsg
	}
	return ""
}

func (r baiduActivityResponse) activityPoints() *int {
	if r.Points != nil {
		return r.Points
	}
	if r.Data != nil {
		return r.Data.Points
	}
	return nil
}

func baiduActivitySecrets(cookie string) []string {
	secrets := []string{cookie}
	for _, part := range strings.Split(cookie, ";") {
		_, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && value != "" {
			secrets = append(secrets, value)
		}
	}
	return secrets
}

func safeBaiduActivityMessage(message, cookie string) string {
	message = strings.TrimSpace(message)
	for _, secret := range baiduActivitySecrets(cookie) {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	if len(message) > baiduActivityMessageLimit {
		message = message[:baiduActivityMessageLimit] + "..."
	}
	return message
}

func containsBaiduActivityKeyword(message string, keywords ...string) bool {
	lower := strings.ToLower(message)
	for _, keyword := range keywords {
		if strings.Contains(lower, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func (d *BaiduNetdisk) activityRequest(pathname string, query map[string]string, result any) error {
	client := baiduActivityClient()
	if client == nil {
		return errors.New("Baidu activity HTTP client is not initialized")
	}
	res, err := client.R().
		SetHeaders(map[string]string{
			"Accept":           "application/json, text/plain, */*",
			"Cookie":           d.Cookie,
			"Referer":          baiduActivityProductionURL + "/wap/svip/growth/task",
			"User-Agent":       "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/112.0.0.0 Safari/537.36",
			"X-Requested-With": "XMLHttpRequest",
		}).
		SetQueryParams(query).
		SetResult(result).
		Execute(http.MethodGet, baiduActivityBaseURL+pathname)
	if err != nil {
		return fmt.Errorf("Baidu activity request failed (%T)", err)
	}
	if res == nil {
		return errors.New("Baidu activity returned no response")
	}
	if res.IsError() {
		return fmt.Errorf("Baidu activity returned HTTP %d", res.StatusCode())
	}
	return nil
}

func (d *BaiduNetdisk) membershipSignin() (baiduActivityResult, error) {
	var response baiduActivityResponse
	err := d.activityRequest("/rest/2.0/membership/level", map[string]string{
		"app_id": "250528",
		"web":    "5",
		"method": "signin",
	}, &response)
	if err != nil {
		return baiduActivityResult{}, err
	}
	if points := response.activityPoints(); points != nil {
		return baiduActivityResult{Points: *points}, nil
	}
	message := safeBaiduActivityMessage(response.activityMessage(), d.Cookie)
	if containsBaiduActivityKeyword(message, "已签到", "重复签到", "not allow") {
		return baiduActivityResult{AlreadyComplete: true}, nil
	}
	if message == "" {
		message = "response omitted points"
	}
	return baiduActivityResult{}, fmt.Errorf("Baidu membership sign-in rejected: %s", message)
}
