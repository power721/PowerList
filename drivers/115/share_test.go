package _115

import (
	"context"
	"errors"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	driver115 "github.com/power721/115driver/pkg/driver"
)

// stub115Client 桩 115driver 客户端:记录传入的 file_ids 并返回预设结果/错误。
// 两步 share/send + updateshare 的 HTTP 契约已在 115driver 侧覆盖
// (pkg/driver/share_create_test.go),这里只验驱动的委托与结果组装。
type stub115Client struct {
	gotFileIDs string
	resp       *driver115.ShareSendResp
	err        error
}

func (s *stub115Client) CreatePermanentShare(fileIDs string) (*driver115.ShareSendResp, error) {
	s.gotFileIDs = fileIDs
	return s.resp, s.err
}

func shareSendResp() *driver115.ShareSendResp {
	resp := &driver115.ShareSendResp{}
	resp.State = true
	resp.Data.ShareCode = "swsexqo3hjs"
	resp.Data.ReceiveCode = "6666"
	resp.Data.ShareURL = "https://115cdn.com/s/swsexqo3hjs"
	resp.Data.ShareTitle = "剧名 (2024)"
	return resp
}

// 驱动把目录对象的 115 cid 原样传给客户端,并组装端点响应结构。
func TestPan115CreatePermanentShareDelegates(t *testing.T) {
	stub := &stub115Client{resp: shareSendResp()}
	d := &Pan115{}
	result, err := d.createPermanentShare(context.Background(), stub,
		&model.Object{ID: "3189661663297662199", Name: "剧名 (2024)", IsFolder: true})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if stub.gotFileIDs != "3189661663297662199" {
		t.Fatalf("file_ids passed = %q, want the directory cid", stub.gotFileIDs)
	}
	if result.ShareCode != "swsexqo3hjs" || result.ReceiveCode != "6666" {
		t.Fatalf("result: got %+v", result)
	}
	if result.ShareURL != "https://115cdn.com/s/swsexqo3hjs" || result.ShareTitle != "剧名 (2024)" {
		t.Fatalf("result: got %+v", result)
	}
}

// 客户端报错(115 API 失败/未登录等)原样上抛,由端点层转给调用方。
func TestPan115CreatePermanentShareErrorPropagates(t *testing.T) {
	stub := &stub115Client{err: errors.New("分享次数已达上限")}
	d := &Pan115{}
	_, err := d.createPermanentShare(context.Background(), stub, &model.Object{ID: "1", IsFolder: true})
	if err == nil || err.Error() != "分享次数已达上限" {
		t.Fatalf("expected passthrough, got %v", err)
	}
}
