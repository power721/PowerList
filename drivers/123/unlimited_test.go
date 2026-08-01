package _123

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// freshThumbUrl 造一条 t= 还没过期的缩略图形态签名直链。
func freshThumbUrl(expire time.Time) string {
	return fmt.Sprintf("https://download-cdn.cjjd19.com/123-4/70c9353a/1830358103-0/"+
		"70c9353af59057bbb0e486fb7e9f9004/c-m9011_24_24?v=5&t=%d&r=L2ZNZG&bzc=1"+
		"&bzs=313832333435383439323a323a303a30&s=%d4e19134a8053510398359f03fe4728b7"+
		"&bi=3918592672&filename=x.mkv&cache_type=1&w=24&h=24&trade_key=123pan-thumbnail&type=video",
		expire.Unix(), expire.Unix())
}

// TestStripThumbTransform 逐例对齐 my.jar FishCrypto._pan123UnlimitedUrl 的 unidbg 实测输出。
func TestStripThumbTransform(t *testing.T) {
	const base = "https://cdn.123295.com/123-159/5ea1c7ee/1814511190-0/etag/"
	cases := []struct {
		name    string
		in      string
		want    string
		changed bool
	}{
		{
			name:    "video thumbnail: strip suffix and trade_key",
			in:      base + "c-m9013_24_24?v=5&t=1&w=24&h=24&trade_key=123pan-thumbnail&type=video",
			want:    base + "c-m9013?v=5&t=1&w=24&h=24&type=video",
			changed: true,
		},
		{
			name:    "other transform size is kept",
			in:      base + "c-m9013_70_70?v=5&t=1&w=70&h=70&trade_key=123pan-thumbnail&type=video",
			want:    base + "c-m9013_70_70?v=5&t=1&w=70&h=70&type=video",
			changed: true,
		},
		{
			name:    "suffix stripped even without trade_key",
			in:      base + "c-m9013_24_24?v=5&t=1&w=24&h=24&type=video",
			want:    base + "c-m9013?v=5&t=1&w=24&h=24&type=video",
			changed: true,
		},
		{
			name:    "trade_key removed even without suffix",
			in:      base + "c-m9013?v=5&t=1&w=24&h=24&trade_key=123pan-thumbnail&type=video",
			want:    base + "c-m9013?v=5&t=1&w=24&h=24&type=video",
			changed: true,
		},
		{
			name:    "other trade_key value is kept",
			in:      base + "c-m9013_24_24?v=5&t=1&trade_key=something-else&type=video",
			want:    base + "c-m9013?v=5&t=1&trade_key=something-else&type=video",
			changed: true,
		},
		{
			name:    "trade_key as first query param is kept (no leading &)",
			in:      base + "c-m9013_24_24?trade_key=123pan-thumbnail&v=5&t=1",
			want:    base + "c-m9013?trade_key=123pan-thumbnail&v=5&t=1",
			changed: true,
		},
		{
			name:    "plain object url untouched",
			in:      base + "c-m8002?v=5&t=1&filename=fanart.jpg&cache_type=1",
			want:    base + "c-m8002?v=5&t=1&filename=fanart.jpg&cache_type=1",
			changed: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, changed := StripThumbTransform(c.in)
			if got != c.want {
				t.Fatalf("url:\n got %s\nwant %s", got, c.want)
			}
			if changed != c.changed {
				t.Fatalf("changed = %v, want %v", changed, c.changed)
			}
		})
	}
}

func TestThumbLinkTTL(t *testing.T) {
	if _, ok := ThumbLinkTTL("https://cdn/x?v=5"); ok {
		t.Fatal("expected no ttl without t=")
	}
	ttl, ok := ThumbLinkTTL(freshThumbUrl(time.Now().Add(7 * 24 * time.Hour)))
	if !ok {
		t.Fatal("expected ttl to parse")
	}
	if ttl < 6*24*time.Hour {
		t.Fatalf("ttl too small: %v", ttl)
	}
	ttl, ok = ThumbLinkTTL(freshThumbUrl(time.Now().Add(-time.Hour)))
	if !ok || ttl > 0 {
		t.Fatalf("expected expired ttl, got %v ok=%v", ttl, ok)
	}
}

