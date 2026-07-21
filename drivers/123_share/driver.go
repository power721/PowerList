package _123Share

import (
	"context"
	"errors"
	"fmt"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"

	_123 "github.com/OpenListTeam/OpenList/v4/drivers/123"
	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
)

type Pan123Share struct {
	model.Storage
	Addition
	apiRateLimit sync.Map
	ref          *_123.Pan123
}

func (d *Pan123Share) Config() driver.Config {
	return config
}

func (d *Pan123Share) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *Pan123Share) Init(ctx context.Context) error {
	if conf.LazyLoad && !conf.StoragesLoaded {
		return nil
	}
	return d.Validate()
}

func (d *Pan123Share) InitReference(storage driver.Driver) error {
	refStorage, ok := storage.(*_123.Pan123)
	if ok {
		d.ref = refStorage
		return nil
	}
	return fmt.Errorf("ref: storage is not 123Pan")
}

func (d *Pan123Share) Drop(ctx context.Context) error {
	d.ref = nil
	return nil
}

func (d *Pan123Share) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	// TODO return the files list, required
	files, err := d.getFiles(ctx, dir.GetID())
	if err != nil {
		return nil, err
	}
	return utils.SliceConvert(files, func(src File) (model.Obj, error) {
		return src, nil
	})
}

// resolveAnonLink 匿名换链入口,声明为 var 以便单测替换(规避真实网络/op 依赖)。
var resolveAnonLink = func(d *Pan123Share, f File, ip string) (*model.Link, error) {
	return d.anonDownloadLink(f, ip)
}

func (d *Pan123Share) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	// 匿名优先:公开分享可免登录换直链,无需 123Pan 账号。
	if f, ok := file.(File); ok {
		if link, err := resolveAnonLink(d, f, args.IP); err == nil {
			return link, nil
		} else if errors.Is(err, err123TrafficLimit) {
			// 分享方流量耗尽,账号重试也无意义,直接返回真实原因。
			return nil, err
		}
	}
	count := op.GetDriverCount("123Pan")
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

func (d *Pan123Share) link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	storage := op.GetFirstDriver("123Pan", idx)
	idx++
	if storage == nil {
		return nil, errors.New("找不到123云盘帐号")
	}
	pan123 := storage.(*_123.Pan123)
	log.Infof("[%v] 获取123文件直链 %v %v %v", pan123.ID, file.GetName(), file.GetID(), file.GetSize())
	f, ok := file.(File)
	if !ok {
		return nil, fmt.Errorf("can't convert obj")
	}
	var headers map[string]string
	if !utils.IsLocalIPAddr(args.IP) {
		headers = map[string]string{
			"X-Forwarded-For": args.IP,
		}
	}
	data := base.Json{
		"driveId":   "0",
		"shareKey":  d.ShareKey,
		"SharePwd":  d.SharePwd,
		"etag":      f.Etag,
		"fileId":    f.FileId,
		"s3keyFlag": f.S3KeyFlag,
		"FileName":  f.FileName,
		"size":      f.Size,
	}
	resp, err := pan123.Request(DownloadInfo, http.MethodPost, func(req *resty.Request) {
		req.SetBody(data).SetHeaders(headers)
	}, nil)
	if err != nil {
		return nil, err
	}
	return unwrap123DownloadLink(utils.Json.Get(resp, "data", "DownloadURL").ToString())
}

func (d *Pan123Share) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) error {
	// TODO create folder, optional
	return errs.NotSupport
}

func (d *Pan123Share) Move(ctx context.Context, srcObj, dstDir model.Obj) error {
	// TODO move obj, optional
	return errs.NotSupport
}

func (d *Pan123Share) Rename(ctx context.Context, srcObj model.Obj, newName string) error {
	// TODO rename obj, optional
	return errs.NotSupport
}

func (d *Pan123Share) Copy(ctx context.Context, srcObj, dstDir model.Obj) error {
	// TODO copy obj, optional
	return errs.NotSupport
}

func (d *Pan123Share) Remove(ctx context.Context, obj model.Obj) error {
	// TODO remove obj, optional
	return errs.NotSupport
}

func (d *Pan123Share) Put(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) error {
	// TODO upload file, optional
	return errs.NotSupport
}

//func (d *Pan123Share) Other(ctx context.Context, args model.OtherArgs) (interface{}, error) {
//	return nil, errs.NotSupport
//}

func (d *Pan123Share) APIRateLimit(ctx context.Context, api string) error {
	value, _ := d.apiRateLimit.LoadOrStore(api,
		rate.NewLimiter(rate.Every(700*time.Millisecond), 1))
	limiter := value.(*rate.Limiter)

	return limiter.Wait(ctx)
}

var _ driver.Driver = (*Pan123Share)(nil)
