package _123Share

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	_123 "github.com/OpenListTeam/OpenList/v4/drivers/123"
	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/go-resty/resty/v2"
	jsoniter "github.com/json-iterator/go"
	log "github.com/sirupsen/logrus"
)

const (
	Api          = "https://yun.123pan.com/api"
	AApi         = "https://yun.123pan.com/a/api"
	BApi         = "https://yun.123pan.com/b/api"
	MainApi      = BApi
	FileList     = MainApi + "/share/get"
	DownloadInfo = MainApi + "/share/download/info"
	//AuthKeySalt      = "8-8D$sL8gPjom7bk#cY"

	// 匿名(免登录)通道:www.123pan.cn 的分享接口对公开分享可直接换直链,无需 Bearer/auth-key。
	AnonOrigin       = "https://www.123pan.cn"
	AnonDownloadInfo = AnonOrigin + "/b/api/share/download/info"
	AnonUA           = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"
)

// err123TrafficLimit 分享方提取流量耗尽(API code 5112)。账号重试或旧驱动都无法解决,
// 调用方应直接返回该错误而非静默回退。
var err123TrafficLimit = errors.New("123 分享流量包不足(5112)")

var idx = 0

func signPath(path string, os string, version string) (k string, v string) {
	table := []byte{'a', 'd', 'e', 'f', 'g', 'h', 'l', 'm', 'y', 'i', 'j', 'n', 'o', 'p', 'k', 'q', 'r', 's', 't', 'u', 'b', 'c', 'v', 'w', 's', 'z'}
	random := fmt.Sprintf("%.f", math.Round(1e7*rand.Float64()))
	now := time.Now().In(time.FixedZone("CST", 8*3600))
	timestamp := fmt.Sprint(now.Unix())
	nowStr := []byte(now.Format("200601021504"))
	for i := 0; i < len(nowStr); i++ {
		nowStr[i] = table[nowStr[i]-48]
	}
	timeSign := fmt.Sprint(crc32.ChecksumIEEE(nowStr))
	data := strings.Join([]string{timestamp, random, path, os, version, timeSign}, "|")
	dataSign := fmt.Sprint(crc32.ChecksumIEEE([]byte(data)))
	return timeSign, strings.Join([]string{timestamp, random, dataSign}, "-")
}

func GetApi(rawUrl string) string {
	u, _ := url.Parse(rawUrl)
	query := u.Query()
	query.Add(signPath(u.Path, "web", "3"))
	u.RawQuery = query.Encode()
	return u.String()
}

func (d *Pan123Share) request(url string, method string, callback base.ReqCallback, resp interface{}) ([]byte, error) {
	storage := op.GetFirstDriver("123Pan", idx)
	if storage != nil {
		pan123, ok := storage.(*_123.Pan123)
		if ok {
			return pan123.Request(url, method, callback, resp)
		}
	}
	if d.ref != nil {
		return d.ref.Request(url, method, callback, resp)
	}
	req := base.RestyClient.R()
	req.SetHeaders(map[string]string{
		"origin":        "https://yun.123pan.com",
		"referer":       "https://yun.123pan.com/",
		"authorization": "Bearer " + d.AccessToken,
		"user-agent":    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) openlist-client",
		"platform":      "web",
		"app-version":   "3",
		//"user-agent":    base.UserAgent,
	})
	if callback != nil {
		callback(req)
	}
	if resp != nil {
		req.SetResult(resp)
	}
	res, err := req.Execute(method, GetApi(url))
	if err != nil {
		return nil, err
	}
	body := res.Body()
	code := utils.Json.Get(body, "code").ToInt()
	if code != 0 {
		return nil, errors.New(jsoniter.Get(body, "message").ToString())
	}
	return body, nil
}

// requestAnon 匿名请求:无 Authorization、无 auth-key 签名,仅带浏览器 UA + Referer。
// 用于公开分享免登录换直链。返回原始响应体,由调用方检查 code。
func (d *Pan123Share) requestAnon(targetUrl, method string, callback base.ReqCallback, resp interface{}) ([]byte, error) {
	req := base.RestyClient.R()
	req.SetHeaders(map[string]string{
		"user-agent":   AnonUA,
		"referer":      AnonOrigin + "/",
		"origin":       AnonOrigin,
		"accept":       "application/json,text/plain,*/*",
		"platform":     "android",
		"app-version":  "43",
		"content-type": "application/json;charset=UTF-8",
	})
	if callback != nil {
		callback(req)
	}
	if resp != nil {
		req.SetResult(resp)
	}
	res, err := req.Execute(method, targetUrl)
	if err != nil {
		return nil, err
	}
	return res.Body(), nil
}

