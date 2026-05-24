package quark_uc_share

import (
	"context"
	"errors"
	"testing"
	"time"

	quark "github.com/OpenListTeam/OpenList/v4/drivers/quark_uc"
	"github.com/OpenListTeam/OpenList/v4/drivers/quark_uc_tv"
	"github.com/OpenListTeam/OpenList/v4/internal/cache"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

func TestQuarkUCShareLink_CachesByFileID(t *testing.T) {
	origCache := quarkUCShareLinkCache
	origResolver := resolveQuarkUCShareLink
	quarkUCShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveQuarkUCShareLink = func(ctx context.Context, d *QuarkUCShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		resolveCalls++
		return &model.Link{URL: "https://example.com/quark/" + file.GetID()}, nil
	}
	t.Cleanup(func() {
		quarkUCShareLinkCache = origCache
		resolveQuarkUCShareLink = origResolver
	})

	d := &QuarkUCShare{Addition: Addition{ShareToken: "share-token"}, config: driver.Config{Name: "QuarkShare"}}
	file := &model.Object{ID: "file-1", Name: "video.mp4"}

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

func TestQuarkUCShareLink_DoesNotCacheErrors(t *testing.T) {
	origCache := quarkUCShareLinkCache
	origResolver := resolveQuarkUCShareLink
	quarkUCShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveQuarkUCShareLink = func(ctx context.Context, d *QuarkUCShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		resolveCalls++
		return nil, errors.New("boom")
	}
	t.Cleanup(func() {
		quarkUCShareLinkCache = origCache
		resolveQuarkUCShareLink = origResolver
	})

	d := &QuarkUCShare{Addition: Addition{ShareToken: "share-token"}, config: driver.Config{Name: "QuarkShare"}}
	file := &model.Object{ID: "file-1", Name: "video.mp4"}

	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	if resolveCalls != 2 {
		t.Fatalf("expected resolver twice after errors, got %d", resolveCalls)
	}
}

func TestQuarkUCShareLink_DifferentFileIDsDoNotShareCache(t *testing.T) {
	origCache := quarkUCShareLinkCache
	origResolver := resolveQuarkUCShareLink
	quarkUCShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveQuarkUCShareLink = func(ctx context.Context, d *QuarkUCShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		resolveCalls++
		return &model.Link{URL: "https://example.com/quark/" + file.GetID()}, nil
	}
	t.Cleanup(func() {
		quarkUCShareLinkCache = origCache
		resolveQuarkUCShareLink = origResolver
	})

	d := &QuarkUCShare{Addition: Addition{ShareToken: "share-token"}, config: driver.Config{Name: "QuarkShare"}}

	_, _ = d.Link(context.Background(), &model.Object{ID: "file-1", Name: "a.mp4"}, model.LinkArgs{})
	_, _ = d.Link(context.Background(), &model.Object{ID: "file-2", Name: "b.mp4"}, model.LinkArgs{})
	if resolveCalls != 2 {
		t.Fatalf("expected resolver twice for different file IDs, got %d", resolveCalls)
	}
}

func TestBindRequestDriverUsesSelectedStorageForRequestAndTempDir(t *testing.T) {
	selected := &quark.QuarkOrUC{TempDirId: "temp-dir-a"}
	other := &quark.QuarkOrUC{TempDirId: "temp-dir-b"}

	binding := bindRequestDriver(selected)
	if binding.requestDriver != selected {
		t.Fatalf("expected request driver to stay bound to selected storage")
	}
	if binding.tempDirID() != "temp-dir-a" {
		t.Fatalf("expected temp dir from selected storage, got %q", binding.tempDirID())
	}
	if binding.matches(other) {
		t.Fatalf("expected binding to reject a different storage instance")
	}
}

func TestBindRequestDriverUsesSelectedTVStorageForRequestAndTempDir(t *testing.T) {
	selected := &quark_uc_tv.QuarkUCTV{TempDirId: "temp-dir-tv-a"}
	other := &requestTVBinding{tempDirId: "temp-dir-tv-b"}

	binding := bindTVRequestDriver(selected)
	if binding.tempDirID() != "temp-dir-tv-a" {
		t.Fatalf("expected tv temp dir from selected storage, got %q", binding.tempDirID())
	}
	if binding.matches(other) {
		t.Fatalf("expected tv binding to reject a different storage instance")
	}
}
