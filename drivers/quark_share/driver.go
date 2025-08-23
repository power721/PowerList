package quark_share

import (
	"context"
	"errors"
	quark "github.com/OpenListTeam/OpenList/v4/drivers/quark_uc"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/internal/token"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	log "github.com/sirupsen/logrus"
)

type QuarkShare struct {
	model.Storage
	Addition
}

func (d *QuarkShare) Config() driver.Config {
	return config
}

func (d *QuarkShare) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *QuarkShare) Init(ctx context.Context) error {
	if Cookie == "" {
		Cookie = token.GetAccountToken(conf.QUARK)
	}

	if conf.LazyLoad && !conf.StoragesLoaded {
		return nil
	}

	return d.Validate()
}

func (d *QuarkShare) Drop(ctx context.Context) error {
	return nil
}

func (d *QuarkShare) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	files, err := d.getShareFiles(dir.GetID())
	if err != nil {
		log.Warnf("list files error: %v", err)
		return nil, err
	}
	return utils.SliceConvert(files, func(src File) (model.Obj, error) {
		return fileToObj(src), nil
	})
}

func (d *QuarkShare) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	count := op.GetDriverCount("Quark")
	var err error
	for i := 0; i < count; i++ {
		link, err := d.link(ctx, file, args)
		if err == nil {
			return link, nil
		}
	}
	return nil, err
}

func (d *QuarkShare) link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	storage := op.GetFirstDriver("Quark", idx)
	idx++
	if storage == nil {
		return nil, errors.New("找不到夸克网盘帐号")
	}
	uc := storage.(*quark.QuarkOrUC)
	log.Infof("[%v] 获取夸克文件直链 %v %v %v", uc.ID, file.GetName(), file.GetID(), file.GetSize())
	newFile, err := d.saveFile(uc, file.GetID())
	if err != nil {
		return nil, err
	}

	link, err := d.getDownloadUrl(ctx, uc, newFile, args)
	return link, err
}

var _ driver.Driver = (*QuarkShare)(nil)
