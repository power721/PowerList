package quark_uc_share

import (
	"context"
	"errors"
	"fmt"
	"time"

	quark "github.com/OpenListTeam/OpenList/v4/drivers/quark_uc"
	"github.com/OpenListTeam/OpenList/v4/drivers/quark_uc_tv"
	"github.com/OpenListTeam/OpenList/v4/internal/cache"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	"github.com/OpenListTeam/OpenList/v4/internal/token"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	log "github.com/sirupsen/logrus"
)

type QuarkUCShare struct {
	model.Storage
	Addition
	config driver.Config
	conf   Conf
}

// 免转存直链是夸克/UC 的签名 CDN URL,夜间夸克收紧 checkplay 时易过期。
// 缓存 2 分钟(对齐不夜 cloud-drive.js 的 120s 播放缓存):每次 miss 重新换一条新签名直链,
// 既省掉重复换链开销,又避免服务到已失效的旧直链。
var quarkUCShareLinkCache = cache.NewKeyedCache[*model.Link](2 * time.Minute)

var resolveQuarkUCShareLink = func(ctx context.Context, d *QuarkUCShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	name := d.getDriverName()
	count := op.GetDriverCount(name)
	log.Infof("[route] 转存+speedup 路径: driver=%s 网盘账号数=%d file=%s", name, count, file.GetName())
	if count == 0 {
		return nil, fmt.Errorf("没有 %s 网盘账号可转存", name)
	}
	var lastErr error
	for i := 0; i < count; i++ {
		link, err := d.link(ctx, file, args)
		if err == nil && link != nil {
			return link, nil
		}
		log.Warnf("[route] 第%d/%d 次转存取链失败: err=%v linkNil=%v", i+1, count, err, link == nil)
		lastErr = err
	}
	return nil, lastErr
}

func (d *QuarkUCShare) Config() driver.Config {
	return d.config
}

func (d *QuarkUCShare) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *QuarkUCShare) Init(ctx context.Context) error {
	key := conf.QUARK
	if d.config.Name == "UCShare" {
		key = conf.UC
	}
	if getShareCookie() == "" {
		setShareCookie(token.GetAccountToken(key))
	}

	if conf.LazyLoad && !conf.StoragesLoaded {
		return nil
	}

	return d.Validate()
}

func (d *QuarkUCShare) Drop(ctx context.Context) error {
	return nil
}

func (d *QuarkUCShare) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	if d.ShareToken == "" {
		if err := d.Validate(); err != nil {
			return nil, err
		}
	}

	files, err := d.getShareFiles(dir.GetID())
	if err != nil {
		log.Warnf("list files error: %v", err)
		return nil, err
	}
	return utils.SliceConvert(files, func(src File) (model.Obj, error) {
		return fileToObj(src), nil
	})
}

func (d *QuarkUCShare) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	if d.ShareToken == "" {
		if err := d.Validate(); err != nil {
			return nil, err
		}
	}

	key := file.GetID()
	if link, ok := quarkUCShareLinkCache.Get(key); ok {
		return link, nil
	}

	// 多账号分片并行下载(开关开):直接对所有网盘账号 goroutine 并发取链,
	// 跳过串行的 resolveQuarkUCShareLink(它失败才换下一个账号,慢;且会让主链账号被重复取)。
	// 首个账号成功后只额外等 2s 收集更多源,不等最慢账号,启播≈首源+2s。
	if multiSourceEnabled(d) {
		links := collectMultiAccountLinks(ctx, d, file, args)
		if len(links) > 0 {
			link := links[0]
			if len(links) > 1 {
				link.MultiSource = sourcesFromLinks(links, d)
				log.Infof("[multi-source] %s %s 用 %d 账号分片下载 file=%s", d.config.Name, file.GetName(), len(link.MultiSource), file.GetID())
			}
			quarkUCShareLinkCache.Set(key, link)
			return link, nil
		}
		// 全部账号取链失败,回退免转存兜底(免转存开关关时跳过)。
		if !shareDirectEnabled(d) {
			return nil, errors.New("[multi-source] 全部账号取链失败且免转存已关闭")
		}
		log.Warnf("[multi-source] 全部账号取链失败,回退免转存")
		link, err := resolveShareDirectLink(d, file)
		if err == nil && link != nil {
			quarkUCShareLinkCache.Set(key, link)
		}
		return link, err
	}

	// 开关关:转存 + speedup 为主(串行轮询账号,失败换下一个),免转存兜底(免转存开关开时)。
	var link *model.Link
	var err error
	link, err = resolveQuarkUCShareLink(ctx, d, file, args)
	if (err != nil || link == nil) && shareDirectEnabled(d) {
		log.Warnf("转存取链失败,回退免转存: %v", err)
		link, err = resolveShareDirectLink(d, file)
	}
	if err == nil && link != nil {
		quarkUCShareLinkCache.Set(key, link)
	}
	return link, err
}

func (d *QuarkUCShare) link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	// TV 账号优先(开启开关时),失败再走网盘账号。
	if setting.GetBool(conf.UssQuarkTv) {
		if link, err := d.getTvLink(ctx, file, args, false); link != nil {
			return link, err
		}
	}

	name := d.getDriverName()
	idx := int(accountIdx.Add(1))
	storage := op.GetFirstDriver(name, idx)
	if storage == nil {
		return nil, fmt.Errorf("找不到%s网盘帐号", name)
	}
	uc := storage.(*quark.QuarkOrUC)
	// 非 SVIP 账号原画直链易被限速,回落 TV 转码流(强制 streaming)。
	if !uc.VIP {
		if link, err := d.getTvLink(ctx, file, args, true); link != nil {
			return link, err
		}
	}

	setShareCookie(uc.Cookie)
	log.Infof("[%v] 获取%s文件直链 %v %v %v", uc.ID, name, file.GetName(), file.GetID(), file.GetSize())
	return d.saveAndLink(ctx, bindRequestDriver(uc), file.GetID(), args)
}

func (d *QuarkUCShare) getTvLink(ctx context.Context, file model.Obj, args model.LinkArgs, forceStream bool) (*model.Link, error) {
	tvName := "QuarkTV"
	if d.config.Name == "UCShare" {
		tvName = "UCTV"
	}
	idx := int(tvAccountIdx.Add(1))
	storage := op.GetFirstDriver(tvName, idx)
	if storage == nil {
		return nil, nil
	}
	uc := storage.(*quark_uc_tv.QuarkUCTV)
	if uc.Cookie == "" {
		return nil, nil
	}
	setShareCookie(uc.Cookie)
	log.Infof("[%v] 获取%s文件直链 %v %v %v", uc.ID, tvName, file.GetName(), file.GetID(), file.GetSize())
	binding := bindTVRequestDriver(uc)
	binding.forceStream = forceStream
	link, err := d.saveAndLink(ctx, binding, file.GetID(), args)
	if err != nil {
		return nil, err
	}
	return link, nil
}

var _ driver.Driver = (*QuarkUCShare)(nil)
