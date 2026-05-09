package thunder_share

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/cache"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

func TestThunderShareLink_CachesByFileID(t *testing.T) {
	origCache := thunderShareLinkCache
	origResolver := resolveThunderShareLink
	thunderShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveThunderShareLink = func(ctx context.Context, d *ThunderShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		resolveCalls++
		return &model.Link{URL: "https://example.com/thunder/" + file.GetID()}, nil
	}
	t.Cleanup(func() {
		thunderShareLinkCache = origCache
		resolveThunderShareLink = origResolver
	})

	d := &ThunderShare{}
	file := &model.Object{ID: "file-1", Name: "video.mp4"}

	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	if resolveCalls != 1 {
		t.Fatalf("expected resolver once, got %d", resolveCalls)
	}
}

func TestThunderShareLink_DifferentFileIDsDoNotShareCache(t *testing.T) {
	origCache := thunderShareLinkCache
	origResolver := resolveThunderShareLink
	thunderShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveThunderShareLink = func(ctx context.Context, d *ThunderShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		resolveCalls++
		return &model.Link{URL: "https://example.com/thunder/" + file.GetID()}, nil
	}
	t.Cleanup(func() {
		thunderShareLinkCache = origCache
		resolveThunderShareLink = origResolver
	})

	d := &ThunderShare{}
	_, _ = d.Link(context.Background(), &model.Object{ID: "file-1", Name: "a.mp4"}, model.LinkArgs{})
	_, _ = d.Link(context.Background(), &model.Object{ID: "file-2", Name: "b.mp4"}, model.LinkArgs{})
	if resolveCalls != 2 {
		t.Fatalf("expected resolver twice for different file IDs, got %d", resolveCalls)
	}
}

func TestThunderShareLink_DoesNotCacheErrors(t *testing.T) {
	origCache := thunderShareLinkCache
	origResolver := resolveThunderShareLink
	thunderShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveThunderShareLink = func(ctx context.Context, d *ThunderShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		resolveCalls++
		return nil, errors.New("boom")
	}
	t.Cleanup(func() {
		thunderShareLinkCache = origCache
		resolveThunderShareLink = origResolver
	})

	d := &ThunderShare{}
	file := &model.Object{ID: "file-1", Name: "video.mp4"}

	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	if resolveCalls != 2 {
		t.Fatalf("expected resolver twice after errors, got %d", resolveCalls)
	}
}
