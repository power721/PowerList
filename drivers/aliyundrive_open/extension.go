package aliyundrive_open

import (
	"context"
	"errors"
	"fmt"
	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	"github.com/OpenListTeam/OpenList/v4/internal/token"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	log "github.com/sirupsen/logrus"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// CanaryHeaderKey CanaryHeaderValue for lifting rate limit restrictions
	CanaryHeaderKey   = "X-Canary"
	CanaryHeaderValue = "client=web,app=share,version=v2.3.1"
)

type ErrorResp struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (d *AliyundriveOpen) Request(uri, method string, callback base.ReqCallback, retry ...bool) ([]byte, error) {
	return d.request(context.Background(), limiterList, uri, method, callback, retry...)
}

func (d *AliyundriveOpen) RefreshAliToken(force bool) error {
	accountId := strconv.Itoa(d.AccountId)
	accessToken := token.GetToken("AccessToken-"+accountId, 7200)
	refreshToken := token.GetToken("RefreshToken-"+accountId, 0)
	log.Debugf("[%v] Ali Token: %v %v %v", d.ID, accountId, accessToken, refreshToken)
	if !force && accessToken != "" && refreshToken != "" {
		d.RefreshToken2, d.AccessToken2 = refreshToken, accessToken
		log.Debugf("[%v] 阿里token已经存在", d.ID)
		return nil
	}
	if refreshToken != "" {
		d.RefreshToken2 = refreshToken
	}

	t := time.Now()
	url := "https://auth.alipan.com/v2/account/token"
	log.Infof("[%v] refresh ali token: %v", d.ID, url)
	var resp base.TokenResp
	var e ErrorResp
	_, err := base.RestyClient.R().
		SetBody(base.Json{"refresh_token": d.RefreshToken2, "grant_type": "refresh_token"}).
		SetResult(&resp).
		SetError(&e).
		Post(url)
	if err != nil {
		return err
	}
	if e.Code != "" {
		return fmt.Errorf("failed to refresh ali token: %s", e.Message)
	}
	d.RefreshToken2, d.AccessToken2 = resp.RefreshToken, resp.AccessToken

	d.SaveToken(t)

	return nil
}

func (d *AliyundriveOpen) SaveToken(t time.Time) {
	accountId := strconv.Itoa(d.AccountId)
	item := &model.Token{
		Key:       "AccessToken-" + accountId,
		Value:     d.AccessToken2,
		AccountId: d.AccountId,
		Modified:  t,
	}

	err := token.SaveToken(item)
	if err != nil {
		log.Warnf("[%v] save AccessToken failed: %v", d.ID, err)
	}

	if d.RefreshToken2 == "" {
		return
	}

	item = &model.Token{
		Key:       "RefreshToken-" + accountId,
		Value:     d.RefreshToken2,
		AccountId: d.AccountId,
		Modified:  t,
	}

	err = token.SaveToken(item)
	if err != nil {
		log.Warnf("[%v] save RefreshToken failed: %v", d.ID, err)
	}

	data := base.Json{
		"refreshToken":     d.RefreshToken2,
		"refreshTokenTime": t,
		"accessToken":      d.AccessToken2,
		"accessTokenTime":  t,
	}
	d.syncTokens(accountId, data)
}

func (d *AliyundriveOpen) SaveOpenToken(t time.Time) {
	accountId := strconv.Itoa(d.AccountId)
	item := &model.Token{
		Key:       "AccessTokenOpen-" + accountId,
		Value:     d.AccessToken,
		AccountId: d.AccountId,
		Modified:  t,
	}

	err := token.SaveToken(item)
	if err != nil {
		log.Warnf("[%v] save AccessTokenOpen failed: %v", d.ID, err)
	}

	item = &model.Token{
		Key:       "RefreshTokenOpen-" + accountId,
		Value:     d.RefreshToken,
		AccountId: d.AccountId,
		Modified:  t,
	}

	err = token.SaveToken(item)
	if err != nil {
		log.Warnf("[%v] save RefreshTokenOpen failed: %v", d.ID, err)
	}

	data := base.Json{
		"openToken":           d.RefreshToken,
		"openTokenTime":       t,
		"openAccessToken":     d.AccessToken,
		"openAccessTokenTime": t,
	}
	d.syncTokens(accountId, data)
}

func (d *AliyundriveOpen) syncTokens(id string, data base.Json) {
	url := "http://127.0.0.1:4567/api/ali/accounts/" + id + "/token"
	_, err := base.RestyClient.R().
		SetHeader("X-API-KEY", setting.GetStr("atv_api_key")).
		SetBody(data).
		Post(url)
	if err != nil {
		log.Warnf("[%v] sync tokens failed: %v", d.ID, err)
	}
}

