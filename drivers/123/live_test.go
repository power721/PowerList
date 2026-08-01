//go:build live123

// 联网端到端验证个人盘的「无限直链」:列目录 -> 取列表签名直链 -> 剥离缩略图标记 -> Range 请求。
// 只在需要实测时手动跑(会带 token 访问 123 线上接口):
//
//	go test -tags live123 -v -run TestLiveUnlimitedLink ./drivers/123/ -token "$(cat token.txt)"
package _123

import (
	"context"
	"encoding/hex"
	"flag"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
)

var liveToken = flag.String("token", "", "123pan web access token (Bearer)")

func TestLiveUnlimitedLink(t *testing.T) {
	if *liveToken == "" {
		t.Skip("pass -token <bearer> to run")
	}
	if conf.Conf == nil {
		conf.Conf = conf.DefaultConfig(t.TempDir())
	}
	base.InitClient()

	d := &Pan123{}
	d.AccessToken = *liveToken
	d.PlatformType = "android"
	d.DeviceName = "Xiaomi"
	d.DeiveType = "M1810E5A"
	d.OsVersion = "Android_8.1.0"
	if err := d.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	ctx := context.Background()

	var find func(parent string, depth int) *File
	find = func(parent string, depth int) *File {
		if depth > 3 {
			return nil
		}
		files, err := d.getFiles(ctx, parent, "")
		if err != nil {
			t.Fatalf("getFiles(%s): %v", parent, err)
		}
		for i := range files {
			if !files[i].IsDir() && files[i].Size > 0 {
				return &files[i]
			}
		}
		for i := range files {
			if files[i].IsDir() {
				if f := find(strconv.FormatInt(files[i].FileId, 10), depth+1); f != nil {
					return f
				}
			}
		}
		return nil
	}

	f := find("0", 0)
	if f == nil {
		t.Skip("no file found in this account")
	}
	t.Logf("file=%s size=%d parent=%d", f.FileName, f.Size, f.ParentFileId)

	link, err := d.thumbDirectLink(ctx, *f, "")
	if err != nil {
		t.Fatalf("thumbDirectLink: %v", err)
	}
	t.Logf("ttl=%v url=%s", *link.Expiration, link.URL)
	// bzs 是计费主体:web 列表的缩略图直链是 hex("<uid>:2:0:0")(末段计费字节数为 0),
	// android 列表 / download_info 则是 hex("<uid>:3:1:<size>")。
	if q, e := url.ParseQuery(mustQuery(link.URL)); e == nil {
		if raw := q.Get("bzs"); raw != "" {
			if dec, e := hex.DecodeString(raw); e == nil {
				t.Logf("bzc=%s bzs=%s", q.Get("bzc"), dec)
			}
		}
	}

	req, _ := http.NewRequest(http.MethodGet, link.URL, nil)
	for k, v := range link.Header {
		req.Header[k] = v
	}
	req.Header.Set("Range", "bytes=0-1023")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("range get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d", res.StatusCode)
	}
	cr := res.Header.Get("Content-Range")
	t.Logf("status=%d Content-Range=%s", res.StatusCode, cr)
	if want := "/" + strconv.FormatInt(f.Size, 10); len(cr) < len(want) || cr[len(cr)-len(want):] != want {
		t.Fatalf("Content-Range %q does not end with full size %q", cr, want)
	}
}

// mustQuery 取 URL 的 query 部分(测试辅助,失败返回空串)。
func mustQuery(raw string) string {
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return raw[i+1:]
	}
	return ""
}
