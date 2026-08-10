package _123Share

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

// File 实现了 model.Obj,可直接作为 Link 入参。

// stubThumbDirect 让「无限直链」这一步失败,以便单测覆盖后面的回退分支。
func stubThumbDirect(t *testing.T) {
	orig := resolveThumbDirect
	resolveThumbDirect = func(d *Pan123Share, ctx context.Context, f File, ip string) (*model.Link, error) {
		return nil, errNoThumbLink
	}
	t.Cleanup(func() { resolveThumbDirect = orig })
}

// freshThumbUrl 造一条 t= 还没过期的缩略图形态签名直链。
func freshThumbUrl(expire time.Time) string {
	return fmt.Sprintf("https://user-app-pay-download-cdn.123295.com/123-159/5ea1c7ee/1814511190-0/"+
		"5ea1c7eeba7579136193ec5a2978549f/c-m9013_24_24?v=5&t=%d&r=81M0WB&bzc=1&bzs=303a303a303a30"+
		"&s=%da044f89bc26f32e992ab431e16efb846&bi=3322231004&filename=x.mkv&cache_type=1"+
		"&w=24&h=24&trade_key=123pan-thumbnail&type=video", expire.Unix(), expire.Unix())
}

// TestThumbDirectLink_UsesFreshListUrl 列表里的签名还够久时,直接剥离返回,不再请求任何接口。
func TestThumbDirectLink_UsesFreshListUrl(t *testing.T) {
	expire := time.Now().Add(7 * 24 * time.Hour)
	d := &Pan123Share{}
	f := File{FileName: "video.mkv", FileId: 1, Size: 19069605034, DownloadUrl: freshThumbUrl(expire)}

	link, err := d.thumbDirectLink(context.Background(), f, "")
	if err != nil {
		t.Fatalf("thumbDirectLink: %v", err)
	}
	if want := "/c-m9013?"; !strings.Contains(link.URL, want) {
		t.Fatalf("expected %q in %s", want, link.URL)
	}
	if strings.Contains(link.URL, "trade_key=123pan-thumbnail") {
		t.Fatalf("trade_key not stripped: %s", link.URL)
	}
	if link.Expiration == nil || *link.Expiration <= 0 {
		t.Fatalf("expected positive expiration, got %v", link.Expiration)
	}
	if link.Header.Get("Referer") != "https://user-app-pay-download-cdn.123295.com/" {
		t.Fatalf("unexpected referer: %q", link.Header.Get("Referer"))
	}
}

func TestThumbDirectLink_DirIsSkipped(t *testing.T) {
	d := &Pan123Share{}
	if _, err := d.thumbDirectLink(context.Background(), File{Type: 1}, ""); !errors.Is(err, errNoThumbLink) {
		t.Fatalf("expected errNoThumbLink for dir, got %v", err)
	}
}

// TestPan123ShareLink_UnlimitedFirst 无限直链可用时优先返回,不碰 share/download/info。
func TestPan123ShareLink_UnlimitedFirst(t *testing.T) {
	origThumb := resolveThumbDirect
	thumbCalls := 0
	resolveThumbDirect = func(d *Pan123Share, ctx context.Context, f File, ip string) (*model.Link, error) {
		thumbCalls++
		return &model.Link{URL: "https://example.com/unlimited"}, nil
	}
	t.Cleanup(func() { resolveThumbDirect = origThumb })

	origAnon := resolveAnonLink
	resolveAnonLink = func(d *Pan123Share, f File, ip string) (*model.Link, error) {
		t.Fatal("anon download/info should not be reached")
		return nil, nil
	}
	t.Cleanup(func() { resolveAnonLink = origAnon })

	d := &Pan123Share{}
	link, err := d.Link(context.Background(), File{FileName: "video.mp4"}, model.LinkArgs{})
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if link == nil || link.URL != "https://example.com/unlimited" {
		t.Fatalf("expected unlimited link, got %+v", link)
	}
	if thumbCalls != 1 {
		t.Fatalf("expected thumb resolver once, got %d", thumbCalls)
	}
}

