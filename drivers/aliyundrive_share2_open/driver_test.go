package aliyundrive_share2_open

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/cache"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

func TestAliyundriveShare2OpenLink_CachesByFileIDAndAliTo115State(t *testing.T) {
	origCache := aliyundriveShareLinkCache
	origResolver := resolveAliyundriveShareLink
	origAliTo115 := aliyundriveShareAliTo115Enabled
	aliyundriveShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveAliyundriveShareLink = func(ctx context.Context, d *AliyundriveShare2Open, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		resolveCalls++
		return &model.Link{URL: "https://example.com/aliyun/" + aliyundriveShareLinkCacheKey(file.GetID())}, nil
	}
	t.Cleanup(func() {
		aliyundriveShareLinkCache = origCache
		resolveAliyundriveShareLink = origResolver
		aliyundriveShareAliTo115Enabled = origAliTo115
	})

	d := &AliyundriveShare2Open{}
	file := &model.Object{ID: "file-1", Name: "video.mp4"}

	aliyundriveShareAliTo115Enabled = func() bool { return false }
	first, err := d.Link(context.Background(), file, model.LinkArgs{})
	if err != nil {
		t.Fatalf("first link: %v", err)
	}
	second, err := d.Link(context.Background(), file, model.LinkArgs{})
	if err != nil {
		t.Fatalf("second link: %v", err)
	}

	aliyundriveShareAliTo115Enabled = func() bool { return true }
	third, err := d.Link(context.Background(), file, model.LinkArgs{})
	if err != nil {
		t.Fatalf("third link: %v", err)
	}
	fourth, err := d.Link(context.Background(), file, model.LinkArgs{})
	if err != nil {
		t.Fatalf("fourth link: %v", err)
	}

	if first.URL != second.URL {
		t.Fatalf("expected cached false-state URL reuse, got %q and %q", first.URL, second.URL)
	}
	if third.URL != fourth.URL {
		t.Fatalf("expected cached true-state URL reuse, got %q and %q", third.URL, fourth.URL)
	}
	if first.URL == third.URL {
		t.Fatalf("expected different cache buckets for AliTo115 states, got %q", first.URL)
	}
	if resolveCalls != 2 {
		t.Fatalf("expected resolver twice for two setting states, got %d", resolveCalls)
	}
}

func TestAliyundriveShare2OpenLink_DoesNotCacheErrors(t *testing.T) {
	origCache := aliyundriveShareLinkCache
	origResolver := resolveAliyundriveShareLink
	origAliTo115 := aliyundriveShareAliTo115Enabled
	aliyundriveShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveAliyundriveShareLink = func(ctx context.Context, d *AliyundriveShare2Open, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		resolveCalls++
		return nil, errors.New("boom")
	}
	t.Cleanup(func() {
		aliyundriveShareLinkCache = origCache
		resolveAliyundriveShareLink = origResolver
		aliyundriveShareAliTo115Enabled = origAliTo115
	})

	aliyundriveShareAliTo115Enabled = func() bool { return false }
	d := &AliyundriveShare2Open{}
	file := &model.Object{ID: "file-1", Name: "video.mp4"}

	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	if resolveCalls != 2 {
		t.Fatalf("expected resolver twice after errors, got %d", resolveCalls)
	}
}
