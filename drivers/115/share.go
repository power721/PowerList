package _115

import (
	"context"
	"errors"
	"strconv"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	driver115 "github.com/power721/115driver/pkg/driver"
)

// shareSendURL / shareUpdateURL 创建与更新分享端点;声明为 var 便于单测替换
// (与 115_share 的 shareReceiveURL 同款手法)。
// 契约来源:2026-09-12 web 抓包。115driver df29f4e 起提供正式封装
// (CreatePermanentShare),待 go.mod 升级发版后本内联实现可切换移除。
var (
	shareSendURL   = "https://webapi.115.com/share/send"
	shareUpdateURL = "https://webapi.115.com/share/updateshare"
)

type shareSendResp struct {
	driver115.BasicResp
	Data struct {
		TotalSize       int64  `json:"total_size"`
		ShareTitle      string `json:"share_title"`
		FileCount       int    `json:"file_count"`
		FolderCount     int    `json:"folder_count"`
		FileCategory    int    `json:"file_category"`
		ReceiveCode     string `json:"receive_code"`
		ShareExDuration string `json:"share_ex_duration"`
		ShareExTime     int64  `json:"share_ex_time"`
		ShareCode       string `json:"share_code"`
		ShareURL        string `json:"share_url"`
		ShareCommand    string `json:"share_command"`
	} `json:"data"`
}

// ShareCreateResult 永久分享创建结果。
type ShareCreateResult struct {
	ShareCode   string `json:"share_code"`
	ReceiveCode string `json:"receive_code"`
	ShareURL    string `json:"share_url"`
	ShareTitle  string `json:"share_title"`
}

// CreatePermanentShare 为账号内目录创建永久(长期)分享链接。
// 115 分享是快照语义:内容为创建时刻目录快照,建后删除盘内源文件不影响分享,
// 追加内容必须新建分享。两步缺一不可:share/send(ignore_warn=1)创建后默认仅
// 15 天,须紧跟 updateshare(share_duration=-1) 改长期。仅 cookie 版账号可用。
func (d *Pan115) CreatePermanentShare(ctx context.Context, dir model.Obj) (*ShareCreateResult, error) {
	return d.createPermanentShare(ctx, d.GetClient(), dir)
}

func (d *Pan115) createPermanentShare(ctx context.Context, client *driver115.Pan115Client, dir model.Obj) (*ShareCreateResult, error) {
	if err := d.WaitLimit(ctx); err != nil {
		return nil, err
	}
	userID := client.UserID
	if userID == 0 {
		return nil, errors.New("115 user id unknown (LoginCheck not run)")
	}

	result := shareSendResp{}
	resp, err := client.NewRequest().
		SetFormData(map[string]string{
			"user_id":     strconv.FormatInt(userID, 10),
			"file_ids":    dir.GetID(),
			"ignore_warn": "1",
			"is_asc":      "0",
			"order":       "file_name",
		}).
		SetHeader("Referer", "https://115.com/").
		ForceContentType("application/json;charset=UTF-8").
		SetResult(&result).
		Post(shareSendURL)
	if err != nil {
		return nil, err
	}
	if !result.State {
		msg := result.Error
		if msg == "" {
			msg = resp.String()
		}
		return nil, errors.New(msg)
	}
	if result.Data.ShareCode == "" {
		return nil, errors.New("share/send succeeded but no share_code returned")
	}

	if err := d.WaitLimit(ctx); err != nil {
		return nil, err
	}
	update := driver115.BasicResp{}
	resp2, err := client.NewRequest().
		SetFormData(map[string]string{
			"share_code":     result.Data.ShareCode,
			"share_duration": "-1",
		}).
		SetHeader("Referer", "https://cdnres.115.com/").
		ForceContentType("application/json;charset=UTF-8").
		SetResult(&update).
		Post(shareUpdateURL)
	if err != nil {
		return nil, err
	}
	if !update.State {
		msg := update.Error
		if msg == "" {
			msg = resp2.String()
		}
		return nil, errors.New("分享 " + result.Data.ShareCode + " 改长期失败: " + msg)
	}

	return &ShareCreateResult{
		ShareCode:   result.Data.ShareCode,
		ReceiveCode: result.Data.ReceiveCode,
		ShareURL:    result.Data.ShareURL,
		ShareTitle:  result.Data.ShareTitle,
	}, nil
}
