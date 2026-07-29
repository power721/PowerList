package aliyundrive_share2_open

import (
	"context"
	_115 "github.com/OpenListTeam/OpenList/v4/drivers/115"
	_123rapid "github.com/OpenListTeam/OpenList/v4/drivers/123_rapid"
	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/cache"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	"github.com/OpenListTeam/OpenList/v4/pkg/cron"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type AliyundriveShare2Open struct {
	base string
	model.Storage
	Addition
	cron *cron.Cron
}

var aliyundriveShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)

var aliyundriveShareAliTo115Enabled = func() bool {
	return setting.GetBool(conf.AliTo115)
}

var aliyundriveShareAliTo123Enabled = func() bool {
	return setting.GetBool(conf.AliTo123)
}

// rapidAliTo123 把阿里文件按 SHA1(content_hash)秒传到 123。声明为 var 便于单测替换。
var rapidAliTo123 = func(ctx context.Context, file *MyFile) *model.Link {
	if file == nil {
		return nil
	}
	sha1 := file.HashInfo.GetHash(utils.SHA1)
	if len(sha1) != utils.SHA1.Width {
		return nil
	}
	link, err := _123rapid.RapidTo123(ctx, _123rapid.Source{
		HashType: utils.SHA1,
		Hash:     sha1,
		Name:     file.Name,
		Size:     file.Size,
	})
	if err != nil || link == nil {
		log.Debugf("[ali-share] rapid to 123 skipped: %v", err)
		return nil
	}
	log.Infof("[ali-share] 使用123秒传直链: %s", file.Name)
	return link
}

func aliyundriveShareLinkCacheKey(fileID string) string {
	return fileID + "|" + strconv.FormatBool(aliyundriveShareAliTo115Enabled()) + "|" + strconv.FormatBool(aliyundriveShareAliTo123Enabled())
}

var resolveAliyundriveShareLink = func(ctx context.Context, d *AliyundriveShare2Open, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	count := op.GetDriverCount("AliyundriveOpen")
	var lastErr error
	for i := 0; i < count; i++ {
		link, myFile, err := d.aliLink(file)
		if err == nil {
			if strings.HasSuffix(file.GetName(), ".md") {
				return link, nil
			}
			// 123 优先:阿里 content_hash 即 SHA1,与 115 同构,直接喂 123 sha1_reuse;未命中回退 115/阿里。
			if aliyundriveShareAliTo123Enabled() {
				if l := rapidAliTo123(ctx, myFile); l != nil {
					return l, nil
				}
			}
			if !aliyundriveShareAliTo115Enabled() {
				return link, nil
			}

			return d.p115Link(ctx, link, myFile, args)
		}
		lastErr = err
	}
	return nil, lastErr
}

func (d *AliyundriveShare2Open) Config() driver.Config {
	return config
}

func (d *AliyundriveShare2Open) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *AliyundriveShare2Open) Init(ctx context.Context) error {
	if conf.LazyLoad && !conf.StoragesLoaded {
		return nil
	}

	err := d.Validate()
	time.Sleep(1500 * time.Millisecond)
	return err
}

func (d *AliyundriveShare2Open) Drop(ctx context.Context) error {
	if d.cron != nil {
		d.cron.Stop()
	}
	return nil
}

func (d *AliyundriveShare2Open) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	return d.list(ctx, dir)
}

func (d *AliyundriveShare2Open) list(ctx context.Context, dir model.Obj) ([]model.Obj, error) {
	if d.ShareToken == "" {
		err := d.getShareToken()
		if err != nil {
			log.Warnf("getShareToken error: %v", err)
			return nil, err
		}
	}

	files, err := d.getFiles(dir.GetID())
	if err != nil {
		log.Warnf("list files error: %v", err)
		return nil, err
	}
	return utils.SliceConvert(files, func(src File) (model.Obj, error) {
		return fileToObj(src), nil
	})
}

