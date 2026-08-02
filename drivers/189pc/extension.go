package _189pc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/casfile"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	"github.com/OpenListTeam/OpenList/v4/internal/stream"
	"github.com/OpenListTeam/OpenList/v4/pkg/cron"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
)

var directLinkObj = func(ctx context.Context, y *Cloud189PC, obj model.Obj) (*model.Link, error) {
	return y.directLink(ctx, obj)
}

var removeResolvedTempObj = func(ctx context.Context, y *Cloud189PC, obj model.Obj) error {
	return y.Remove(ctx, obj)
}

var clearRecycleAfterRemove = func(ctx context.Context, y *Cloud189PC, obj model.Obj) error {
	_, err := y.CreateBatchTask("CLEAR_RECYCLE", y.FamilyID, "", nil, BatchTaskInfo{
		FileId:   obj.GetID(),
		FileName: obj.GetName(),
		IsFolder: 0,
	})
	return err
}

var getDeleteDelaySeconds = func() int {
	return setting.GetInt(conf.DeleteDelayTime, 900)
}

var findResolvedCASFileByName = func(ctx context.Context, y *Cloud189PC, name string, folderID string) (model.Obj, error) {
	return y.findFileByName(ctx, name, folderID, y.isFamily())
}

var scheduleResolvedTempCleanup = func(ctx context.Context, y *Cloud189PC, obj model.Obj) {
	y.scheduleDelayedResolvedTempCleanup(ctx, obj)
}

var openTransferredCASStream = func(ctx context.Context, y *Cloud189PC, obj model.Obj) (model.FileStreamer, error) {
	link, err := directLinkObj(ctx, y, obj)
	if err != nil {
		return nil, err
	}
	casStream, err := stream.NewSeekableStream(&stream.FileStream{
		Ctx: ctx,
		Obj: obj,
	}, link)
	if err != nil {
		return nil, err
	}
	return casStream, nil
}

var readTransferredCASInfo = func(file model.FileStreamer) (*casfile.Info, error) {
	return readCASRestoreInfo(file)
}

var restoreTransferredCASFromInfo = func(ctx context.Context, y *Cloud189PC, dstDir model.Obj, casFileName string, info *casfile.Info) (model.Obj, error) {
	log.Debugf("restoreSourceFromCASInfo: %+v to directory %+v", info, dstDir)
	return y.restoreSourceFromCASInfo(ctx, dstDir, casFileName, info)
}

var restoreTransferredCASAndLink = func(ctx context.Context, y *Cloud189PC, obj model.Obj) (*model.Link, model.Obj, error) {
	log.Infof("[%v] resolving transferred .cas file %s(%s)", y.ID, obj.GetName(), obj.GetID())
	casStream, err := openTransferredCASStream(ctx, y, obj)
	if err != nil {
		log.Warnf("[%v] failed to open transferred .cas stream %s(%s): %v", y.ID, obj.GetName(), obj.GetID(), err)
		return nil, nil, err
	}
	defer casStream.Close()

	info, err := readTransferredCASInfo(casStream)
	if err != nil {
		log.Warnf("[%v] failed to read transferred .cas metadata %s(%s): %v", y.ID, obj.GetName(), obj.GetID(), err)
		return nil, nil, err
	}

	// Force payload-name semantics for this restore path, regardless of the driver's config.
	// Important: do not copy Cloud189PC by value, because it contains sync.Map which must not be copied after first use.
	forcedDriver := cloneDriverForCASRestore(y)
	forcedDriver.RestoreSourceUseCurrentName = false

	dstDir := &model.Object{
		ID:       y.TempDirId,
		Name:     conf.TempDirName,
		IsFolder: true,
	}
	if y.FamilyID != "" {
		dstDir = &model.Object{
			ID:       y.familyTransferFolder.GetID(),
			Name:     y.familyTransferFolder.GetName(),
			IsFolder: true,
		}
		forcedDriver.Type = "family"
	}
	log.Infof("[%v] restoring transferred .cas %s(%s) into %s(%s) as payload %q", y.ID, obj.GetName(), obj.GetID(), dstDir.GetName(), dstDir.GetID(), info.Name)
	log.Debugf("restore to %v %v", forcedDriver.Type, y.FamilyID)
	restoredObj, err := restoreTransferredCASFromInfo(ctx, forcedDriver, dstDir, obj.GetName(), info)
	if err != nil {
		log.Warnf("[%v] failed to restore transferred .cas %s(%s): %v", y.ID, obj.GetName(), obj.GetID(), err)
		return nil, nil, err
	}
	log.Infof("[%v] restored transferred .cas to %s(%s)", y.ID, restoredObj.GetName(), restoredObj.GetID())
	log.Debugf("directLinkObj: %+v", restoredObj)
	link, err := directLinkObj(ctx, forcedDriver, restoredObj)
	if err != nil {
		log.Warnf("[%v] failed to link restored transferred object %s(%s): %v", y.ID, restoredObj.GetName(), restoredObj.GetID(), err)
		return nil, nil, err
	}
	return link, restoredObj, nil
}