func (d *AliyundriveOpen) createTempDir(ctx context.Context) {
	dir := &model.Object{
		ID:   "root",
		Path: "",
	}

	res, err := d.MakeDir(ctx, dir, conf.TempDirName)

	if err != nil {
		log.Warnf("[%v] 创建阿里缓存文件夹失败: %v", d.ID, err)
	} else {
		d.TempDirId = res.GetID()
	}

	if d.TempDirId == "" {
		d.TempDirId = "root"
	}
	log.Infof("[%v] 阿里缓存文件夹ID： %v", d.ID, d.TempDirId)

	d.cleanTempFolder(ctx)
}

func (d *AliyundriveOpen) cleanTempFolder(ctx context.Context) {
	if d.TempDirId == "root" {
		return
	}

	dir := &model.Object{
		ID:   d.TempDirId,
		Path: "",
	}

	files, err := d.List(ctx, dir, model.ListArgs{})
	if err != nil {
		log.Errorf("[%v] 获取文件列表失败 %v", d.ID, err)
		return
	}

	for _, file := range files {
		log.Infof("[%v] 删除文件 %v %v", d.ID, file.GetName(), file.GetID())
		f := &model.Object{
			ID: file.GetID(),
		}
		_ = d.Remove(ctx, f)
	}
}

func (d *AliyundriveOpen) checkUserId() error {
	userid, err := d.getUser()
	if err != nil {
		return err
	}

	res, err := d.Request("/adrive/v1.0/user/getDriveInfo", http.MethodPost, nil)
	if err != nil {
		log.Warnf("[%v] getDriveInfo error: %v", d.ID, err)
		return err
	}
	uid := utils.Json.Get(res, "user_id").ToString()
	name := utils.Json.Get(res, "name").ToString()
	log.Infof("[%v] 开放token 账号(%v) 昵称： %v", d.ID, uid, name)
	if uid != userid {
		return errors.New("阿里Token与开放Token账号不匹配！")
	}
	d.DriveId = utils.Json.Get(res, d.DriveType+"_drive_id").ToString()
	if d.DriveId == "" {
		d.DriveId = utils.Json.Get(res, "default_drive_id").ToString()
		log.Infof("[%v] use default drive: %v", d.ID, d.DriveId)
	} else {
		log.Infof("[%v] use %v drive: %v", d.ID, d.DriveType, d.DriveId)
	}
	return nil
}

func (d *AliyundriveOpen) getUser() (string, error) {
	res, err := d.request2("https://user.aliyundrive.com/v2/user/get", http.MethodPost, nil, true)
	if err != nil {
		return "", err
	}
	userid := utils.Json.Get(res, "user_id").ToString()
	nickname := utils.Json.Get(res, "nick_name").ToString()
	log.Printf("[%v] 阿里token 账号(%v) 昵称： %v", d.ID, userid, nickname)
	return userid, nil
}

func (d *AliyundriveOpen) getVipInfo() {
	res, err := d.request2("https://api.aliyundrive.com/business/v1.0/users/vip/info", http.MethodPost, nil, true)
	if err != nil {
		log.Warnf("[%v] get vip info failed: %v", d.ID, err)
		return
	}
	identity := utils.Json.Get(res, "identity").ToString()
	status := utils.Json.Get(res, "status").ToString()
	d.IsVip = strings.Contains(identity, "vip")
	log.Infof("[%v] 阿里账号会员类型: %v  状态: %v", d.ID, identity, status)
}

func (d *AliyundriveOpen) request2(url, method string, callback base.ReqCallback, retry bool) ([]byte, error) {
	var e ErrorResp
	req := base.RestyClient.R().
		SetError(&e).
		SetHeader("content-type", "application/json").
		SetHeader("Referer", "https://www.alipan.com/").
		SetHeader("User-Agent", conf.UserAgent).
		SetHeader("Authorization", "Bearer\t"+d.AccessToken2).
		SetHeader(CanaryHeaderKey, CanaryHeaderValue)
	if callback != nil {
		callback(req)
	} else {
		req.SetBody("{}")
	}
	resp, err := req.Execute(method, url)
	if err != nil {
		log.Warnf("[%v] 请求失败: %v", d.ID, err)
		return nil, err
	}
	if e.Code != "" {
		log.Warnf("[%v] 请求失败: %v '%v'", d.ID, e.Code, e.Message)
		if retry && e.Code == "AccessTokenInvalid" {
			err = d.RefreshAliToken(true)
			if err != nil {
				return nil, err
			}
			return d.request2(url, method, callback, false)
		} else {
			return nil, errors.New(e.Code + ": " + e.Message)
		}
	}
	return resp.Body(), nil
}
