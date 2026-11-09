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
	origAliTo123 := aliyundriveShareAliTo123Enabled
	origFallback := aliyundriveShareAliTo115FallbackEnabled
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
		aliyundriveShareAliTo123Enabled = origAliTo123
		aliyundriveShareAliTo115FallbackEnabled = origFallback
	})

	d := &AliyundriveShare2Open{}
	file := &model.Object{ID: "file-1", Name: "video.mp4"}

	aliyundriveShareAliTo123Enabled = func() bool { return false }
	aliyundriveShareAliTo115Enabled = func() bool { return false }
	aliyundriveShareAliTo115FallbackEnabled = func() bool { return true }
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
	origAliTo123 := aliyundriveShareAliTo123Enabled
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
		aliyundriveShareAliTo123Enabled = origAliTo123
	})

	aliyundriveShareAliTo123Enabled = func() bool { return false }
	aliyundriveShareAliTo115Enabled = func() bool { return false }
	d := &AliyundriveShare2Open{}
	file := &model.Object{ID: "file-1", Name: "video.mp4"}

	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	_, _ = d.Link(context.Background(), file, model.LinkArgs{})
	if resolveCalls != 2 {
		t.Fatalf("expected resolver twice after errors, got %d", resolveCalls)
	}
}

func TestFallbackToAliLink_EnabledReturnsAliLink(t *testing.T) {
	origFallback := aliyundriveShareAliTo115FallbackEnabled
	aliyundriveShareAliTo115FallbackEnabled = func() bool { return true }
	t.Cleanup(func() { aliyundriveShareAliTo115FallbackEnabled = origFallback })

	aliLink := &model.Link{URL: "https://example.com/aliyun"}
	link, err := fallbackToAliLink(aliLink, errors.New("获取115链接超时"))
	if err != nil {
		t.Fatalf("expected fallback to aliyun link, got error: %v", err)
	}
	if link != aliLink {
		t.Fatalf("expected aliyun link returned, got %v", link)
	}

	// 无115账号且回退开启:保持旧行为返回阿里直链
	link, err = fallbackToAliLink(aliLink, nil)
	if err != nil {
		t.Fatalf("expected fallback with no 115 driver, got error: %v", err)
	}
	if link != aliLink {
		t.Fatalf("expected aliyun link returned, got %v", link)
	}
}

func TestFallbackToAliLink_DisabledReturnsError(t *testing.T) {
	origFallback := aliyundriveShareAliTo115FallbackEnabled
	aliyundriveShareAliTo115FallbackEnabled = func() bool { return false }
	t.Cleanup(func() { aliyundriveShareAliTo115FallbackEnabled = origFallback })

	aliLink := &model.Link{URL: "https://example.com/aliyun"}

	link, err := fallbackToAliLink(aliLink, errors.New("获取115链接超时"))
	if err == nil {
		t.Fatal("expected error when fallback disabled, got nil")
	}
	if link != nil {
		t.Fatalf("expected nil link when fallback disabled, got %v", link)
	}
	if err.Error() != "获取115链接超时" {
		t.Fatalf("expected original error preserved, got %q", err.Error())
	}

	link, err = fallbackToAliLink(aliLink, nil)
	if err == nil || link != nil {
		t.Fatalf("expected error for missing 115 drivers, got link=%v err=%v", link, err)
	}
	if err.Error() != "没有可用的115账号" {
		t.Fatalf("expected missing-driver error, got %q", err.Error())
	}
}

