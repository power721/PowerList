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

var quarkUCShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)

var resolveQuarkUCShareLink = func(ctx context.Context, d *QuarkUCShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	name := d.getDriverName()
	count := op.GetDriverCount(name)
	var lastErr error
	for i := 0; i < count; i++ {
		link, err := d.link(ctx, file, args)
		if err == nil {
			return link, nil
		}
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
	if Cookie == "" {
		Cookie = token.GetAccountToken(key)
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

	// 按主账号类型选主路径,两条路互为兜底:
	//   有 SVIP 主账号 → 转存(save+download 原画,全速,超大文件/ISO 可靠)为主,免转存兜底
	//   否则(非 SVIP / 无账号) → 免转存(share-direct,原画、省空间省等待)为主,转存兜底
	// 超大文件(如 ISO)免转存直链不稳,SVIP 走转存更可靠。
	var link *model.Link
	var err error
	if accountIsSVIP(d) {
		link, err = resolveQuarkUCShareLink(ctx, d, file, args)
		if err != nil || link == nil {
			log.Warnf("SVIP 转存失败,回退免转存: %v", err)
			link, err = resolveShareDirectLink(d, file)
		}
	} else {
		link, err = resolveShareDirectLink(d, file)
		if err != nil || link == nil {
			log.Warnf("免转存失败,回退转存: %v", err)
			link, err = resolveQuarkUCShareLink(ctx, d, file, args)
		}
	}
	if err == nil && link != nil {
		quarkUCShareLinkCache.Set(key, link)
	}
	return link, err
}

func (d *QuarkUCShare) link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	if setting.GetBool(conf.UssQuarkTv) {
		link, err := d.getTvLink(ctx, file, args, false)
		if link != nil {
			return link, err
		}
	}

	name := d.getDriverName()
	storage := op.GetFirstDriver(name, idx)
	idx++
	if storage == nil {
		return nil, errors.New(fmt.Sprintf("找不到%s网盘帐号", name))
	}
	uc := storage.(*quark.QuarkOrUC)
	if !uc.VIP {
		link, err := d.getTvLink(ctx, file, args, true)
		if link != nil {
			return link, err
		}
	}

	Cookie = uc.Cookie
	log.Infof("[%v] 获取%s文件直链 %v %v %v", uc.ID, name, file.GetName(), file.GetID(), file.GetSize())
	binding := bindRequestDriver(uc)
	newFile, err := d.saveFile(binding, uc, file.GetID())
	if err != nil {
		return nil, err
	}

	link, err := d.getDownloadUrl(ctx, uc, newFile, args)
	return link, err
}

func (d *QuarkUCShare) getTvLink(ctx context.Context, file model.Obj, args model.LinkArgs, forceStream bool) (*model.Link, error) {
	var tvName string
	if d.config.Name == "UCShare" {
		tvName = "UCTV"
	} else {
		tvName = "QuarkTV"
	}
	storage := op.GetFirstDriver(tvName, idx2)
	idx2++
	if storage != nil {
		uc := storage.(*quark_uc_tv.QuarkUCTV)
		if uc.Cookie != "" {
			Cookie = uc.Cookie
			log.Infof("[%v] 获取%s文件直链 %v %v %v", uc.ID, tvName, file.GetName(), file.GetID(), file.GetSize())
			binding := bindTVRequestDriver(uc)
			newFile, err := d.saveTvFile(ctx, binding, uc, file.GetID())
			if err != nil {
				return nil, err
			}

			videoLinkMethod := uc.Addition.VideoLinkMethod
			if forceStream {
				uc.Addition.VideoLinkMethod = "streaming"
			}
			link, err := d.getTvDownloadUrl(ctx, uc, newFile, args)
			uc.Addition.VideoLinkMethod = videoLinkMethod
			if link != nil && uc.VideoLinkMethod == "streaming" {
				link.URL = link.URL + "#proxy=0"
			}
			return link, err
		}
	}
	return nil, nil
}

var _ driver.Driver = (*QuarkUCShare)(nil)