func collectTransferredShareCandidates(files []model.Obj, targetName string) (model.Obj, []string) {
	candidates := make([]string, 0, len(files))
	var matched model.Obj
	for _, file := range files {
		candidates = append(candidates, fmt.Sprintf("%s(%s)", file.GetName(), file.GetID()))
		if matched == nil && file.GetName() == targetName {
			matched = file
		}
	}
	return matched, candidates
}

func cloneDriverForCASRestore(y *Cloud189PC) *Cloud189PC {
	// Positional construction keeps this clone in lockstep with Cloud189PC's field list
	// while still leaving sync.Map at its zero value (sync.Map must not be copied).
	return &Cloud189PC{
		y.Storage,
		y.Addition,
		y.identity,
		y.client,
		y.loginParam,
		y.qrcodeParam,
		y.tokenInfo,
		y.uploadThread,
		y.familyTransferFolder,
		y.cleanFamilyTransferFile,
		y.storageConfig,
		y.ref,
		y.TempDirId,
		y.cron,
		y.client2,
		sync.Map{},
	}
}

func (y *Cloud189PC) resolveTransferredShareFile(ctx context.Context, transferFile model.Obj) (*model.Link, model.Obj, error) {
	if strings.HasSuffix(strings.ToLower(transferFile.GetName()), ".cas") {
		return restoreTransferredCASAndLink(ctx, y, transferFile)
	}
	link, err := directLinkObj(ctx, y, transferFile)
	if err != nil {
		return nil, nil, err
	}
	return link, transferFile, nil
}

func (y *Cloud189PC) casRestoreTempDir() model.Obj {
	if y.FamilyID != "" && y.familyTransferFolder != nil {
		return &model.Object{
			ID:       y.familyTransferFolder.GetID(),
			Name:     y.familyTransferFolder.GetName(),
			IsFolder: true,
		}
	}
	return &model.Object{
		ID:       y.TempDirId,
		Name:     conf.TempDirName,
		IsFolder: true,
	}
}