// TestThumbDirectLink_UsesFreshListUrl 列表里的签名已是缩略图形态且还够久时,
// 直接剥离返回,不再请求任何接口。
func TestThumbDirectLink_UsesFreshListUrl(t *testing.T) {
	stubWebList(t, func(d *Pan123, ctx context.Context, parentId string) (map[int64]string, error) {
		t.Fatal("web list should not be reached")
		return nil, nil
	})
	d := &Pan123{}
	f := File{FileName: "video.mkv", FileId: 1, Size: 492020632,
		DownloadUrl: freshThumbUrl(time.Now().Add(7 * 24 * time.Hour))}

	link, err := d.thumbDirectLink(context.Background(), f, "")
	if err != nil {
		t.Fatalf("thumbDirectLink: %v", err)
	}
	if want := "/c-m9011?"; !strings.Contains(link.URL, want) {
		t.Fatalf("expected %q in %s", want, link.URL)
	}
	if strings.Contains(link.URL, "trade_key=123pan-thumbnail") {
		t.Fatalf("trade_key not stripped: %s", link.URL)
	}
	if link.Expiration == nil || *link.Expiration <= 0 {
		t.Fatalf("expected positive expiration, got %v", link.Expiration)
	}
	if link.Header.Get("Referer") != "https://download-cdn.cjjd19.com/" {
		t.Fatalf("unexpected referer: %q", link.Header.Get("Referer"))
	}
}

// TestThumbDirectLink_PrefersWebThumbOverPlainList android/tv 平台列表给的是普通形态
// (计费与 download_info 相同),此时应拉一次 web 列表换成不计额度的缩略图形态。
func TestThumbDirectLink_PrefersWebThumbOverPlainList(t *testing.T) {
	expire := time.Now().Add(7 * 24 * time.Hour)
	plain, _ := StripThumbTransform(freshThumbUrl(expire)) // 模拟 android 列表:无变换后缀/trade_key
	calls := 0
	stubWebList(t, func(d *Pan123, ctx context.Context, parentId string) (map[int64]string, error) {
		calls++
		if parentId != "9" {
			t.Fatalf("unexpected parentId %q", parentId)
		}
		return map[int64]string{1: freshThumbUrl(expire)}, nil
	})

	d := &Pan123{}
	f := File{FileName: "video.mkv", FileId: 1, ParentFileId: 9, DownloadUrl: plain}
	link, err := d.thumbDirectLink(context.Background(), f, "")
	if err != nil {
		t.Fatalf("thumbDirectLink: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected one web list call, got %d", calls)
	}
	if strings.Contains(link.URL, "_24_24") || strings.Contains(link.URL, "trade_key") {
		t.Fatalf("expected stripped thumb url, got %s", link.URL)
	}

	// 第二次命中缓存,不再发请求。
	if _, err := d.thumbDirectLink(context.Background(), f, ""); err != nil {
		t.Fatalf("second thumbDirectLink: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected web list to be cached, got %d calls", calls)
	}
}

// TestThumbDirectLink_FallsBackToPlainListUrl web 列表拿不到时,退而用手上这条普通形态的
// 直链——计费与 download_info 相同,但仍省掉一次 POST 和一次 302 探测。
func TestThumbDirectLink_FallsBackToPlainListUrl(t *testing.T) {
	plain, _ := StripThumbTransform(freshThumbUrl(time.Now().Add(7 * 24 * time.Hour)))
	stubWebList(t, func(d *Pan123, ctx context.Context, parentId string) (map[int64]string, error) {
		return nil, errors.New("token expired")
	})

	d := &Pan123{}
	link, err := d.thumbDirectLink(context.Background(), File{FileId: 2, DownloadUrl: plain}, "")
	if err != nil {
		t.Fatalf("thumbDirectLink: %v", err)
	}
	if link.URL != plain {
		t.Fatalf("expected plain list url, got %s", link.URL)
	}
}

// TestThumbDirectLink_NoUsableUrl 列表没给直链且 web 列表也失败时报错,由 Link() 回退。
func TestThumbDirectLink_NoUsableUrl(t *testing.T) {
	stubWebList(t, func(d *Pan123, ctx context.Context, parentId string) (map[int64]string, error) {
		return nil, errors.New("nope")
	})
	d := &Pan123{}
	if _, err := d.thumbDirectLink(context.Background(), File{FileId: 3}, ""); !errors.Is(err, ErrNoThumbLink) {
		t.Fatalf("expected ErrNoThumbLink, got %v", err)
	}
}

func TestThumbDirectLink_DirIsSkipped(t *testing.T) {
	d := &Pan123{}
	if _, err := d.thumbDirectLink(context.Background(), File{Type: 1}, ""); !errors.Is(err, ErrNoThumbLink) {
		t.Fatalf("expected ErrNoThumbLink for dir, got %v", err)
	}
}

// stubWebList 替换 web 列表拉取并清空缓存,避免用例间互相污染。
func stubWebList(t *testing.T, fn func(*Pan123, context.Context, string) (map[int64]string, error)) {
	orig := fetchWebThumbUrls
	fetchWebThumbUrls = fn
	webThumbCache.Clear()
	t.Cleanup(func() {
		fetchWebThumbUrls = orig
		webThumbCache.Clear()
	})
}
