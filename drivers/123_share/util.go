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
	_123rapid "github.com/OpenListTeam/OpenList/v4/drivers/123_rapid"
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

	// 匿名(免登录)通道:yun.123pan.com 的分享接口对公开分享可直接换直链,无需 Bearer/auth-key。
	// www.123pan.cn 已不再对该 API 提供 HTTPS(握手返回明文 HTTP),故改用 yun.123pan.com。
	AnonOrigin       = "https://yun.123pan.com"
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

// requestAnon 匿名请求:无 Authorization,但带 signPath 签名(GetApi)+ 官方 web 客户端头。
// 实测 /b/api 的 share/get、share/download/info 均要求 web 签名(否则返回非 0 code),
// 故匿名路径同样走 GetApi(与鉴权路径 signPath(path,"web","3") 一致),仅少了 Bearer。
// 返回原始响应体,由调用方检查 code。
func (d *Pan123Share) requestAnon(targetUrl, method string, callback base.ReqCallback, resp interface{}) ([]byte, error) {
	req := base.RestyClient.R()
	req.SetHeaders(map[string]string{
		"user-agent":   AnonUA,
		"referer":      AnonOrigin + "/",
		"origin":       AnonOrigin,
		"accept":       "application/json,text/plain,*/*",
		"platform":     "web",
		"app-version":  "3",
		"content-type": "application/json;charset=UTF-8",
	})
	if callback != nil {
		callback(req)
	}
	if resp != nil {
		req.SetResult(resp)
	}
	res, err := req.Execute(method, GetApi(targetUrl))
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

// rapidShareTo123 在分享方流量耗尽(err123TrafficLimit, 5112)时,按 MD5(Etag)把 123 分享文件
// 秒传到 123 Open 账号并取个人下载直链(不走分享流量)。声明为 var 便于单测替换。
// 参考 drivers/123_rapid/rapid.go(RapidTo123)与 drivers/quark_uc_share/util.go:rapidQuarkUCTo123;
// 秒传文件落 alist-tvbox-temp 并延时清理(见 drivers/123_open/extension.go)。
var rapidShareTo123 = func(f File) *model.Link {
	if len(f.Etag) != utils.MD5.Width {
		return nil
	}
	link, err := _123rapid.RapidTo123(context.Background(), _123rapid.Source{
		HashType: utils.MD5,
		Hash:     f.Etag,
		Name:     f.FileName,
		Size:     f.Size,
	})
	if err != nil || link == nil {
		log.Debugf("[123_share] rapid to 123 skipped: %v", err)
		return nil
	}
	log.Infof("[123_share] 使用123秒传直链(5112 兜底): %s", f.FileName)
	return link
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

// listQuery 鉴权路径的 /share/get 查询参数(历史可用形态:小写 parentFileId)。
func (d *Pan123Share) listQuery(parentId string, page int) map[string]string {
	return map[string]string{
		"limit":          "100",
		"next":           "0",
		"orderBy":        "file_id",
		"orderDirection": "desc",
		"parentFileId":   parentId,
		"Page":           strconv.Itoa(page),
		"shareKey":       d.ShareKey,
		"SharePwd":       d.SharePwd,
	}
}

// listQueryAnon 匿名路径的 /share/get 查询参数:对齐官方 web 客户端实测请求——
// ParentFileId 驼峰、next=-1,并带 event/operateType/OrderId/superAdmin。
func (d *Pan123Share) listQueryAnon(parentId string, page int) map[string]string {
	return map[string]string{
		"limit":          "100",
		"next":           "-1",
		"orderBy":        "file_name",
		"orderDirection": "asc",
		"ParentFileId":   parentId,
		"Page":           strconv.Itoa(page),
		"shareKey":       d.ShareKey,
		"SharePwd":       d.SharePwd,
		"event":          "homeListFile",
		"operateType":    "4",
		"OrderId":        "",
		"superAdmin":     "null",
	}
}

// resolveAnonList 匿名列目录入口,声明为 var 以便单测替换(规避真实网络/op 依赖)。
// 公开分享的 /share/get 可免登录返回,与匿名下载(anonDownloadLink)对齐。
var resolveAnonList = func(d *Pan123Share, ctx context.Context, parentId string) ([]File, error) {
	return d.getFilesAnon(ctx, parentId)
}

// getFiles 列目录:匿名优先(零账号可用),失败回退账号/ref/AccessToken。
func (d *Pan123Share) getFiles(ctx context.Context, parentId string) ([]File, error) {
	if files, err := resolveAnonList(d, ctx, parentId); err == nil {
		return files, nil
	} else {
		log.Debugf("[123_share] 匿名列目录失败,回退账号: %v", err)
	}
	return d.getFilesAuth(ctx, parentId)
}

// getFilesAuth 鉴权列目录:走 d.request(账号/ref/AccessToken + signPath 签名)。
func (d *Pan123Share) getFilesAuth(ctx context.Context, parentId string) ([]File, error) {
	page := 1
	res := make([]File, 0)
	for {
		if err := d.APIRateLimit(ctx, FileList); err != nil {
			return nil, err
		}
		var resp Files
		_, err := d.request(FileList, http.MethodGet, func(req *resty.Request) {
			req.SetQueryParams(d.listQuery(parentId, page))
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

// getFilesAnon 匿名列目录:无 Authorization,但带 signPath 签名 + 官方 web 头(见 requestAnon)。
// 用于公开分享免登录浏览。code!=0 视为失败,由调用方回退鉴权路径。
func (d *Pan123Share) getFilesAnon(ctx context.Context, parentId string) ([]File, error) {
	page := 1
	res := make([]File, 0)
	for {
		if err := d.APIRateLimit(ctx, FileList); err != nil {
			return nil, err
		}
		var resp Files
		body, err := d.requestAnon(FileList, http.MethodGet, func(req *resty.Request) {
			req.SetQueryParams(d.listQueryAnon(parentId, page))
		}, &resp)
		if err != nil {
			return nil, err
		}
		if code := utils.Json.Get(body, "code").ToInt(); code != 0 {
			return nil, fmt.Errorf("123 匿名列目录失败: %s", jsoniter.Get(body, "message").ToString())
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
	// 匿名优先:公开分享可免登录校验(对齐官方 web 客户端请求形态)。
	if body, err := d.requestAnon(FileList, http.MethodGet, func(req *resty.Request) {
		q := d.listQueryAnon("0", 1)
		q["limit"] = "1"
		req.SetQueryParams(q)
	}, nil); err == nil && utils.Json.Get(body, "code").ToInt() == 0 {
		return nil
	}
	// 回退鉴权(账号/ref/AccessToken + 签名)。
	_, err := d.request(FileList, http.MethodGet, func(req *resty.Request) {
		q := d.listQuery("0", 1)
		q["limit"] = "1"
		req.SetQueryParams(q)
	}, nil)
	return err
}
