package _115_share

import (
	"context"
	"errors"
	"fmt"
	_115 "github.com/OpenListTeam/OpenList/v4/drivers/115"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	log "github.com/sirupsen/logrus"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	driver115 "github.com/power721/115driver/pkg/driver"
	"golang.org/x/time/rate"
)

type Pan115Share struct {
	model.Storage
	Addition
	limiter *rate.Limiter
}

func (d *Pan115Share) Config() driver.Config {
	return config
}

func (d *Pan115Share) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *Pan115Share) Init(ctx context.Context) error {
	//if d.LimitRate > 0 {
	//	d.limiter = rate.NewLimiter(rate.Limit(d.LimitRate), 1)
	//}

	if conf.LazyLoad && !conf.StoragesLoaded {
		return nil
	}

	return d.Validate()
}

func (d *Pan115Share) WaitLimit(ctx context.Context) error {
	if d.limiter != nil {
		return d.limiter.Wait(ctx)
	}
	return nil
}

func (d *Pan115Share) Drop(ctx context.Context) error {
	return nil
}

func (d *Pan115Share) Validate() error {
	pan115 := op.Get115Driver(idx)
	if pan115 == nil {
		return errors.New("找不到115云盘帐号")
	}
	client := pan115.(*_115.Pan115).GetClient()
	_, err := client.GetShareSnap(d.ShareCode, d.ReceiveCode, "0", driver115.QueryLimit(1))
	return err
}

func (d *Pan115Share) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	storage := op.Get115Driver(idx)
	if storage == nil {
		return []model.Obj{}, errors.New("找不到115云盘帐号")
	}
	pan115 := storage.(*_115.Pan115)
	if err := pan115.WaitLimit(ctx); err != nil {
		return nil, err
	}
	client := pan115.GetClient()

	files := make([]driver115.ShareFile, 0)
	fileResp, err := client.GetShareSnap(d.ShareCode, d.ReceiveCode, dir.GetID(), driver115.QueryLimit(int(pan115.PageSize)))
	if err != nil {
		return nil, err
	}
	files = append(files, fileResp.Data.List...)
	total := fileResp.Data.Count
	count := len(fileResp.Data.List)
	for total > count {
		fileResp, err := client.GetShareSnap(
			d.ShareCode, d.ReceiveCode, dir.GetID(),
			driver115.QueryLimit(int(pan115.PageSize)), driver115.QueryOffset(count),
		)
		if err != nil {
			return nil, err
		}
		files = append(files, fileResp.Data.List...)
		count += len(fileResp.Data.List)
	}

	return utils.SliceConvert(files, transFunc)
}

func (d *Pan115Share) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	count := op.GetDriverCount("115 Cloud")
	var err error
	for i := 0; i < count; i++ {
		link, err := d.link(ctx, file, args)
		if err == nil {
			return link, nil
		}
	}
	return nil, err
}

func (d *Pan115Share) link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	storage := op.Get115Driver(idx)
	idx++
	if storage == nil {
		return nil, errors.New("找不到115云盘帐号")
	}
	pan115 := storage.(*_115.Pan115)
	if err := pan115.WaitLimit(ctx); err != nil {
		return nil, err
	}
	client := pan115.GetClient()
	log.Infof("[%v] 获取115文件直链 %v %v %v", pan115.ID, file.GetName(), file.GetID(), file.GetSize())

	parts := strings.Split(file.GetID(), "-")
	fid := parts[0]
	sha1 := parts[1]
	downloadInfo, err := client.DownloadByShareCode(d.ShareCode, d.ReceiveCode, fid)
	if err != nil {
		return nil, err
	}

	go delayDelete115(pan115, sha1)
	exp := 4 * time.Hour
	return &model.Link{
		URL:         downloadInfo.URL.URL + fmt.Sprintf("#storageId=%d", pan115.ID),
		Expiration:  &exp,
		Concurrency: pan115.Concurrency,
		PartSize:    pan115.ChunkSize * utils.KB,
	}, nil
}

func delayDelete115(pan115 *_115.Pan115, sha1 string) {
	delayTime := setting.GetInt(conf.DeleteDelayTime, 900)
	if delayTime == 0 {
		return
	}

	log.Infof("[%v] Delete 115 temp file %v after %v seconds.", pan115.ID, sha1, delayTime)
	time.Sleep(time.Duration(delayTime) * time.Second)
	pan115.DeleteReceivedFile(sha1)
}

func (d *Pan115Share) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) error {
	return errs.NotSupport
}

func (d *Pan115Share) Move(ctx context.Context, srcObj, dstDir model.Obj) error {
	return errs.NotSupport
}

func (d *Pan115Share) Rename(ctx context.Context, srcObj model.Obj, newName string) error {
	return errs.NotSupport
}

func (d *Pan115Share) Copy(ctx context.Context, srcObj, dstDir model.Obj) error {
	return errs.NotSupport
}

func (d *Pan115Share) Remove(ctx context.Context, obj model.Obj) error {
	return errs.NotSupport
}

func (d *Pan115Share) Put(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) error {
	return errs.NotSupport
}

var _ driver.Driver = (*Pan115Share)(nil)
