// Package _123rapid 实现跨盘秒传到 123(115/夸克·UC/光鸭 → 123)。
//
// 移植自 Node.js 参考实现 rapid-transfer.js:纯 hash 秒传(不下载/上传源文件),
// 命中后 createShare 建公开分享,再免登录解析 123 分享直链返回。
// 失败/未命中返回 nil 链,由调用方(各源 share 驱动的 Link())回退到源原生链。
//
// 秒传原语复用 drivers/123_open:
//   - SHA1(115) → Open123.sha1_reuse
//   - MD5(夸克/UC/光鸭) → Open123.file/create (etag=md5)
//
// 免登录解析(匿名 share/get + share/download/info)移植自 drivers/123_share。
package _123rapid

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	_123_open "github.com/OpenListTeam/OpenList/v4/drivers/123_open"
	"github.com/OpenListTeam/OpenList/v4/internal/cache"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	log "github.com/sirupsen/logrus"
)

const (
	open123DriverName = "123 Open"
	// mappingTTL:秒传结果(fileId/shareKey)缓存时长。v1 不清理 123 临时文件,
	// 分享长期有效,故给较长 TTL 避免重复秒传;过期后重放一次秒传即可。
	mappingTTL  = 6 * time.Hour
	shareExpire = 0 // 永久分享
)

// Source 描述待秒传的源文件。HashType=SHA1(115) 或 MD5(夸克/UC/光鸭)。
type Source struct {
	HashType *utils.HashType
	Hash     string // 40-hex(SHA1) 或 32-hex(MD5),大小写不敏感
	Name     string
	Size     int64
}

func (s Source) valid() bool {
	if s.Size <= 0 || s.Name == "" || s.HashType == nil {
		return false
	}
	h := strings.ToLower(strings.TrimSpace(s.Hash))
	switch s.HashType {
	case utils.SHA1:
		return len(h) == utils.SHA1.Width
	case utils.MD5:
		return len(h) == utils.MD5.Width
	}
	return false
}

func (s Source) cacheKey() string {
	width := 0
	if s.HashType != nil {
		width = s.HashType.Width
	}
	return fmt.Sprintf("%d:%s:%d", width, strings.ToLower(s.Hash), s.Size)
}

// rapidEntry 缓存秒传结果:同一 hash+size 命中后不必再秒传/建分享,直接免登录解析。
type rapidEntry struct {
	fileID   int64
	shareKey string
	pwd      string
}

var (
	rapidCache = cache.NewKeyedCache[*rapidEntry](mappingTTL)
	// rapidIdx 轮询 123 Open 账号(同一 hash 各账号结果一致,主要分散落盘)。
	rapidIdx int
)

// RapidTo123 秒传 src 到某个 123 Open 账号并返回免登录可播放直链。
// 成功返回 (link, nil);未命中或任何失败返回 (nil, err)(调用方据此回退源链)。
// 找不到 123 Open 账号时返回 (nil, errNoOpen123)。
func RapidTo123(ctx context.Context, src Source) (*model.Link, error) {
	if !src.valid() {
		return nil, errors.New("rapid: invalid source (need name/size/hash)")
	}

	// 命中缓存:优先免登录分享直链,失败(如 5112)回退 Open 账号个人下载。
	if e, ok := rapidCache.Get(src.cacheKey()); ok && e != nil {
		if link := resolve123Link(firstOpen123(), e); link != nil {
			return link, nil
		}
	}

	count := op.GetDriverCount(open123DriverName)
	if count == 0 {
		return nil, errNoOpen123
	}
	var lastErr error
	for i := 0; i < count; i++ {
		storage := op.GetFirstDriver(open123DriverName, rapidIdx)
		rapidIdx++
		if storage == nil {
			continue
		}
		open, ok := storage.(*_123_open.Open123)
		if !ok || open == nil {
			continue
		}

		reuse, fileID, err := open.Reuse(src.HashType, src.Hash, src.Name, src.Size, 1)
		if err != nil {
			// 鉴权/限流类错误换下一个账号重试;业务错误直接返回。
			lastErr = err
			log.Warnf("[rapid-to-123] Reuse failed (%s/%s): %v", open123DriverName, src.Name, err)
			continue
		}
		if !reuse {
			// 未命中:同一 hash 各账号结果一致,无需再试。
			return nil, errRapidMiss
		}
		if fileID <= 0 {
			lastErr = errors.New("rapid: reuse hit but empty fileID")
			continue
		}

		// 建分享用于免登录解析;失败不致命(免费账号分享流量为 0 会 5112,改走个人下载)。
		entry := &rapidEntry{fileID: fileID}
		if share, err := open.CreateShare(fileID, src.Name, shareExpire); err == nil && share != nil && share.Data.ShareKey != "" {
			entry.shareKey = share.Data.ShareKey
			entry.pwd = share.Data.SharePwd
		} else {
			log.Warnf("[rapid-to-123] CreateShare failed, will use personal download (%s): %v", src.Name, err)
		}
		rapidCache.Set(src.cacheKey(), entry)

		if link := resolve123Link(open, entry); link != nil {
			log.Infof("[rapid-to-123] %s reuse hit (via share=%v)", src.Name, entry.shareKey != "")
			return link, nil
		}
		lastErr = errors.New("rapid: resolve link failed (anon share + personal download)")
		log.Warnf("[rapid-to-123] resolve failed (%s/%s)", open123DriverName, src.Name)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errNoOpen123
}

// firstOpen123 取首个 123 Open 账号(缓存命中回退个人下载时用)。
func firstOpen123() *_123_open.Open123 {
	s := op.GetFirstDriver(open123DriverName, 0)
	if s == nil {
		return nil
	}
	open, _ := s.(*_123_open.Open123)
	return open
}

// resolve123Link 对齐 JS tryResolveA123PlayableUrl 的兜底链:
// 1) 免登录分享直链(share/download/info);2) 失败(含 5112 分享流量不足)回退 Open 账号个人 download_info(不走分享流量)。
func resolve123Link(open *_123_open.Open123, entry *rapidEntry) *model.Link {
	if entry.shareKey != "" {
		if link, err := resolveAnonShareLink(entry.shareKey, entry.pwd, entry.fileID); err == nil && link != nil {
			return link
		}
	}
	if open != nil {
		if dl, err := open.DownloadURL(entry.fileID); err == nil && dl != "" {
			exp := 30 * time.Minute
			return &model.Link{URL: dl, Expiration: &exp}
		}
	}
	return nil
}

var (
	errNoOpen123 = errors.New("rapid: 找不到 123 Open 账号")
	// errRapidMiss 123 未命中该 hash(非错误,调用方静默回退)。
	errRapidMiss = errors.New("rapid: 123 未命中秒传")
)
