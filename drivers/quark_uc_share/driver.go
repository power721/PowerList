package quark_uc_share

import (
	"context"
	"errors"
	"fmt"
	quark "github.com/OpenListTeam/OpenList/v4/drivers/quark_uc"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
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

	name := d.getDriverName()
	count := op.GetDriverCount(name)
	var err error
	for i := 0; i < count; i++ {
		link, err := d.link(ctx, file, args)
		if err == nil {
			return link, nil
		}
	}
	return nil, err
}

func (d *QuarkUCShare) link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	name := d.getDriverName()
	storage := op.GetFirstDriver(name, idx)
	idx++
	if storage == nil {
		return nil, errors.New(fmt.Sprintf("找不到%s网盘帐号", name))
	}
	uc := storage.(*quark.QuarkOrUC)
	Cookie = uc.Cookie
	log.Infof("[%v] 获取%s文件直链 %v %v %v", uc.ID, name, file.GetName(), file.GetID(), file.GetSize())
	newFile, err := d.saveFile(uc, file.GetID())
	if err != nil {
		return nil, err
	}

	link, err := d.getDownloadUrl(ctx, uc, newFile, args)
	return link, err
}

var _ driver.Driver = (*QuarkUCShare)(nil)
