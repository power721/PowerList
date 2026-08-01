package _123Share

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	_123 "github.com/OpenListTeam/OpenList/v4/drivers/123"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	log "github.com/sirupsen/logrus"
)

// errNoThumbLink 该文件的 /share/get 列表项没有可用的 DownloadUrl(目录,或接口没给),
// 调用方应回退到 share/download/info。
var errNoThumbLink = errors.New("123 分享列表未提供直链")

// resolveThumbDirect 无限直链入口,声明为 var 以便单测替换(规避真实网络/op 依赖)。
var resolveThumbDirect = func(d *Pan123Share, ctx context.Context, f File, ip string) (*model.Link, error) {
	return d.thumbDirectLink(ctx, f, ip)
}

// thumbDirectLink 用 /share/get 列表里的 DownloadUrl 直接组直链,不再调 share/download/info。
// DownloadUrl 为空或签名快过期时,重新匿名列一次父目录换新签名。
func (d *Pan123Share) thumbDirectLink(ctx context.Context, f File, ip string) (*model.Link, error) {
	if f.IsDir() {
		return nil, errNoThumbLink
	}
	raw := f.DownloadUrl
	if ttl, ok := _123.ThumbLinkTTL(raw); raw == "" || !ok || ttl < _123.ThumbLinkMinTTL {
		fresh, err := d.refreshDownloadUrl(ctx, f)
		if err != nil {
			if raw == "" {
				return nil, err
			}
			// 刷新失败但手上这条还没到期,继续用。
			log.Debugf("[123_share] 刷新直链失败,沿用列表缓存: %v", err)
		} else {
			raw = fresh
		}
	}
	if raw == "" {
		return nil, errNoThumbLink
	}

	direct, stripped := _123.StripThumbTransform(raw)
	ttl, ok := _123.ThumbLinkTTL(direct)
	if !ok || ttl <= 0 {
		return nil, errNoThumbLink
	}
	u, err := url.Parse(direct)
	if err != nil {
		return nil, err
	}
	log.Debugf("[123_share] 无限直链(stripped=%v, ttl=%v): %s", stripped, ttl.Truncate(time.Second), f.FileName)

	// 不跟 302:签名直链本身有效期长达数天,而 302 落地的节点 URL 带一次性 xmfcid,
	// 缓存价值更低;播放器/代理会自行跟随。
	exp := ttl
	return &model.Link{
		URL:        direct,
		Expiration: &exp,
		Header: http.Header{
			"Referer":    []string{u.Scheme + "://" + u.Host + "/"},
			"User-Agent": []string{AnonUA},
		},
	}, nil
}

// refreshDownloadUrl 重新匿名列一次父目录,取回该 FileId 的新签名 DownloadUrl。
func (d *Pan123Share) refreshDownloadUrl(ctx context.Context, f File) (string, error) {
	files, err := d.getFilesAnon(ctx, strconv.FormatInt(f.ParentFileId, 10))
	if err != nil {
		return "", err
	}
	for _, it := range files {
		if it.FileId == f.FileId && it.DownloadUrl != "" {
			return it.DownloadUrl, nil
		}
	}
	return "", errNoThumbLink
}