func (y *Cloud189PC) resolveCASInfoForPlayback(ctx context.Context, casFileName string, info *casfile.Info) (*model.Link, model.Obj, *Cloud189PC, error) {
	forcedDriver := cloneDriverForCASRestore(y)
	forcedDriver.RestoreSourceUseCurrentName = false
	if y.FamilyID != "" {
		forcedDriver.Type = "family"
	}

	dstDir := forcedDriver.casRestoreTempDir()
	restoredName, err := forcedDriver.resolveRestoreSourceName(casFileName, info)
	if err != nil {
		return nil, nil, nil, err
	}

	resolvedObj, err := findResolvedCASFileByName(ctx, forcedDriver, restoredName, dstDir.GetID())
	if err != nil {
		if !errs.IsObjectNotFound(err) {
			return nil, nil, nil, err
		}
		resolvedObj, err = restoreTransferredCASFromInfo(ctx, forcedDriver, dstDir, casFileName, info)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	link, err := directLinkObj(ctx, forcedDriver, resolvedObj)
	if err != nil {
		return nil, nil, nil, err
	}
	return link, resolvedObj, forcedDriver, nil
}

func (y *Cloud189PC) RestoreCASForPlayback(ctx context.Context, casFileName string, info *casfile.Info) (*model.Link, error) {
	link, cleanupTarget, cleanupDriver, err := y.resolveCASInfoForPlayback(ctx, casFileName, info)
	if err != nil {
		return nil, err
	}
	scheduleResolvedTempCleanup(context.WithoutCancel(ctx), cleanupDriver, cleanupTarget)
	return link, nil
}

func (y *Cloud189PC) resolveExistingCASFile(ctx context.Context, casFile model.Obj) (*model.Link, model.Obj, error) {
	casStream, err := openTransferredCASStream(ctx, y, casFile)
	if err != nil {
		return nil, nil, err
	}
	defer casStream.Close()

	info, err := readTransferredCASInfo(casStream)
	if err != nil {
		return nil, nil, err
	}

	link, cleanupTarget, _, err := y.resolveCASInfoForPlayback(ctx, casFile.GetName(), info)
	return link, cleanupTarget, err
}

func (y *Cloud189PC) scheduleDelayedResolvedTempCleanup(ctx context.Context, cleanupTarget model.Obj) {
	delaySeconds := getDeleteDelaySeconds()
	if delaySeconds == 0 || cleanupTarget == nil {
		return
	}
	go func() {
		time.Sleep(time.Duration(delaySeconds) * time.Second)

		cleanupName := cleanupTarget.GetName()
		cleanupID := cleanupTarget.GetID()
		log.Infof("[%v] Delete 189 temp file: %v %v", y.ID, cleanupID, cleanupName)
		removeErr := removeResolvedTempObj(ctx, y, cleanupTarget)
		if removeErr != nil {
			log.Infof("[%v] 天翼云盘删除文件:%s失败: %v", y.ID, cleanupName, removeErr)
			return
		}
		log.Debugf("[%v] 已删除天翼云盘下的文件: %v", y.ID, cleanupName)
		if removeErr = clearRecycleAfterRemove(ctx, y, cleanupTarget); removeErr != nil {
			log.Infof("[%v] 天翼云盘清除回收站失败: %v", y.ID, removeErr)
		} else {
			log.Debugf("[%v] 天翼云盘清除回收站完成", y.ID)
		}
	}()
}

func (y *Cloud189PC) createFamilyTempDir() error {
	var rootFolder Cloud189Folder
	_, err := y.post(API_URL+"/family/file/createFolder.action", func(req *resty.Request) {
		req.SetQueryParams(map[string]string{
			"folderName": conf.TempDirName,
			"familyId":   y.FamilyID,
		})
	}, &rootFolder, true)
	if err != nil {
		return err
	}
	y.familyTransferFolder = &rootFolder

	log.Info("189Cloud family temp folder id: ", rootFolder.GetID())
	return nil
}

func (y *Cloud189PC) createTempDir(ctx context.Context) error {
	if y.FamilyID != "" {
		y.createFamilyTempDir()
	}

	dir := &Cloud189File{
		ID: "-11",
	}
	_, err := y.MakeDir(ctx, dir, conf.TempDirName)
	if err != nil {
		log.Warnf("create temp dir failed: %v", err)
	}

	files, err := y.getFiles(ctx, y.TempDirId, false)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.GetName() == conf.TempDirName {
			y.TempDirId = file.GetID()
			break
		}
	}

	log.Info("189Cloud temp folder id: ", y.TempDirId)
	return nil
}

const (
	vipActivityID      = "ACT2025VIP2T"
	vipPrizeID         = "2T_2025VIP"
	greenActivityID    = "ACT2024cztx"
	greenActivityPage  = "https://m.cloud.189.cn/zt/2024/green-task-system/index.html?uxChannel=10021132000"
	maxActivityMessage = 256
)

var (
	checkinAPIBaseURL    = API_URL
	checkinMarketBaseURL = "https://m.cloud.189.cn"
)

type checkinMarketResponse struct {
	Code    String `json:"code"`
	Message string `json:"message"`
	Msg     string `json:"msg"`
}

func activityResponseError(operation string, res *resty.Response, err error) error {
	if err != nil {
		return fmt.Errorf("%s request failed (%T)", operation, err)
	}
	if res == nil {
		return fmt.Errorf("%s returned no response", operation)
	}
	if res.IsError() {
		return fmt.Errorf("%s returned HTTP %d", operation, res.StatusCode())
	}
	return nil
}

func safeActivityMessage(message string, secrets ...string) string {
	message = strings.TrimSpace(message)
	for _, secret := range secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	if len(message) > maxActivityMessage {
		message = message[:maxActivityMessage] + "..."
	}
	return message
}