// unwrap123DownloadLink 把 download/info 返回的 DownloadURL 解包成可播放直链:
// 处理 base64 params 重定向、302 location / data.redirect_url,并设 Referer;15 分钟有效。
func unwrap123DownloadLink(downloadUrl string) (*model.Link, error) {
	if downloadUrl == "" {
		return nil, errors.New("empty 123 download url")
	}
	ou, err := url.Parse(downloadUrl)
	if err != nil {
		return nil, err
	}
	u_ := ou.String()
	if nu := ou.Query().Get("params"); nu != "" {
		du, e := base64.StdEncoding.DecodeString(nu)
		if e != nil {
			return nil, e
		}
		u, e := url.Parse(string(du))
		if e != nil {
			return nil, e
		}
		u_ = u.String()
	}
	log.Debug("123 download url: ", u_)
	res, err := base.NoRedirectClient.R().SetHeader("Referer", AnonOrigin+"/").Get(u_)
	if err != nil {
		return nil, err
	}
	log.Debug(res.String())
	exp := 15 * time.Minute
	link := &model.Link{Expiration: &exp, URL: u_}
	log.Debugln("123 res code: ", res.StatusCode())
	if res.StatusCode() == 302 {
		link.URL = res.Header().Get("location")
	} else if res.StatusCode() < 300 {
		link.URL = utils.Json.Get(res.Body(), "data", "redirect_url").ToString()
	}
	link.Header = http.Header{
		"Referer": []string{fmt.Sprintf("%s://%s/", ou.Scheme, ou.Host)},
	}
	return link, nil
}

// anonDownloadLink 匿名换取 123 分享直链(无需 123Pan 账号)。
// 返回 err123TrafficLimit 表示分享方流量耗尽,调用方不应再回退账号重试。
func (d *Pan123Share) anonDownloadLink(f File, ip string) (*model.Link, error) {
	headers := map[string]string{}
	if !utils.IsLocalIPAddr(ip) {
		headers["X-Forwarded-For"] = ip
	}
	body := base.Json{
		"driveId":   "0",
		"shareKey":  d.ShareKey,
		"SharePwd":  d.SharePwd,
		"etag":      f.Etag,
		"fileId":    f.FileId,
		"s3keyFlag": f.S3KeyFlag,
		"FileName":  f.FileName,
		"size":      f.Size,
	}
	respBody, err := d.requestAnon(AnonDownloadInfo, http.MethodPost, func(req *resty.Request) {
		req.SetBody(body).SetHeaders(headers)
	}, nil)
	if err != nil {
		return nil, err
	}
	code := utils.Json.Get(respBody, "code").ToInt()
	message := jsoniter.Get(respBody, "message").ToString()
	if code == 5112 || strings.Contains(message, "流量包不足") || strings.Contains(message, "提取流量不足") {
		return nil, err123TrafficLimit
	}
	if code != 0 {
		return nil, errors.New(message)
	}
	return unwrap123DownloadLink(utils.Json.Get(respBody, "data", "DownloadURL").ToString())
}

func (d *Pan123Share) getFiles(ctx context.Context, parentId string) ([]File, error) {
	page := 1
	res := make([]File, 0)
	for {
		if err := d.APIRateLimit(ctx, FileList); err != nil {
			return nil, err
		}
		var resp Files
		query := map[string]string{
			"limit":          "100",
			"next":           "0",
			"orderBy":        "file_id",
			"orderDirection": "desc",
			"parentFileId":   parentId,
			"Page":           strconv.Itoa(page),
			"shareKey":       d.ShareKey,
			"SharePwd":       d.SharePwd,
		}
		_, err := d.request(FileList, http.MethodGet, func(req *resty.Request) {
			req.SetQueryParams(query)
		}, &resp)
		if err != nil {
			return nil, err
		}
		page++
		res = append(res, resp.Data.InfoList...)
		if len(resp.Data.InfoList) == 0 || resp.Data.Next == "-1" {
			break
		}
	}
	return res, nil
}

// do others that not defined in Driver interface

func (d *Pan123Share) Validate() error {
	query := map[string]string{
		"limit":          "1",
		"next":           "0",
		"orderBy":        "file_id",
		"orderDirection": "desc",
		"parentFileId":   "0",
		"Page":           "1",
		"shareKey":       d.ShareKey,
		"SharePwd":       d.SharePwd,
	}
	_, err := d.request(FileList, http.MethodGet, func(req *resty.Request) {
		req.SetQueryParams(query)
	}, nil)
	return err
}
