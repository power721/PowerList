package _123

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/cache"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	jsoniter "github.com/json-iterator/go"
	log "github.com/sirupsen/logrus"
)

// WebFileList web 平台形态的列目录接口(/b/api),只有它返回缩略图形态的 DownloadUrl。
const WebFileList = BApi + "/file/list/new"

// WebUserAgent web 平台请求用的 UA,与 123 网页端一致。
const WebUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"

// 「无限直链」——直接用文件列表里 123 已经签好的直链,省掉 file/download_info。
//
// /b/api/file/list/new 的每个列表项都带一条服务端签名的 CDN 直链 DownloadUrl,
// 对视频给的是缩略图形态:
//
//	https://download-cdn.cjjd19.com/123-4/70c9353a/1830358103-0/<etag>/c-m9011_24_24
//	  ?v=5&t=<expire>&r=…&bzc=1&bzs=<hex>&ur=…&s=<expire><sign>&bi=…
//	  &filename=…&cache_type=1&w=24&h=24&trade_key=123pan-thumbnail&type=video
//
// 签名 s= 只覆盖对象路径(/123-N/<etag 前 8 位>/<s3KeyFlag>/<etag>/c-m<node>),既不覆盖
// 变换后缀 _24_24 也不覆盖 trade_key。去掉这两处后同一签名即原始文件的合法直链,
// 实测 206 且 Content-Range 是完整大小。
//
// 收益:
//   - 每次 Link() 少一次 POST file/download_info(还绕开了它 700ms 的限流器),
//     也少一次现有实现里对 302 的探测请求;
//   - 列表直链的 t= 是过期时间戳,实测签发后 +7 天,远长于 download_info 的 15 分钟;
//   - 计费参数 bzs(hex 编码的 ASCII)实测三种取值:
//     web 列表(缩略图形态) hex("<uid>:2:0:0")
//     android 列表         hex("<uid>:3:1:<size>")
//     file/download_info   hex("<uid>:2:1:<size>")
//     末两段在后两者是「1 + 文件大小」,在 web 列表是「0 + 0」。倾向于说明缩略图通道
//     不计下载额度,但账内额度无法从外部核实,故不作保证。
//     注意 android/tv 平台的列表给的是普通形态(与 download_info 同计费),
//     所以 thumbDirectLink 会额外拉一次 web 平台列表去换缩略图形态。
//
// 剥离规则来自对 my.jar(2026-08-01)assets/FishGuard-v8.so 里
// FishCrypto._pan123UnlimitedUrl(String) 的 unidbg 逐例实测:两次「首次出现」的
// 字面量替换,_70_70 等其它尺寸与其它 trade_key 取值都不动。
const (
	thumbTransformSuffix = "_24_24"
	thumbTradeKeyParam   = "&trade_key=123pan-thumbnail"

	// ThumbLinkMinTTL 列表直链剩余有效期低于此值就重新列一次目录换新签名。
	ThumbLinkMinTTL = 30 * time.Minute
)

// StripThumbTransform 去掉签名直链上的缩略图变换标记,返回新 URL 与是否发生改动。
// 刻意用字面量替换而不是解析 query 后重排:重排会打乱参数顺序,而 trade_key 的
// 匹配依赖前导 '&'(位于 '?' 之后的首个参数不该被删,与 native 行为一致)。
func StripThumbTransform(raw string) (string, bool) {
	out := raw
	if i := strings.Index(out, thumbTransformSuffix); i >= 0 {
		out = out[:i] + out[i+len(thumbTransformSuffix):]
	}
	if i := strings.Index(out, thumbTradeKeyParam); i >= 0 {
		out = out[:i] + out[i+len(thumbTradeKeyParam):]
	}
	return out, out != raw
}

// ThumbLinkTTL 读直链里的 t=(过期时间戳),返回剩余有效期;解析不出 t 时返回 0,false。
func ThumbLinkTTL(raw string) (time.Duration, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return 0, false
	}
	ts := u.Query().Get("t")
	if ts == "" {
		return 0, false
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return 0, false
	}
	return time.Until(time.Unix(sec, 0)), true
}

// ErrNoThumbLink 该文件的列表项没有可用的 DownloadUrl(目录,或接口没给),
// 调用方应回退到 file/download_info。
var ErrNoThumbLink = errors.New("123 文件列表未提供直链")

// resolveThumbDirect 无限直链入口,声明为 var 以便单测替换(规避真实网络依赖)。
var resolveThumbDirect = func(d *Pan123, ctx context.Context, f File, ip string) (*model.Link, error) {
	return d.thumbDirectLink(ctx, f, ip)
}

// isThumbForm 判断这条列表直链是不是缩略图形态(即那条不计额度的通道)。
func isThumbForm(raw string) bool {
	_, ok := StripThumbTransform(raw)
	return ok
}

// webThumbCache 缓存 web 平台列表返回的 parentFileId -> {fileId: 直链},
// 避免同一目录下每个文件各发一次列表请求。key 是 "<storageID>:<parentFileId>"。
var webThumbCache = cache.NewKeyedCache[map[int64]string](5 * time.Minute)

