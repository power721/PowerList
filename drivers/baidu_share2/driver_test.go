package baidu_share

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/cache"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

// stubResolvers 替换两条取链路径为可控桩函数,隔离 op/storage,返回还原函数。
func stubResolvers(direct func(d *BaiduShare2, file model.Obj) (*model.Link, error),
	transfer func(ctx context.Context, d *BaiduShare2, file model.Obj, args model.LinkArgs) (*model.Link, error)) func() {
	origCache := baiduShareLinkCache
	origDirect := resolveShareDirectLink
	origTransfer := resolveBaiduShareLink
	baiduShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveShareDirectLink = direct
	resolveBaiduShareLink = transfer
	return func() {
		baiduShareLinkCache = origCache
		resolveShareDirectLink = origDirect
		resolveBaiduShareLink = origTransfer
	}
}

// baiduDlnaSekey 必须把原始 randsk 与已编码(BDCLND)形态都归一化为单编码值,
// 使 resty 再编码后服务端拿到一致的「编码 sekey」(对已编码值幂等,不破坏无提取码分享)。
// 关键回归:字面 '+' 必须编码为 %2B,绝不能被当作空格(若用 QueryUnescape 归一化会踩此坑)。
func TestBaiduDlnaSekey_Normalizes(t *testing.T) {
	raw := "Fk2Ab+Z9=="
	encoded := "Fk2Ab%2BZ9%3D%3D"
	want := encoded
	if got := baiduDlnaSekey(raw); got != want {
		t.Errorf("from raw randsk: got %q want %q", got, want)
	}
	if got := baiduDlnaSekey(encoded); got != want {
		t.Errorf("from encoded BDCLND (must be idempotent): got %q want %q", got, want)
	}
	if strings.Contains(baiduDlnaSekey(raw), " ") || !strings.Contains(baiduDlnaSekey(raw), "%2B") {
		t.Errorf("literal '+' must encode to %%2B, not space: got %q", baiduDlnaSekey(raw))
	}
}

func TestBaiduShare2Link_CachesByFileID(t *testing.T) {
	directCalls, transferCalls := 0, 0
	restore := stubResolvers(
		func(d *BaiduShare2, file model.Obj) (*model.Link, error) {
			directCalls++
			return &model.Link{URL: "https://example.com/baidu/" + file.GetID()}, nil
		},
		func(ctx context.Context, d *BaiduShare2, file model.Obj, args model.LinkArgs) (*model.Link, error) {
			transferCalls++
			return &model.Link{URL: "https://transfer/" + file.GetID()}, nil
		},
	)
	defer restore()

	d := &BaiduShare2{}
	file := &model.Object{ID: "file-1", Name: "video.mp4"}

	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	_, _ = d.Link(context.Background(), file, model.LinkArgs{Type: "ignored"})
	if directCalls != 1 {
		t.Fatalf("expected resolver once, got %d", directCalls)
	}
	if transferCalls != 0 {
		t.Fatalf("免转存命中不应回退转存, got %d transfer calls", transferCalls)
	}
}

func TestBaiduShare2Link_DoesNotCacheNilOrError(t *testing.T) {
	directCalls := 0
	restore := stubResolvers(
		func(d *BaiduShare2, file model.Obj) (*model.Link, error) {
			directCalls++
			if directCalls == 1 {
				return nil, nil // nil → 不缓存
			}
			return nil, errors.New("boom") // error → 不缓存
		},
		func(ctx context.Context, d *BaiduShare2, file model.Obj, args model.LinkArgs) (*model.Link, error) {
			return nil, nil // 兜底也失败,结果不被缓存
		},
	)
	defer restore()

	d := &BaiduShare2{}
	file := &model.Object{ID: "file-1", Name: "video.mp4"}

	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	if directCalls != 2 {
		t.Fatalf("expected resolver twice after nil/error results, got %d", directCalls)
	}
}

func TestBaiduShare2Link_DifferentFileIDsDoNotShareCache(t *testing.T) {
	directCalls := 0
	restore := stubResolvers(
		func(d *BaiduShare2, file model.Obj) (*model.Link, error) {
			directCalls++
			return &model.Link{URL: "https://example.com/baidu/" + file.GetID()}, nil
		},
		func(ctx context.Context, d *BaiduShare2, file model.Obj, args model.LinkArgs) (*model.Link, error) {
			return &model.Link{URL: "https://transfer/" + file.GetID()}, nil
		},
	)
	defer restore()

	d := &BaiduShare2{}

	_, _ = d.Link(context.Background(), &model.Object{ID: "file-1", Name: "a.mp4"}, model.LinkArgs{})
	_, _ = d.Link(context.Background(), &model.Object{ID: "file-2", Name: "b.mp4"}, model.LinkArgs{})
	if directCalls != 2 {
		t.Fatalf("expected resolver twice for different file IDs, got %d", directCalls)
	}
}

// 免转存命中 → 不应回退到转存。
func TestBaiduShare2Link_ShareDirectPrimarySkipsTransfer(t *testing.T) {
	transferCalls := 0
	restore := stubResolvers(
		func(d *BaiduShare2, file model.Obj) (*model.Link, error) {
			return &model.Link{URL: "https://d.pcs.baidu.com/dlna/" + file.GetID()}, nil
		},
		func(ctx context.Context, d *BaiduShare2, file model.Obj, args model.LinkArgs) (*model.Link, error) {
			transferCalls++
			return &model.Link{URL: "https://transfer/" + file.GetID()}, nil
		},
	)
	defer restore()

	d := &BaiduShare2{}
	file := &model.Object{ID: "file-1", Name: "v.mp4"}
	link, err := d.Link(context.Background(), file, model.LinkArgs{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if transferCalls != 0 {
		t.Fatalf("免转存命中不应回退转存, got %d transfer calls", transferCalls)
	}
	if !strings.HasPrefix(link.URL, "https://d.pcs.baidu.com/dlna/") {
		t.Fatalf("expected 免转存 link, got %s", link.URL)
	}
}

// 免转存失败 → 应回退到转存一次。
func TestBaiduShare2Link_ShareDirectFailFallsBackToTransfer(t *testing.T) {
	transferCalls := 0
	restore := stubResolvers(
		func(d *BaiduShare2, file model.Obj) (*model.Link, error) {
			return nil, errors.New("share-direct disabled")
		},
		func(ctx context.Context, d *BaiduShare2, file model.Obj, args model.LinkArgs) (*model.Link, error) {
			transferCalls++
			return &model.Link{URL: "https://transfer/" + file.GetID()}, nil
		},
	)
	defer restore()

	d := &BaiduShare2{}
	file := &model.Object{ID: "file-1", Name: "v.mp4"}
	link, err := d.Link(context.Background(), file, model.LinkArgs{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if transferCalls != 1 {
		t.Fatalf("免转存失败应回退转存一次, got %d", transferCalls)
	}
	if !strings.HasPrefix(link.URL, "https://transfer/") {
		t.Fatalf("expected 转存 link, got %s", link.URL)
	}
}
