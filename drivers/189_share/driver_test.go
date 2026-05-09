package _189_share

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/cache"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

func TestCloud189ShareLink_CachesByFileID(t *testing.T) {
	origCache := cloud189ShareLinkCache
	origResolver := resolveCloud189ShareLink
	cloud189ShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveCloud189ShareLink = func(ctx context.Context, d *Cloud189Share, file model.Obj) (*model.Link, error) {
		resolveCalls++
		return &model.Link{URL: "https://example.com/189/" + file.GetID()}, nil
	}
	t.Cleanup(func() {
		cloud189ShareLinkCache = origCache
		resolveCloud189ShareLink = origResolver
	})

	d := &Cloud189Share{}
	file := &FileObj{ObjThumb: model.ObjThumb{Object: model.Object{ID: "file-1", Name: "video.mp4"}}}

	first, err := d.Link(context.Background(), file, model.LinkArgs{})
	if err != nil {
		t.Fatalf("first link: %v", err)
	}
	second, err := d.Link(context.Background(), file, model.LinkArgs{Type: "ignored"})
	if err != nil {
		t.Fatalf("second link: %v", err)
	}
	if first.URL != second.URL {
		t.Fatalf("expected cached URL reuse, got %q and %q", first.URL, second.URL)
	}
	if resolveCalls != 1 {
		t.Fatalf("expected resolver once, got %d", resolveCalls)
	}
}

func TestCloud189ShareLink_UsesDifferentKeysForDifferentFiles(t *testing.T) {
	origCache := cloud189ShareLinkCache
	origResolver := resolveCloud189ShareLink
	cloud189ShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveCloud189ShareLink = func(ctx context.Context, d *Cloud189Share, file model.Obj) (*model.Link, error) {
		resolveCalls++
		return &model.Link{URL: "https://example.com/189/" + file.GetID()}, nil
	}
	t.Cleanup(func() {
		cloud189ShareLinkCache = origCache
		resolveCloud189ShareLink = origResolver
	})

	d := &Cloud189Share{}
	file1 := &FileObj{ObjThumb: model.ObjThumb{Object: model.Object{ID: "file-1", Name: "a.mp4"}}}
	file2 := &FileObj{ObjThumb: model.ObjThumb{Object: model.Object{ID: "file-2", Name: "b.mp4"}}}

	_, _ = d.Link(context.Background(), file1, model.LinkArgs{})
	_, _ = d.Link(context.Background(), file2, model.LinkArgs{})
	if resolveCalls != 2 {
		t.Fatalf("expected resolver twice for different file IDs, got %d", resolveCalls)
	}
}

func TestCloud189ShareLink_DoesNotCacheErrors(t *testing.T) {
	origCache := cloud189ShareLinkCache
	origResolver := resolveCloud189ShareLink
	cloud189ShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveCloud189ShareLink = func(ctx context.Context, d *Cloud189Share, file model.Obj) (*model.Link, error) {
		resolveCalls++
		return nil, errors.New("boom")
	}
	t.Cleanup(func() {
		cloud189ShareLinkCache = origCache
		resolveCloud189ShareLink = origResolver
	})

	d := &Cloud189Share{}
	file := &FileObj{ObjThumb: model.ObjThumb{Object: model.Object{ID: "file-1", Name: "video.mp4"}}}

	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	if resolveCalls != 2 {
		t.Fatalf("expected resolver twice after errors, got %d", resolveCalls)
	}
}
