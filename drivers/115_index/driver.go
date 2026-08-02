package _115_index

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	_115 "github.com/OpenListTeam/OpenList/v4/drivers/115"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/index115"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	log "github.com/sirupsen/logrus"

	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

type Pan115Index struct {
	model.Storage
	Addition
}

func (d *Pan115Index) Config() driver.Config {
	return config
}

func (d *Pan115Index) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *Pan115Index) Init(ctx context.Context) error {
	log.Info("Pan115Index init")
	return nil
}

func (d *Pan115Index) Drop(ctx context.Context) error {
	return nil
}

func (d *Pan115Index) Validate() error {
	return nil
}

func (d *Pan115Index) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	id := dir.GetID()
	if id == "/" {
		id = ""
	}
	parts := strings.Split(id, ":")
	shareCode := ""
	if len(parts) > 0 {
		shareCode = parts[0]
	}
	receiveCode := ""
	if len(parts) > 1 {
		receiveCode = parts[1]
	}
	fid := ""
	if len(parts) > 2 {
		fid = parts[2]
	}

	var req = index115.BrowseRequest{
		ShareCode:   shareCode,
		ReceiveCode: receiveCode,
		ParentID:    fid,
	}
	log.Debugf("browse: req: %+v", req)
	files, err := index115.MyIndex115Service.Browse(ctx, req)
	if err != nil {
		return nil, err
	}
	return utils.SliceConvert(files, transFunc)
}

func (d *Pan115Index) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
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

func (d *Pan115Index) link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
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

	parts := strings.Split(file.GetID(), ":")
	shareCode := parts[0]
	receiveCode := parts[1]
	fid := parts[2]
	sha1 := parts[3]
	downloadInfo, err := client.DownloadByShareCode(shareCode, receiveCode, fid)
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

func (d *Pan115Index) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) error {
	return errs.NotSupport
}

func (d *Pan115Index) Move(ctx context.Context, srcObj, dstDir model.Obj) error {
	return errs.NotSupport
}

func (d *Pan115Index) Rename(ctx context.Context, srcObj model.Obj, newName string) error {
	return errs.NotSupport
}

func (d *Pan115Index) Copy(ctx context.Context, srcObj, dstDir model.Obj) error {
	return errs.NotSupport
}

func (d *Pan115Index) Remove(ctx context.Context, obj model.Obj) error {
	return errs.NotSupport
}

func (d *Pan115Index) Put(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) error {
	return errs.NotSupport
}

var _ driver.Driver = (*Pan115Index)(nil)