func TestAliyundriveShareLinkCacheKey_FallbackOnlyMattersWhenAliTo115Enabled(t *testing.T) {
	orig115 := aliyundriveShareAliTo115Enabled
	orig123 := aliyundriveShareAliTo123Enabled
	origFallback := aliyundriveShareAliTo115FallbackEnabled
	t.Cleanup(func() {
		aliyundriveShareAliTo115Enabled = orig115
		aliyundriveShareAliTo123Enabled = orig123
		aliyundriveShareAliTo115FallbackEnabled = origFallback
	})
	aliyundriveShareAliTo123Enabled = func() bool { return false }

	// AliTo115关闭:回退开关不影响key(与旧行为零差异)
	aliyundriveShareAliTo115Enabled = func() bool { return false }
	aliyundriveShareAliTo115FallbackEnabled = func() bool { return true }
	disabledKey := aliyundriveShareLinkCacheKey("file-1")
	aliyundriveShareAliTo115FallbackEnabled = func() bool { return false }
	if key := aliyundriveShareLinkCacheKey("file-1"); key != disabledKey {
		t.Fatalf("expected fallback ignored when AliTo115 disabled, got %q vs %q", key, disabledKey)
	}

	// AliTo115开启:切换回退开关产生不同缓存桶,旧缓存(含回退的阿里直链)立即失效
	aliyundriveShareAliTo115Enabled = func() bool { return true }
	aliyundriveShareAliTo115FallbackEnabled = func() bool { return true }
	fallbackOnKey := aliyundriveShareLinkCacheKey("file-1")
	aliyundriveShareAliTo115FallbackEnabled = func() bool { return false }
	if key := aliyundriveShareLinkCacheKey("file-1"); key == fallbackOnKey {
		t.Fatalf("expected different cache buckets for fallback states, got %q", key)
	}
}

func TestAliyundriveShare2OpenLink_HonorsLinkExpiration(t *testing.T) {
	origCache := aliyundriveShareLinkCache
	origResolver := resolveAliyundriveShareLink
	origAliTo115 := aliyundriveShareAliTo115Enabled
	origAliTo123 := aliyundriveShareAliTo123Enabled
	origFallback := aliyundriveShareAliTo115FallbackEnabled
	aliyundriveShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveCalls := 0
	exp := 100 * time.Millisecond
	resolveAliyundriveShareLink = func(ctx context.Context, d *AliyundriveShare2Open, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		resolveCalls++
		if file.GetID() == "expired" {
			return &model.Link{URL: "https://example.com/oss", Expiration: &exp}, nil
		}
		return &model.Link{URL: "https://example.com/115"}, nil
	}
	t.Cleanup(func() {
		aliyundriveShareLinkCache = origCache
		resolveAliyundriveShareLink = origResolver
		aliyundriveShareAliTo115Enabled = origAliTo115
		aliyundriveShareAliTo123Enabled = origAliTo123
		aliyundriveShareAliTo115FallbackEnabled = origFallback
	})

	aliyundriveShareAliTo123Enabled = func() bool { return false }
	aliyundriveShareAliTo115Enabled = func() bool { return false }
	aliyundriveShareAliTo115FallbackEnabled = func() bool { return true }
	d := &AliyundriveShare2Open{}

	ossFile := &model.Object{ID: "expired", Name: "README.md"}
	first, err := d.Link(context.Background(), ossFile, model.LinkArgs{})
	if err != nil {
		t.Fatalf("first oss link: %v", err)
	}
	second, err := d.Link(context.Background(), ossFile, model.LinkArgs{})
	if err != nil {
		t.Fatalf("second oss link: %v", err)
	}
	if first.URL != second.URL {
		t.Fatalf("expected cached oss link reuse, got %q and %q", first.URL, second.URL)
	}
	time.Sleep(200 * time.Millisecond)
	if _, err = d.Link(context.Background(), ossFile, model.LinkArgs{}); err != nil {
		t.Fatalf("third oss link: %v", err)
	}
	if resolveCalls != 2 {
		t.Fatalf("expired oss link should be re-resolved after link expiration, resolve calls=%d", resolveCalls)
	}

	// 无 Expiration 的链接(115/123 转存链)不因此提前淘汰。
	panFile := &model.Object{ID: "pan", Name: "video.mp4"}
	if _, err = d.Link(context.Background(), panFile, model.LinkArgs{}); err != nil {
		t.Fatalf("pan link: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if _, err = d.Link(context.Background(), panFile, model.LinkArgs{}); err != nil {
		t.Fatalf("pan link reuse: %v", err)
	}
	if resolveCalls != 3 {
		t.Fatalf("link without expiration should stay cached, resolve calls=%d", resolveCalls)
	}
}