func activitySecrets(token *AppSessionResp) []string {
	if token == nil {
		return nil
	}
	return []string{token.SessionKey, token.FamilySessionKey, token.AccessToken}
}

func marketResponseMessage(resp checkinMarketResponse) string {
	if resp.Message != "" {
		return resp.Message
	}
	return resp.Msg
}

func alreadyCompletedActivity(message string) bool {
	return strings.Contains(message, "已领取") ||
		strings.Contains(message, "已完成") ||
		strings.Contains(message, "已签到") ||
		strings.Contains(message, "重复")
}

func (y *Cloud189PC) claimVIPSpace() error {
	token := y.getTokenInfo()
	if token == nil || token.SessionKey == "" {
		return errors.New("VIP space claim requires a session key")
	}

	var result checkinMarketResponse
	res, err := y.getClient().R().
		SetResult(&result).
		SetQueryParams(map[string]string{
			"noCache":    strconv.FormatInt(time.Now().UnixMilli(), 10),
			"activityId": vipActivityID,
			"sessionKey": token.SessionKey,
			"prizeId":    vipPrizeID,
		}).
		Get(checkinMarketBaseURL + "/market/drawTargetSpace.action")
	if err := activityResponseError("VIP space claim", res, err); err != nil {
		return err
	}

	message := safeActivityMessage(marketResponseMessage(result), activitySecrets(token)...)
	if string(result.Code) == "0" || alreadyCompletedActivity(message) {
		log.Infof("[%v] VIP monthly space: %s", y.ID, message)
		return nil
	}
	return fmt.Errorf("VIP monthly space rejected: %s", message)
}

type greenSignResponse struct {
	Result  *bool  `json:"result"`
	Message string `json:"message"`
	Msg     string `json:"msg"`
}

type greenSignInfoResponse struct {
	Data *String `json:"data"`
}

func (y *Cloud189PC) newGreenActivityClient() (*resty.Client, error) {
	token := y.getTokenInfo()
	if token == nil || token.SessionKey == "" || token.AccessToken == "" {
		return nil, errors.New("green activity requires session and access tokens")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create green activity cookie jar: %w", err)
	}
	baseClient := y.getClient()
	if baseClient == nil {
		return nil, errors.New("green activity requires an initialized HTTP client")
	}
	client := baseClient.Clone().
		SetCookieJar(jar).
		SetHeaders(map[string]string{
			"Accept":          "application/json, text/plain, */*",
			"Accept-Language": "zh-CN,zh-Hans;q=0.9",
			"Referer":         greenActivityPage,
		})
	res, requestErr := client.R().SetQueryParams(map[string]string{
		"sessionKey":   token.SessionKey,
		"sessionKeyFm": token.FamilySessionKey,
		"eAccessToken": token.AccessToken,
		"redirectUrl":  greenActivityPage,
		"rand":         "6947_" + strconv.FormatInt(time.Now().Unix(), 10),
	}).Get(checkinMarketBaseURL + "/ssoLoginMerge.action")
	if err := activityResponseError("green activity SSO", res, requestErr); err != nil {
		return nil, err
	}
	return client, nil
}

func (y *Cloud189PC) greenDailyCheckin(client *resty.Client) error {
	token := y.getTokenInfo()
	var signResult greenSignResponse
	res, requestErr := client.R().
		SetResult(&signResult).
		SetQueryParams(map[string]string{
			"sessionKey": token.SessionKey,
			"activityId": greenActivityID,
		}).
		Get(checkinMarketBaseURL + "/market/signInNew.action")
	var resultErrs []error
	if err := activityResponseError("green daily check-in", res, requestErr); err != nil {
		resultErrs = append(resultErrs, err)
	} else if signResult.Result == nil {
		resultErrs = append(resultErrs, errors.New("green daily check-in response omitted result"))
	} else if !*signResult.Result {
		message := safeActivityMessage(marketResponseMessage(checkinMarketResponse{
			Message: signResult.Message,
			Msg:     signResult.Msg,
		}), activitySecrets(token)...)
		if alreadyCompletedActivity(message) {
			log.Infof("[%v] green daily check-in: %s", y.ID, message)
		} else {
			resultErrs = append(resultErrs, fmt.Errorf("green daily check-in rejected: %s", message))
		}
	} else {
		log.Infof("[%v] green daily check-in succeeded", y.ID)
	}

	var info greenSignInfoResponse
	res, requestErr = client.R().
		SetResult(&info).
		SetQueryParams(map[string]string{
			"sessionKey": token.SessionKey,
			"activityId": greenActivityID,
		}).
		Get(checkinMarketBaseURL + "/market/signInNewInfo.action")
	if err := activityResponseError("green sign-in info", res, requestErr); err != nil {
		resultErrs = append(resultErrs, err)
	} else if info.Data == nil {
		resultErrs = append(resultErrs, errors.New("green sign-in info response omitted data"))
	} else {
		day := safeActivityMessage(string(*info.Data), activitySecrets(token)...)
		log.Infof("[%v] green activity weekly sign-in day: %s", y.ID, day)
	}
	return errors.Join(resultErrs...)
}

