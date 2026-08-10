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

// stubMultiSourceDisabled 关闭多账号分片下载并还原,避免 Link() 读 setting 死锁(测试里 op 未初始化)。
func stubMultiSourceDisabled(t *testing.T) {
	t.Helper()
	origMS := multiSourceEnabled
	origCollect := collectMultiAccountLinks
	multiSourceEnabled = func(d *QuarkUCShare) bool { return false }
	collectMultiAccountLinks = func(ctx context.Context, d *QuarkUCShare, file model.Obj, args model.LinkArgs) []*model.Link {
		return nil
	}
	t.Cleanup(func() {
		multiSourceEnabled = origMS
		collectMultiAccountLinks = origCollect
	})
}

func TestQuarkUCShareLink_CachesByFileID(t *testing.T) {
	origCache := quarkUCShareLinkCache
	origResolver := resolveQuarkUCShareLink
	origDirect := resolveShareDirectLink
	origSVIP := accountIsSVIP
	origMS := multiSourceEnabled
	origCollect := collectMultiAccountLinks
	quarkUCShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveQuarkUCShareLink = func(ctx context.Context, d *QuarkUCShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		resolveCalls++
		return &model.Link{URL: "https://example.com/quark/" + file.GetID()}, nil
	}
	accountIsSVIP = func(d *QuarkUCShare) bool { return false } // 非 SVIP:免转存为主
	resolveShareDirectLink = func(d *QuarkUCShare, file model.Obj) (*model.Link, error) {
		return nil, errors.New("share-direct stub disabled") // 置失败,流程落到 resolveQuarkUCShareLink
	}
	multiSourceEnabled = func(d *QuarkUCShare) bool { return false }
	collectMultiAccountLinks = func(ctx context.Context, d *QuarkUCShare, file model.Obj, args model.LinkArgs) []*model.Link {
		return nil
	}
	t.Cleanup(func() {
		quarkUCShareLinkCache = origCache
		resolveQuarkUCShareLink = origResolver
		resolveShareDirectLink = origDirect
		accountIsSVIP = origSVIP
		multiSourceEnabled = origMS
		collectMultiAccountLinks = origCollect
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
	origDirect := resolveShareDirectLink
	origSVIP := accountIsSVIP
	quarkUCShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveQuarkUCShareLink = func(ctx context.Context, d *QuarkUCShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		resolveCalls++
		return nil, errors.New("boom")
	}
	accountIsSVIP = func(d *QuarkUCShare) bool { return false } // 非 SVIP:免转存为主
	resolveShareDirectLink = func(d *QuarkUCShare, file model.Obj) (*model.Link, error) {
		return nil, errors.New("share-direct stub disabled") // 失败 → 落到同样报错的 resolveQuarkUCShareLink
	}
	multiSourceEnabled = func(d *QuarkUCShare) bool { return false }
	collectMultiAccountLinks = func(ctx context.Context, d *QuarkUCShare, file model.Obj, args model.LinkArgs) []*model.Link {
		return nil
	}
	t.Cleanup(func() {
		quarkUCShareLinkCache = origCache
		resolveQuarkUCShareLink = origResolver
		resolveShareDirectLink = origDirect
		accountIsSVIP = origSVIP
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
	stubMultiSourceDisabled(t)
	origCache := quarkUCShareLinkCache
	origResolver := resolveQuarkUCShareLink
	origDirect := resolveShareDirectLink
	origSVIP := accountIsSVIP
	quarkUCShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	resolveQuarkUCShareLink = func(ctx context.Context, d *QuarkUCShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		resolveCalls++
		return &model.Link{URL: "https://example.com/quark/" + file.GetID()}, nil
	}
	accountIsSVIP = func(d *QuarkUCShare) bool { return false } // 非 SVIP:免转存为主
	resolveShareDirectLink = func(d *QuarkUCShare, file model.Obj) (*model.Link, error) {
		return nil, errors.New("share-direct stub disabled") // 置失败,流程落到 resolveQuarkUCShareLink
	}
	t.Cleanup(func() {
		quarkUCShareLinkCache = origCache
		resolveQuarkUCShareLink = origResolver
		resolveShareDirectLink = origDirect
		accountIsSVIP = origSVIP
	})

	d := &QuarkUCShare{Addition: Addition{ShareToken: "share-token"}, config: driver.Config{Name: "QuarkShare"}}

	_, _ = d.Link(context.Background(), &model.Object{ID: "file-1", Name: "a.mp4"}, model.LinkArgs{})
	_, _ = d.Link(context.Background(), &model.Object{ID: "file-2", Name: "b.mp4"}, model.LinkArgs{})
	if resolveCalls != 2 {
		t.Fatalf("expected resolver twice for different file IDs, got %d", resolveCalls)
	}
}

func TestQuarkUCShareLink_FallbackToShareDirectOnSaveFail(t *testing.T) {
	// 转存(save+speedup)为主;转存失败时回退免转存(share-direct)。
	stubMultiSourceDisabled(t)
	origCache := quarkUCShareLinkCache
	origResolver := resolveQuarkUCShareLink
	origDirect := resolveShareDirectLink
	quarkUCShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	directCalls := 0
	resolveQuarkUCShareLink = func(ctx context.Context, d *QuarkUCShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		resolveCalls++
		return nil, errors.New("save failed")
	}
	resolveShareDirectLink = func(d *QuarkUCShare, file model.Obj) (*model.Link, error) {
		directCalls++
		return &model.Link{URL: "https://example.com/share-direct/" + file.GetID()}, nil
	}
	t.Cleanup(func() {
		quarkUCShareLinkCache = origCache
		resolveQuarkUCShareLink = origResolver
		resolveShareDirectLink = origDirect
	})

	d := &QuarkUCShare{Addition: Addition{ShareToken: "share-token"}, config: driver.Config{Name: "QuarkShare"}}
	file := &model.Object{ID: "fid-fidtoken-pid", Name: "video.mp4"}

	link, err := d.Link(context.Background(), file, model.LinkArgs{})
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if link == nil || link.URL == "" {
		t.Fatalf("expected non-empty link")
	}
	if resolveCalls != 1 {
		t.Fatalf("expected save attempted once, got %d", resolveCalls)
	}
	if directCalls != 1 {
		t.Fatalf("expected share-direct fallback once, got %d", directCalls)
	}
}

func TestQuarkUCShareLink_PrefersSaveAndSpeedup(t *testing.T) {
	// 转存(save+speedup)为主:成功时不调用免转存(免转存无 speedup,被限速,仅兜底)。
	stubMultiSourceDisabled(t)
	origCache := quarkUCShareLinkCache
	origResolver := resolveQuarkUCShareLink
	origDirect := resolveShareDirectLink
	quarkUCShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	directCalls := 0
	resolveQuarkUCShareLink = func(ctx context.Context, d *QuarkUCShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		resolveCalls++
		return &model.Link{URL: "https://example.com/saved/" + file.GetID()}, nil
	}
	resolveShareDirectLink = func(d *QuarkUCShare, file model.Obj) (*model.Link, error) {
		directCalls++
		return nil, errors.New("share-direct should not be called")
	}
	t.Cleanup(func() {
		quarkUCShareLinkCache = origCache
		resolveQuarkUCShareLink = origResolver
		resolveShareDirectLink = origDirect
	})

	d := &QuarkUCShare{Addition: Addition{ShareToken: "share-token"}, config: driver.Config{Name: "QuarkShare"}}
	file := &model.Object{ID: "fid-fidtoken-pid", Name: "big.iso"}

	link, err := d.Link(context.Background(), file, model.LinkArgs{})
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if link == nil || link.URL == "" {
		t.Fatalf("expected non-empty link")
	}
	if resolveCalls != 1 {
		t.Fatalf("expected save+speedup once, got %d", resolveCalls)
	}
	if directCalls != 0 {
		t.Fatalf("expected share-direct not called when save+speedup succeeds, got %d", directCalls)
	}
}

// 开关开启、collectMultiAccountLinks 返回多条链时,Link() 跳过串行主链,直接用首条为 primary、全部填 MultiSource。
func TestQuarkUCShareLink_PopulatesMultiSourceWhenEnabled(t *testing.T) {
	stubMultiSourceDisabled(t)
	origCache := quarkUCShareLinkCache
	origResolver := resolveQuarkUCShareLink
	origDirect := resolveShareDirectLink
	quarkUCShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	// 开关开时 Link() 不再调 resolveQuarkUCShareLink(直接并发),这里设个哨兵确保不被调用。
	resolveQuarkUCShareLink = func(ctx context.Context, d *QuarkUCShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		t.Fatalf("resolveQuarkUCShareLink should not be called when multi-source enabled")
		return nil, nil
	}
	resolveShareDirectLink = func(d *QuarkUCShare, file model.Obj) (*model.Link, error) {
		return nil, errors.New("share-direct stub disabled")
	}
	// 开启多账号分片:collectMultiAccountLinks 返回 3 条链(首条为 primary)。
	multiSourceEnabled = func(d *QuarkUCShare) bool { return true }
	collectMultiAccountLinks = func(ctx context.Context, d *QuarkUCShare, file model.Obj, args model.LinkArgs) []*model.Link {
		return []*model.Link{
			{URL: "https://cdn.example.com/account1/fid", Concurrency: 4, PartSize: 512 << 10},
			{URL: "https://cdn.example.com/account2/fid", Concurrency: 4, PartSize: 512 << 10},
			{URL: "https://cdn.example.com/account3/fid", Concurrency: 4, PartSize: 512 << 10},
		}
	}
	t.Cleanup(func() {
		quarkUCShareLinkCache = origCache
		resolveQuarkUCShareLink = origResolver
		resolveShareDirectLink = origDirect
	})

	d := &QuarkUCShare{Addition: Addition{ShareToken: "share-token"}, config: driver.Config{Name: "QuarkShare"}}
	file := &model.Object{ID: "fid-fidtoken-pid", Name: "big.mp4"}

	link, err := d.Link(context.Background(), file, model.LinkArgs{})
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if link == nil {
		t.Fatal("nil link")
	}
	if link.URL != "https://cdn.example.com/account1/fid" {
		t.Fatalf("expected primary=account1, got %s", link.URL)
	}
	if len(link.MultiSource) != 3 {
		t.Fatalf("expected 3 MultiSource, got %d", len(link.MultiSource))
	}
	// Concurrency 不在 Link 放大(multiSourceRangeReader 内部按 N 放大且带上限),保持 primary 原值。
	if link.Concurrency != 4 {
		t.Fatalf("expected Concurrency unchanged=4, got %d", link.Concurrency)
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
