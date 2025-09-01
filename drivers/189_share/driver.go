package _189_share

import (
	"context"
	"errors"
	_189pc "github.com/OpenListTeam/OpenList/v4/drivers/189pc"
	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
	"path/filepath"
)

type Cloud189Share struct {
	model.Storage
	Addition
	client *resty.Client
}

func (d *Cloud189Share) Config() driver.Config {
	return config
}

func (d *Cloud189Share) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *Cloud189Share) Init(ctx context.Context) error {
	d.client = base.NewRestyClient().SetHeaders(map[string]string{
		"Accept":  "application/json;charset=UTF-8",
		"Referer": "https://cloud.189.cn",
	})

	if conf.LazyLoad && !conf.StoragesLoaded {
		return nil
	}

	return d.Validate()
}

func (d *Cloud189Share) Drop(ctx context.Context) error {
	return nil
}

func (d *Cloud189Share) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	files, err := d.getShareFiles(ctx, dir)
	if err != nil {
		return nil, err
	}
	return utils.SliceConvert(files, func(src FileObj) (model.Obj, error) {
		src.Path = filepath.Join(dir.GetPath(), src.GetID())
		return &src, nil
	})
}

func (d *Cloud189Share) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	err := limiter.WaitN(ctx, 1)
	if err != nil {
		return nil, err
	}

	_, ok := file.(*FileObj)
	if !ok {
		return nil, errors.New("文件格式错误")
	}

	count := op.GetDriverCount("189CloudPC")
	for i := 0; i < count; i++ {
		link, err := d.link(ctx, file)
		if err == nil {
			return link, nil
		}
	}
	return nil, err
}

func (d *Cloud189Share) link(ctx context.Context, file model.Obj) (*model.Link, error) {
	storage := op.GetFirstDriver("189CloudPC", idx)
	idx++
	if storage == nil {
		return nil, errors.New("找不到天翼云盘帐号")
	}
	cloud189PC := storage.(*_189pc.Cloud189PC)
	log.Infof("[%v] 获取天翼云盘文件直链 %v %v %v", cloud189PC.ID, file.GetName(), file.GetID(), file.GetSize())

	shareInfo, err := d.getShareInfo()
	if err != nil {
		return nil, err
	}

	link, err := cloud189PC.GetShareLink(shareInfo.ShareId, file)
	if link != nil {
		return link, nil
	} else {
		log.Warnf("[%v] Get share link error: %v", cloud189PC.ID, err)
	}

	fileObject, _ := file.(*FileObj)
	log.Infof("[%v] 获取天翼云盘转存链接 %v %v", cloud189PC.ID, file.GetName(), file.GetID())
	link, err = cloud189PC.Transfer(ctx, shareInfo.ShareId, fileObject.ID, fileObject.oldName)
	return link, err
}

var _ driver.Driver = (*Cloud189Share)(nil)