// thumbDirectLink 用文件列表里已签名的直链直接组 Link,不再调 file/download_info。
//
// 优先级:
//  1. 手上这条(List 缓存的)已经是缩略图形态且没快过期 —— 零请求;
//  2. 否则拉一次 web 平台列表拿缩略图形态 —— 一个请求,换来不计额度的通道;
//  3. 否则退而用手上这条普通形态的直链(android/tv 平台列表给的就是这种,
//     计费与 download_info 相同,但仍省掉一次 POST 和一次 302 探测);
//  4. 都不行则返回错误,由 Link() 回退 file/download_info。
func (d *Pan123) thumbDirectLink(ctx context.Context, f File, ip string) (*model.Link, error) {
	if f.IsDir() {
		return nil, ErrNoThumbLink
	}

	raw := f.DownloadUrl
	if fresh(raw) && isThumbForm(raw) {
		return d.buildThumbLink(raw, f)
	}

	if u, err := d.webThumbUrl(ctx, f); err == nil {
		return d.buildThumbLink(u, f)
	} else {
		log.Debugf("[123] web 列表未取到缩略图直链: %v", err)
	}

	if fresh(raw) {
		return d.buildThumbLink(raw, f)
	}
	return nil, ErrNoThumbLink
}

// fresh 直链的 t=(过期时间戳)是否还剩足够时间。
func fresh(raw string) bool {
	if raw == "" {
		return false
	}
	ttl, ok := ThumbLinkTTL(raw)
	return ok && ttl >= ThumbLinkMinTTL
}

func (d *Pan123) buildThumbLink(raw string, f File) (*model.Link, error) {
	direct, stripped := StripThumbTransform(raw)
	ttl, ok := ThumbLinkTTL(direct)
	if !ok || ttl <= 0 {
		return nil, ErrNoThumbLink
	}
	u, err := url.Parse(direct)
	if err != nil {
		return nil, err
	}
	log.Debugf("[123] 列表直链(stripped=%v, ttl=%v): %s", stripped, ttl.Truncate(time.Second), f.FileName)
	// 不跟 302:签名直链本身有效期数天,而 302 落地的节点 URL 带一次性 xmfcid,
	// 缓存价值更低;播放器/代理会自行跟随。
	exp := ttl
	return &model.Link{
		URL:        direct,
		Expiration: &exp,
		Header: http.Header{
			"Referer": []string{u.Scheme + "://" + u.Host + "/"},
		},
	}, nil
}

// fetchWebThumbUrls 拉 web 平台列表,声明为 var 以便单测替换(规避真实网络依赖)。
var fetchWebThumbUrls = func(d *Pan123, ctx context.Context, parentId string) (map[int64]string, error) {
	return d.webListDownloadUrls(ctx, parentId)
}

// webThumbUrl 取该文件在 web 平台列表里的缩略图形态直链(带缓存)。
func (d *Pan123) webThumbUrl(ctx context.Context, f File) (string, error) {
	parentId := strconv.FormatInt(f.ParentFileId, 10)
	key := strconv.Itoa(int(d.ID)) + ":" + parentId
	if m, ok := webThumbCache.Get(key); ok {
		if u, ok := m[f.FileId]; ok && fresh(u) && isThumbForm(u) {
			return u, nil
		}
	}
	m, err := fetchWebThumbUrls(d, ctx, parentId)
	if err != nil {
		return "", err
	}
	webThumbCache.Set(key, m)
	if u, ok := m[f.FileId]; ok && isThumbForm(u) {
		return u, nil
	}
	return "", ErrNoThumbLink
}

// webListDownloadUrls 以 web 平台形态(signPath 签名 + web 头,不带 android 的 auth-key)
// 列一次目录。只有 web 平台的列表才返回缩略图形态的 DownloadUrl:
//
//	web     -> .../c-m9011_24_24?...&bzs=hex("<uid>:2:0:0")&...&trade_key=123pan-thumbnail
//	android -> .../c-m9011?...&bzs=hex("<uid>:3:1:<size>")            (与 download_info 同计费)
func (d *Pan123) webListDownloadUrls(ctx context.Context, parentId string) (map[int64]string, error) {
	if err := d.APIRateLimit(ctx, WebFileList); err != nil {
		return nil, err
	}
	out := make(map[int64]string)
	page := 1
	for {
		var resp Files
		res, err := base.RestyClient.R().
			SetContext(ctx).
			SetHeaders(map[string]string{
				"authorization": "Bearer " + d.AccessToken,
				"user-agent":    WebUserAgent,
				"accept":        "application/json, text/plain, */*",
				"referer":       "https://yun.123pan.com/",
				"origin":        "https://yun.123pan.com",
				"platform":      "web",
				"app-version":   "3",
			}).
			SetQueryParams(map[string]string{
				"driveId":              "0",
				"limit":                "100",
				"next":                 "0",
				"orderBy":              "file_id",
				"orderDirection":       "desc",
				"parentFileId":         parentId,
				"trashed":              "false",
				"SearchData":           "",
				"Page":                 strconv.Itoa(page),
				"OnlyLookAbnormalFile": "0",
				"event":                "homeListFile",
				"operateType":          "4",
				"inDirectSpace":        "false",
			}).
			SetResult(&resp).
			Get(GetApi(WebFileList))
		if err != nil {
			return nil, err
		}
		if code := utils.Json.Get(res.Body(), "code").ToInt(); code != 0 {
			return nil, fmt.Errorf("123 web 列表失败: %s", jsoniter.Get(res.Body(), "message").ToString())
		}
		for _, it := range resp.Data.InfoList {
			if it.DownloadUrl != "" {
				out[it.FileId] = it.DownloadUrl
			}
		}
		page++
		if len(resp.Data.InfoList) == 0 || resp.Data.Next == "-1" {
			break
		}
	}
	return out, nil
}