type greenTask struct {
	TaskID   String `json:"taskId"`
	TaskName string `json:"taskName"`
	Status   bool   `json:"status"`
}

type greenTaskListResponse struct {
	Data *[]greenTask `json:"data"`
}

type greenTaskResult struct {
	Data *bool `json:"data"`
}

type greenScoreResponse struct {
	Data *struct {
		UserScore *String `json:"userScore"`
	} `json:"data"`
}

func (y *Cloud189PC) runGreenTasks(client *resty.Client, taskType string) error {
	token := y.getTokenInfo()
	if token == nil || token.SessionKey == "" {
		return errors.New("green tasks require a session key")
	}
	var list greenTaskListResponse
	res, requestErr := client.R().
		SetResult(&list).
		SetQueryParams(map[string]string{
			"sessionKey": token.SessionKey,
			"activityId": greenActivityID,
			"random":     strconv.FormatInt(time.Now().UnixNano(), 10),
			"taskType":   taskType,
		}).
		Get(checkinMarketBaseURL + "/market/getGreenTaskList.action")
	if err := activityResponseError("green task list type "+taskType, res, requestErr); err != nil {
		return err
	}
	if list.Data == nil {
		return fmt.Errorf("green task list type %s omitted data", taskType)
	}

	var taskErrs []error
	for _, task := range *list.Data {
		if task.Status {
			continue
		}
		var result greenTaskResult
		taskID := safeActivityMessage(string(task.TaskID), activitySecrets(token)...)
		res, requestErr = client.R().
			SetResult(&result).
			SetFormData(map[string]string{
				"activityId": greenActivityID,
				"sessionKey": token.SessionKey,
				"taskId":     string(task.TaskID),
			}).
			Post(checkinMarketBaseURL + "/market/doGreenTask.action")
		if err := activityResponseError("green task "+taskID, res, requestErr); err != nil {
			taskErrs = append(taskErrs, err)
			continue
		}
		if result.Data == nil {
			taskErrs = append(taskErrs, fmt.Errorf("green task %s omitted result data", taskID))
			continue
		}
		name := safeActivityMessage(task.TaskName, activitySecrets(token)...)
		if *result.Data {
			log.Infof("[%v] green task completed: %s", y.ID, name)
		} else {
			log.Infof("[%v] green task skipped: %s", y.ID, name)
		}
	}
	return errors.Join(taskErrs...)
}

func (y *Cloud189PC) queryGreenScore(client *resty.Client) error {
	token := y.getTokenInfo()
	if token == nil || token.SessionKey == "" {
		return errors.New("green score requires a session key")
	}
	var result greenScoreResponse
	res, requestErr := client.R().
		SetResult(&result).
		SetQueryParams(map[string]string{
			"sessionKey": token.SessionKey,
			"activityId": greenActivityID,
		}).
		Get(checkinMarketBaseURL + "/market/getGreenLevelList.action")
	if err := activityResponseError("green score", res, requestErr); err != nil {
		return err
	}
	if result.Data == nil || result.Data.UserScore == nil {
		return errors.New("green score response omitted userScore")
	}
	score := safeActivityMessage(string(*result.Data.UserScore), activitySecrets(token)...)
	log.Infof("[%v] green energy: %sg", y.ID, score)
	return nil
}

