package _123Share

import (
	"context"
	"errors"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

// File 实现了 model.Obj,可直接作为 Link 入参。

func TestPan123ShareLink_AnonymousFirstReturnsAnonLink(t *testing.T) {
	// 无需 123Pan 账号:匿名成功即返回,不走账号路径。
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
	// 5112 流量包不足:不回退账号,直接透传真实错误。
	origAnon := resolveAnonLink
	resolveAnonLink = func(d *Pan123Share, f File, ip string) (*model.Link, error) {
		return nil, err123TrafficLimit
	}
	t.Cleanup(func() { resolveAnonLink = origAnon })

	d := &Pan123Share{}
	file := File{FileName: "video.mp4"}

	_, err := d.Link(context.Background(), file, model.LinkArgs{})
	if !errors.Is(err, err123TrafficLimit) {
		t.Fatalf("expected err123TrafficLimit, got %v", err)
	}
}
