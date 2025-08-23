package thunder_share

import (
	"context"
	"errors"
	"github.com/OpenListTeam/OpenList/v4/drivers/thunder_browser"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	log "github.com/sirupsen/logrus"
)

type ThunderShare struct {
	model.Storage
	Addition
}

func (d *ThunderShare) Config() driver.Config {
	return config
}

func (d *ThunderShare) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *ThunderShare) Init(ctx context.Context) error {
	if conf.LazyLoad && !conf.StoragesLoaded {
		return nil
	}

	return d.Validate()
}

func (d *ThunderShare) Drop(ctx context.Context) error {
	return nil
}

func (d *ThunderShare) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	files, err := d.listShareFiles(ctx, dir)
	if err != nil {
		log.Warnf("list Thunder files error: %v", err)
		return nil, err
	}
	return files, err
}

func (d *ThunderShare) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	count := op.GetDriverCount("ThunderBrowser")
	var err error
	for i := 0; i < count; i++ {
		link, err := d.link(ctx, file, args)
		if err == nil {
			return link, nil
		}
	}
	return nil, err
}

func (d *ThunderShare) link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	storage := op.GetFirstDriver("ThunderBrowser", idx)
	idx++
	if storage == nil {
		return nil, errors.New("找不到迅雷云盘帐号")
	}
	thunder := storage.(*thunder_browser.ThunderBrowser)
	log.Infof("[%v] 获取迅雷云盘文件直链 %v %v %v", thunder.ID, file.GetName(), file.GetID(), file.GetSize())
	fileId, err := d.saveFile(ctx, thunder, file)
	if err != nil {
		log.Warnf("[%v] 保存迅雷文件失败: %v", thunder.ID, err)
		return nil, err
	}

	link, err := d.getDownloadUrl(ctx, thunder, fileId)
	return link, err
}

var _ driver.Driver = (*ThunderShare)(nil)
