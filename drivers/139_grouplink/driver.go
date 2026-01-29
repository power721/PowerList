package _139_grouplink

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	_139 "github.com/OpenListTeam/OpenList/v4/drivers/139"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	log "github.com/sirupsen/logrus"
	"time"
)

var _ driver.Driver = (*Yun139GroupLink)(nil)

func (d *Yun139GroupLink) Init(ctx context.Context) error {
	return nil
}

func (d *Yun139GroupLink) Drop(ctx context.Context) error {
	return nil
}

func (d *Yun139GroupLink) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	files, err := d.list(dir.GetID())
	if err != nil {
		log.Warnf("获取文件列表失败: %v", err)
		return nil, err
	}

	return utils.SliceConvert(files, func(src File) (model.Obj, error) {
		return src, nil
	})
}

func (d *Yun139GroupLink) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	f, ok := file.(File)
	if !ok {
		return nil, errors.New("文件格式错误")
	}

	if f.URL != "" {
		exp := 15 * time.Minute
		return &model.Link{
			URL:         f.URL,
			Expiration:  &exp,
			Concurrency: 5,
			PartSize:    10 * utils.MB,
		}, nil
	}

	count := op.GetDriverCount("139Yun")
	if count == 0 {
		return nil, errors.New("未配置139Yun账号，无法获取高速下载链接")
	}

	var lastErr error
	for i := 0; i < count; i++ {
		link, err := d.myLink(ctx, f)
		if err == nil {
			return link, nil
		}
		lastErr = err
		atomic.AddInt32(&idx, 1)
	}
	return nil, fmt.Errorf("所有%d个139Yun账号均获取直链失败：%v", count, lastErr)
}

func (d *Yun139GroupLink) myLink(ctx context.Context, f File) (*model.Link, error) {
	driverIdx := int(atomic.LoadInt32(&idx) % int32(op.GetDriverCount("139Yun")))
	storage := op.GetFirstDriver("139Yun", driverIdx)
	if storage == nil {
		return nil, errors.New("找不到139云盘账号")
	}
	yun139 := storage.(*_139.Yun139)
	log.Infof("[139Yun-%d] 为grouplink文件获取高速直链：%s（ID：%s）", yun139.ID, f.Name, f.ID)
	url, err := d.getDownloadUrl(f.ID)
	if err != nil {
		return nil, err
	}

	exp := 15 * time.Minute
	return &model.Link{
		URL:         url + fmt.Sprintf("#storageId=%d", yun139.ID),
		Expiration:  &exp,
		Concurrency: yun139.Concurrency, 
		PartSize:    yun139.ChunkSize,   
	}, nil
}