// TestPan123ShareLink_UnlimitedDisabled 配置关掉后跳过无限直链,直接走 download/info。
func TestPan123ShareLink_UnlimitedDisabled(t *testing.T) {
	origThumb := resolveThumbDirect
	resolveThumbDirect = func(d *Pan123Share, ctx context.Context, f File, ip string) (*model.Link, error) {
		t.Fatal("thumb resolver should be skipped when disabled")
		return nil, nil
	}
	t.Cleanup(func() { resolveThumbDirect = origThumb })

	origAnon := resolveAnonLink
	resolveAnonLink = func(d *Pan123Share, f File, ip string) (*model.Link, error) {
		return &model.Link{URL: "https://example.com/anon-direct"}, nil
	}
	t.Cleanup(func() { resolveAnonLink = origAnon })

	d := &Pan123Share{}
	d.DisableUnlimited = true
	link, err := d.Link(context.Background(), File{FileName: "video.mp4"}, model.LinkArgs{})
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if link == nil || link.URL != "https://example.com/anon-direct" {
		t.Fatalf("expected anon link, got %+v", link)
	}
}

func TestPan123ShareLink_AnonymousFirstReturnsAnonLink(t *testing.T) {
	// 无限直链不可用时:匿名 download/info 成功即返回,不走账号路径。
	stubThumbDirect(t)
	origAnon := resolveAnonLink
	anonCalls := 0
	resolveAnonLink = func(d *Pan123Share, f File, ip string) (*model.Link, error) {
		anonCalls++
		return &model.Link{URL: "https://example.com/anon-direct"}, nil
	}
	t.Cleanup(func() { resolveAnonLink = origAnon })

	d := &Pan123Share{}
	file := File{FileName: "video.mp4"}

	link, err := d.Link(context.Background(), file, model.LinkArgs{})
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if link == nil || link.URL != "https://example.com/anon-direct" {
		t.Fatalf("expected anon direct link, got %+v", link)
	}
	if anonCalls != 1 {
		t.Fatalf("expected anon resolver once, got %d", anonCalls)
	}
}

func TestPan123ShareLink_TrafficLimitShortCircuits(t *testing.T) {
	// 5112 流量包不足:秒传兜底也未命中时,不回退账号,直接透传真实错误。
	stubThumbDirect(t)
	origAnon := resolveAnonLink
	resolveAnonLink = func(d *Pan123Share, f File, ip string) (*model.Link, error) {
		return nil, err123TrafficLimit
	}
	t.Cleanup(func() { resolveAnonLink = origAnon })

	origRapid := rapidShareTo123
	rapidShareTo123 = func(f File) *model.Link { return nil }
	t.Cleanup(func() { rapidShareTo123 = origRapid })

	d := &Pan123Share{}
	file := File{FileName: "video.mp4"}

	_, err := d.Link(context.Background(), file, model.LinkArgs{})
	if !errors.Is(err, err123TrafficLimit) {
		t.Fatalf("expected err123TrafficLimit, got %v", err)
	}
}

func TestPan123ShareList_AnonymousFirstReturnsAnonList(t *testing.T) {
	// 无需 123Pan 账号:匿名列目录成功即返回,不走鉴权路径(getFilesAuth)。
	orig := resolveAnonList
	anonCalls := 0
	resolveAnonList = func(d *Pan123Share, ctx context.Context, parentId string) ([]File, error) {
		anonCalls++
		return []File{{FileName: "anon.mp4", FileId: 7}}, nil
	}
	t.Cleanup(func() { resolveAnonList = orig })

	d := &Pan123Share{}
	files, err := d.List(context.Background(), File{}, model.ListArgs{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(files) != 1 || files[0].GetName() != "anon.mp4" {
		t.Fatalf("expected anon list, got %+v", files)
	}
	if anonCalls != 1 {
		t.Fatalf("expected anon resolver once, got %d", anonCalls)
	}
}

func TestPan123ShareLink_TrafficLimitRapidFallback(t *testing.T) {
	// 5112 时秒传兜底命中 → 返回秒传直链,不再透传错误。
	stubThumbDirect(t)
	origAnon := resolveAnonLink
	resolveAnonLink = func(d *Pan123Share, f File, ip string) (*model.Link, error) {
		return nil, err123TrafficLimit
	}
	t.Cleanup(func() { resolveAnonLink = origAnon })

	origRapid := rapidShareTo123
	rapidShareTo123 = func(f File) *model.Link { return &model.Link{URL: "https://example.com/rapid-123"} }
	t.Cleanup(func() { rapidShareTo123 = origRapid })

	d := &Pan123Share{}
	file := File{FileName: "video.mp4"}

	link, err := d.Link(context.Background(), file, model.LinkArgs{})
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if link == nil || link.URL != "https://example.com/rapid-123" {
		t.Fatalf("expected rapid fallback link, got %+v", link)
	}
}