func (y *Cloud189PC) runCheckinStep(name string, step func() error) {
	if err := step(); err != nil {
		log.Warnf("[%v] %s failed: %v", y.ID, name, err)
		return
	}
	log.Infof("[%v] %s completed", y.ID, name)
}

func (y *Cloud189PC) standardCloudCheckin() error {
	_, err := y.get(checkinAPIBaseURL+"/mkt/userSign.action", nil, nil)
	return err
}

func (y *Cloud189PC) drawStandardCheckinPrize() error {
	_, err := y.get(checkinMarketBaseURL+"/v2/drawPrizeMarketDetails.action", func(req *resty.Request) {
		req.SetQueryParams(map[string]string{
			"taskId":     "TASK_SIGNIN",
			"activityId": "ACT_SIGNIN",
		})
	}, nil)
	return err
}

func (y *Cloud189PC) runGreenActivity() error {
	client, err := y.newGreenActivityClient()
	if err != nil {
		return err
	}
	var activityErrs []error
	if err := y.greenDailyCheckin(client); err != nil {
		activityErrs = append(activityErrs, err)
	}
	for _, taskType := range []string{"1", "2", "3"} {
		if err := y.runGreenTasks(client, taskType); err != nil {
			activityErrs = append(activityErrs, err)
		}
	}
	if err := y.queryGreenScore(client); err != nil {
		activityErrs = append(activityErrs, err)
	}
	return errors.Join(activityErrs...)
}

func (y *Cloud189PC) Checkin() {
	if !y.AutoCheckin {
		return
	}

	go y.checkin()
	y.cron = cron.NewCron(time.Hour * 24)
	y.cron.Do(func() {
		y.checkin()
	})
}

func (y *Cloud189PC) checkin() {
	y.runCheckinStep("cloud check-in", y.standardCloudCheckin)
	y.runCheckinStep("check-in prize draw", y.drawStandardCheckinPrize)
	y.runCheckinStep("VIP monthly space", y.claimVIPSpace)
	y.runCheckinStep("green activity", y.runGreenActivity)
}

func (y *Cloud189PC) GetShareLink(shareId int, file model.Obj) (*model.Link, error) {
	if y.Cookie == "" {
		return nil, errors.New("no cookie found")
	}

	url := "https://cloud.189.cn/api/portal/getNewVlcVideoPlayUrl.action"
	res, err := y.client.R().
		SetQueryParams(map[string]string{
			"shareId": strconv.Itoa(shareId),
			"fileId":  file.GetID(),
			"dt":      "1",
			"type":    "4",
		}).
		SetHeader("accept", "application/json;charset=UTF-8").
		SetHeader("cookie", y.Cookie).
		Get(url)

	log.Debugf("[%v] getShareLink result: %s", y.ID, res.String())
	if err != nil {
		log.Warnf("[%v] getShareLink failed: %v", y.ID, err)
	}

	url = utils.Json.Get(res.Body(), "normal", "url").ToString()
	if url != "" {
		res, err = y.client2.R().
			SetQueryParams(map[string]string{
				"shareId": strconv.Itoa(shareId),
				"fileId":  file.GetID(),
				"dt":      "1",
				"type":    "4",
			}).
			SetHeader("accept", "application/json;charset=UTF-8").
			SetHeader("cookie", y.Cookie).
			Get(url)
		newUrl := res.Header().Get("Location")
		if newUrl != "" {
			url = newUrl
		}
		exp := time.Hour
		link := &model.Link{
			Expiration: &exp,
			URL:        url,
			Header: http.Header{
				"User-Agent": []string{base.UserAgent},
			},
			Concurrency: y.Concurrency,
			PartSize:    y.ChunkSize * utils.KB,
		}
		log.Debugf("使用直链播放：%v", url)
		return link, nil
	}

	msg := utils.Json.Get(res.Body(), "errorMsg").ToString()
	return nil, errors.New(msg)
}