func (d *AliyundriveShare2Open) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	key := aliyundriveShareLinkCacheKey(file.GetID())
	if link, ok := aliyundriveShareLinkCache.Get(key); ok {
		return link, nil
	}

	link, err := resolveAliyundriveShareLink(ctx, d, file, args)
	if err == nil && link != nil {
		aliyundriveShareLinkCache.Set(key, link)
	}
	return link, err
}

func (d *AliyundriveShare2Open) aliLink(file model.Obj) (*model.Link, *MyFile, error) {
	ali, err := getAliOpenDriver(idx)
	idx++
	if err != nil {
		return nil, nil, err
	}
	log.Infof("[%v] 获取阿里云盘文件直链 %v %v %v %v", ali.ID, ali.DriveId, file.GetName(), file.GetID(), file.GetSize())
	fileId, err := d.saveFile(ali, file.GetID())
	if err != nil {
		return nil, nil, err
	}

	newFile := MyFile{
		FileId: fileId,
		Name:   "livp",
	}

	link, hash, err := d.getOpenLink(ali, newFile)
	if err != nil {
		return nil, nil, err
	}

	myFile := MyFile{
		FileId:   fileId,
		Name:     file.GetName(),
		Size:     file.GetSize(),
		HashInfo: utils.NewHashInfo(utils.SHA1, hash),
	}

	return link, &myFile, nil
}

func (d *AliyundriveShare2Open) p115Link(ctx context.Context, link *model.Link, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	count := op.GetDriverCount("115 Cloud")
	for i := 0; i < count; i++ {
		driver115 := op.Get115Driver(idx2)
		idx2++
		if driver115 != nil {
			link115, err2 := d.saveTo115(ctx, driver115.(*_115.Pan115), file, link, args)
			if err2 == nil {
				return link115, nil
			}
		} else {
			break
		}
	}
	return link, nil
}

func (d *AliyundriveShare2Open) Other(ctx context.Context, args model.OtherArgs) (interface{}, error) {
	ali, err := getAliOpenDriver(idx)
	if err != nil {
		return nil, err
	}

	if args.Method == "share_info" {
		d.getShareToken()
		data := base.Json{
			"shareId":    d.ShareId,
			"sharePwd":   d.SharePwd,
			"shareToken": d.ShareToken,
			"fileId":     args.Obj.GetID(),
		}
		return data, nil
	}

	if args.Method != "video_preview" {
		return nil, errs.NotSupport
	}

	log.Infof("[%v] 获取文件链接 %v %v %v %v", ali.ID, ali.DriveId, args.Obj.GetID(), args.Obj.GetName(), args.Obj.GetSize())
	fileId, err := d.saveFile(ali, args.Obj.GetID())
	idx++
	if err != nil {
		return nil, err
	}

	var resp VideoPreviewResponse
	var uri string
	data := base.Json{
		"drive_id": ali.DriveId,
		"file_id":  fileId,
	}
	switch args.Method {
	case "video_preview":
		uri = "/adrive/v1.0/openFile/getVideoPreviewPlayInfo"
		data["category"] = "live_transcoding"
		data["url_expire_sec"] = 14400
	default:
		return nil, errs.NotSupport
	}
	_, err = ali.Request(uri, http.MethodPost, func(req *resty.Request) {
		req.SetBody(data).SetResult(&resp)
	})

	go d.deleteDelay(ali, fileId)

	if err != nil {
		log.Errorf("获取文件链接失败：%v", err)
		return nil, err
	}

	if args.Data == "preview" {
		url, _, _ := d.getDownloadUrl(ali, fileId)
		if url != "" {
			resp.PlayInfo.Videos = append(resp.PlayInfo.Videos, LiveTranscoding{
				TemplateId: "原画",
				Status:     "finished",
				Url:        url,
			})
		}
	}

	return resp, nil
}

var _ driver.Driver = (*AliyundriveShare2Open)(nil)
