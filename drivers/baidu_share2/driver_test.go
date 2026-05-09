package baidu_share

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/cache"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

func TestBaiduShare2Link_CachesByFileID(t *testing.T) {
	origCache := baiduShareLinkCache
	origResolver := resolveBaiduShareLink
	baiduShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveBaiduShareLink = func(ctx context.Context, d *BaiduShare2, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		resolveCalls++
		return &model.Link{URL: "https://example.com/baidu/" + file.GetID()}, nil
	}
	t.Cleanup(func() {
		baiduShareLinkCache = origCache
		resolveBaiduShareLink = origResolver
	})

	d := &BaiduShare2{}
	file := &model.Object{ID: "file-1", Name: "video.mp4"}

	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	_, _ = d.Link(context.Background(), file, model.LinkArgs{Type: "ignored"})
	if resolveCalls != 1 {
		t.Fatalf("expected resolver once, got %d", resolveCalls)
	}
}

func TestBaiduShare2Link_DoesNotCacheNilOrError(t *testing.T) {
	origCache := baiduShareLinkCache
	origResolver := resolveBaiduShareLink
	baiduShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveBaiduShareLink = func(ctx context.Context, d *BaiduShare2, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		resolveCalls++
		if resolveCalls == 1 {
			return nil, nil
		}
		return nil, errors.New("boom")
	}
	t.Cleanup(func() {
		baiduShareLinkCache = origCache
		resolveBaiduShareLink = origResolver
	})

	d := &BaiduShare2{}
	file := &model.Object{ID: "file-1", Name: "video.mp4"}

	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	if resolveCalls != 2 {
		t.Fatalf("expected resolver twice after nil/error results, got %d", resolveCalls)
	}
}

func TestBaiduShare2Link_DifferentFileIDsDoNotShareCache(t *testing.T) {
	origCache := baiduShareLinkCache
	origResolver := resolveBaiduShareLink
	baiduShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveBaiduShareLink = func(ctx context.Context, d *BaiduShare2, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		resolveCalls++
		return &model.Link{URL: "https://example.com/baidu/" + file.GetID()}, nil
	}
	t.Cleanup(func() {
		baiduShareLinkCache = origCache
		resolveBaiduShareLink = origResolver
	})

	d := &BaiduShare2{}

	_, _ = d.Link(context.Background(), &model.Object{ID: "file-1", Name: "a.mp4"}, model.LinkArgs{})
	_, _ = d.Link(context.Background(), &model.Object{ID: "file-2", Name: "b.mp4"}, model.LinkArgs{})
	if resolveCalls != 2 {
		t.Fatalf("expected resolver twice for different file IDs, got %d", resolveCalls)
	}
}