func (y *Cloud189PC) Transfer(ctx context.Context, shareId int, fileId string, fileName string) (*model.Link, error) {
	if y.getTokenInfo() == nil {
		return nil, errors.New("no token found")
	}

	other := map[string]string{"shareId": strconv.Itoa(shareId)}

	log.Debug("create share save task")
	resp, err := y.CreateBatchTask("SHARE_SAVE", "", y.TempDirId, other, BatchTaskInfo{
		FileId:   fileId,
		FileName: fileName,
		IsFolder: 0,
	})

	if err != nil && !strings.Contains(err.Error(), "there is a conflict with the target object") {
		return nil, err
	}
	if err != nil {
		taskID := ""
		if resp != nil {
			taskID = resp.TaskID
		}
		log.Warnf("[%v] SHARE_SAVE create task conflict for %s(%s) into temp dir %s, task_id=%s: %v", y.ID, fileName, fileId, y.TempDirId, taskID, err)
	}

	log.Debug("wait task")
	err = y.WaitBatchTask("SHARE_SAVE", resp.TaskID, time.Second)
	if err != nil && !strings.Contains(err.Error(), "there is a conflict with the target object") {
		return nil, err
	}
	if err != nil {
		log.Warnf("[%v] SHARE_SAVE wait conflict for %s(%s) into temp dir %s, task_id=%s: %v", y.ID, fileName, fileId, y.TempDirId, resp.TaskID, err)
	}

	log.Debug("get files")
	files, err := y.getFiles(ctx, y.TempDirId, false)
	if err != nil {
		return nil, err
	}

	log.Debug("get new file")
	transferFile, candidates := collectTransferredShareCandidates(files, fileName)
	log.Infof("[%v] SHARE_SAVE temp dir %s candidates for %s(%s): %s", y.ID, y.TempDirId, fileName, fileId, strings.Join(candidates, ", "))

	if transferFile == nil || transferFile.GetID() == "" {
		log.Warnf("[%v] SHARE_SAVE did not produce a usable temp object for %s(%s) in temp dir %s", y.ID, fileName, fileId, y.TempDirId)
		return nil, errors.New("文件转存失败")
	}
	log.Infof("[%v] SHARE_SAVE selected temp object %s(%s) for requested file %s(%s)", y.ID, transferFile.GetName(), transferFile.GetID(), fileName, fileId)

	log.Debug("get new file link")
	link, cleanupTarget, err := y.resolveTransferredShareFile(ctx, transferFile)
	if err != nil {
		log.Warnf("[%v] failed to resolve transferred share file %s(%s): %v", y.ID, transferFile.GetName(), transferFile.GetID(), err)
	}

	if cleanupTarget != nil {
		driver := y
		if cleanupTarget != transferFile && y.FamilyID != "" {
			driver = cloneDriverForCASRestore(y)
			driver.Type = "family"
		}
		log.Infof("[%v] Delete 189 temp file %v after %v seconds.", driver.ID, cleanupTarget.GetID(), getDeleteDelaySeconds())
		scheduleResolvedTempCleanup(ctx, driver, cleanupTarget)
	}

	return link, err
}

func RsaEncode(origData []byte, j_rsakey string, hex bool) string {
	publicKey := []byte("-----BEGIN PUBLIC KEY-----\n" + j_rsakey + "\n-----END PUBLIC KEY-----")
	block, _ := pem.Decode(publicKey)
	pubInterface, _ := x509.ParsePKIXPublicKey(block.Bytes)
	pub := pubInterface.(*rsa.PublicKey)
	b, err := rsa.EncryptPKCS1v15(rand.Reader, pub, origData)
	if err != nil {
		log.Errorf("err: %s", err.Error())
	}
	res := base64.StdEncoding.EncodeToString(b)
	if hex {
		return b64tohex(res)
	}
	return res
}

var b64map = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

var BI_RM = "0123456789abcdefghijklmnopqrstuvwxyz"

func int2char(a int) string {
	return strings.Split(BI_RM, "")[a]
}

func b64tohex(a string) string {
	d := ""
	e := 0
	c := 0
	for i := 0; i < len(a); i++ {
		m := strings.Split(a, "")[i]
		if m != "=" {
			v := strings.Index(b64map, m)
			if 0 == e {
				e = 1
				d += int2char(v >> 2)
				c = 3 & v
			} else if 1 == e {
				e = 2
				d += int2char(c<<2 | v>>4)
				c = 15 & v
			} else if 2 == e {
				e = 3
				d += int2char(c)
				d += int2char(v >> 2)
				c = 3 & v
			} else {
				e = 0
				d += int2char(c<<2 | v>>4)
				d += int2char(15 & v)
			}
		}
	}
	if e == 1 {
		d += int2char(c << 2)
	}
	return d
}
