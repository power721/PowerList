package _115

import (
	"context"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	driver115 "github.com/power721/115driver/pkg/driver"
)

// ShareCreateResult 永久分享创建结果。
type ShareCreateResult struct {
	ShareCode   string `json:"share_code"`
	ReceiveCode string `json:"receive_code"`
	ShareURL    string `json:"share_url"`
	ShareTitle  string `json:"share_title"`
}

// driver115Client 分享创建的最小客户端契约(生产实现即 *driver115.Pan115Client,抽薄便于单测替换)。
type driver115Client interface {
	CreatePermanentShare(fileIDs string) (*driver115.ShareSendResp, error)
}

// CreatePermanentShare 为账号内目录创建永久(长期)分享链接,
// 委托 115driver 的 CreatePermanentShare:share/send(ignore_warn=1)创建后默认仅 15 天,
// 紧跟 updateshare(share_duration=-1) 改长期,两步缺一不可。
// 115 分享是快照语义:内容为创建时刻目录快照,建后删除盘内源文件不影响分享,
// 追加内容必须新建分享。仅 cookie 版账号可用(开放平台无分享 API)。
func (d *Pan115) CreatePermanentShare(ctx context.Context, dir model.Obj) (*ShareCreateResult, error) {
	return d.createPermanentShare(ctx, d.GetClient(), dir)
}

func (d *Pan115) createPermanentShare(ctx context.Context, client driver115Client, dir model.Obj) (*ShareCreateResult, error) {
	if err := d.WaitLimit(ctx); err != nil {
		return nil, err
	}
	result, err := client.CreatePermanentShare(dir.GetID())
	if err != nil {
		return nil, err
	}
	return &ShareCreateResult{
		ShareCode:   result.Data.ShareCode,
		ReceiveCode: result.Data.ReceiveCode,
		ShareURL:    result.Data.ShareURL,
		ShareTitle:  result.Data.ShareTitle,
	}, nil
}
