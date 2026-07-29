package _123rapid

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/go-resty/resty/v2"
	jsoniter "github.com/json-iterator/go"
)

// 匿名(免登录)通道:www.123pan.cn 的分享接口对公开分享可直接列目录/换直链,
// 无需 Bearer/auth-key。常量与 drivers/123_share 对齐。
const (
	AnonOrigin       = "https://www.123pan.cn"
	AnonShareList    = AnonOrigin + "/b/api/share/get"
	AnonDownloadInfo = AnonOrigin + "/b/api/share/download/info"
	AnonUA           = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"
)

// shareFileMeta 匿名列分享取得的文件元信息,用于免登录换直链。
type shareFileMeta struct {
	FileID    int64
	FileName  string
	Size      int64
	Etag      string
	S3KeyFlag string
}

type shareListResp struct {
	Code int `json:"code"`
	Data struct {
		InfoList []struct {
			FileId    int64  `json:"FileId"`
			FileName  string `json:"FileName"`
			Size      int64  `json:"Size"`
			Etag      string `json:"Etag"`
			S3KeyFlag string `json:"S3KeyFlag"`
			Type      int    `json:"Type"`
		} `json:"InfoList"`
		Next string `json:"Next"`
	} `json:"data"`
}

// requestAnon 匿名请求:无 Authorization、无 auth-key 签名,仅浏览器 UA + Referer。
func requestAnon(targetUrl, method string, callback base.ReqCallback) ([]byte, error) {
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
	res, err := req.Execute(method, targetUrl)
	if err != nil {
		return nil, err
	}
	return res.Body(), nil
}

// listShareFile 匿名列分享,定位 fileID 对应文件,取 Etag/S3KeyFlag 等元信息。
func listShareFile(shareKey, pwd string, fileID int64) (*shareFileMeta, error) {
	page := 1
	next := "0"
	for safety := 0; safety < 50; safety++ {
		q := map[string]string{
			"limit":          "100",
			"next":           next,
			"orderBy":        "file_id",
			"orderDirection": "desc",
			"parentFileId":   "0",
			"Page":           strconv.Itoa(page),
			"shareKey":       shareKey,
			"SharePwd":       pwd,
		}
		body, err := requestAnon(AnonShareList, http.MethodGet, func(req *resty.Request) {
			req.SetQueryParams(q)
		})
		if err != nil {
			return nil, err
		}
		var resp shareListResp
		_ = jsoniter.Unmarshal(body, &resp)
		for _, it := range resp.Data.InfoList {
			if it.FileId == fileID {
				return &shareFileMeta{
					FileID:    it.FileId,
					FileName:  it.FileName,
					Size:      it.Size,
					Etag:      it.Etag,
					S3KeyFlag: it.S3KeyFlag,
				}, nil
			}
		}
		if len(resp.Data.InfoList) == 0 || resp.Data.Next == "" || resp.Data.Next == "-1" {
			break
		}
		next = resp.Data.Next
		page++
	}
	return nil, fmt.Errorf("rapid: share %s 未找到文件 %d", shareKey, fileID)
}

// anonDownloadURL 匿名换直链。code 5112 表示分享方流量耗尽。
func anonDownloadURL(shareKey, pwd string, m *shareFileMeta) (string, error) {
	body := map[string]any{
		"driveId":   "0",
		"shareKey":  shareKey,
		"SharePwd":  pwd,
		"etag":      m.Etag,
		"fileId":    m.FileID,
		"s3keyFlag": m.S3KeyFlag,
		"FileName":  m.FileName,
		"size":      m.Size,
	}
	respBody, err := requestAnon(AnonDownloadInfo, http.MethodPost, func(req *resty.Request) {
		req.SetBody(body)
	})
	if err != nil {
		return "", err
	}
	code := utils.Json.Get(respBody, "code").ToInt()
	msg := jsoniter.Get(respBody, "message").ToString()
	if code == 5112 {
		return "", errors.New("rapid: 123 分享流量包不足 (5112)")
	}
	if code != 0 {
		return "", errors.New(msg)
	}
	return utils.Json.Get(respBody, "data", "DownloadURL").ToString(), nil
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
	res, err := base.NoRedirectClient.R().SetHeader("Referer", AnonOrigin+"/").Get(u_)
	if err != nil {
		return nil, err
	}
	exp := 15 * time.Minute
	link := &model.Link{Expiration: &exp, URL: u_}
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

// resolveAnonShareLink 免登录解析 123 分享直链:列分享取元信息 → 换直链 → 解包。
func resolveAnonShareLink(shareKey, pwd string, fileID int64) (*model.Link, error) {
	if shareKey == "" || fileID <= 0 {
		return nil, errors.New("rapid: empty shareKey/fileID")
	}
	meta, err := listShareFile(shareKey, pwd, fileID)
	if err != nil {
		return nil, err
	}
	dl, err := anonDownloadURL(shareKey, pwd, meta)
	if err != nil {
		return nil, err
	}
	return unwrap123DownloadLink(dl)
}
