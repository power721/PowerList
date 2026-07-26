package baidu_netdisk

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/pkg/cron"
	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
)

const (
	baiduActivityProductionURL = "https://pan.baidu.com"
	baiduActivityMessageLimit  = 256
	baiduCheckinInterval       = 24 * time.Hour
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

func (r baiduActivityResponse) activityScore() *int {
	if r.Score != nil {
		return r.Score
	}
	if r.Data != nil {
		return r.Data.Score
	}
	return nil
}

func (r baiduActivityResponse) questionValues() (answer, askID *int64) {
	answer, askID = r.Answer, r.AskID
	if r.Data != nil {
		if answer == nil {
			answer = r.Data.Answer
		}
		if askID == nil {
			askID = r.Data.AskID
		}
	}
	return answer, askID
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

func (d *BaiduNetdisk) getDailyQuestion() (answer, askID int64, err error) {
	var response baiduActivityResponse
	err = d.activityRequest("/act/v2/membergrowv2/getdailyquestion", map[string]string{
		"app_id": "250528",
		"web":    "5",
	}, &response)
	if err != nil {
		return 0, 0, err
	}
	answerValue, askIDValue := response.questionValues()
	if answerValue == nil {
		return 0, 0, errors.New("Baidu daily question omitted answer")
	}
	if askIDValue == nil {
		return 0, 0, errors.New("Baidu daily question omitted ask_id")
	}
	return *answerValue, *askIDValue, nil
}

func (d *BaiduNetdisk) answerDailyQuestion(answer, askID int64) (baiduActivityResult, error) {
	var response baiduActivityResponse
	err := d.activityRequest("/act/v2/membergrowv2/answerquestion", map[string]string{
		"app_id": "250528",
		"web":    "5",
		"ask_id": strconv.FormatInt(askID, 10),
		"answer": strconv.FormatInt(answer, 10),
	}, &response)
	if err != nil {
		return baiduActivityResult{}, err
	}
	if score := response.activityScore(); score != nil {
		return baiduActivityResult{Points: *score}, nil
	}
	message := safeBaiduActivityMessage(response.activityMessage(), d.Cookie)
	if containsBaiduActivityKeyword(message, "已回答", "exceeded", "超出", "超限") {
		return baiduActivityResult{AlreadyComplete: true}, nil
	}
	if message == "" {
		message = "response omitted score"
	}
	return baiduActivityResult{}, fmt.Errorf("Baidu daily answer rejected: %s", message)
}

func (d *BaiduNetdisk) logActivityResult(name string, result baiduActivityResult) {
	if result.AlreadyComplete {
		log.Infof("[%d] Baidu %s already complete", d.ID, name)
		return
	}
	log.Infof("[%d] Baidu %s complete: +%d points", d.ID, name, result.Points)
}

func (d *BaiduNetdisk) executeCheckin() {
	result, err := d.membershipSignin()
	if err != nil {
		log.Warnf("[%d] Baidu membership check-in failed: %v", d.ID, err)
	} else {
		d.logActivityResult("membership check-in", result)
	}

	answer, askID, err := d.getDailyQuestion()
	if err != nil {
		log.Warnf("[%d] Baidu daily question failed: %v", d.ID, err)
		return
	}
	result, err = d.answerDailyQuestion(answer, askID)
	if err != nil {
		log.Warnf("[%d] Baidu daily answer failed: %v", d.ID, err)
		return
	}
	d.logActivityResult("daily answer", result)
}

type baiduCheckinScheduler interface {
	Do(func())
	Stop()
}

var (
	newBaiduCheckinScheduler = func(interval time.Duration) baiduCheckinScheduler {
		return cron.NewCron(interval)
	}
	launchBaiduCheckin = func(job func()) { go job() }
)

func (d *BaiduNetdisk) stopCheckin() {
	if d.checkinScheduler != nil {
		d.checkinScheduler.Stop()
		d.checkinScheduler = nil
	}
}

func (d *BaiduNetdisk) startCheckin() {
	d.stopCheckin()
	if !d.AutoCheckin {
		return
	}
	job := d.executeCheckin
	launchBaiduCheckin(job)
	d.checkinScheduler = newBaiduCheckinScheduler(baiduCheckinInterval)
	d.checkinScheduler.Do(job)
}
