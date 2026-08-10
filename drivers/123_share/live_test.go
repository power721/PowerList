//go:build live123share

// 联网端到端验证「无限直链」:匿名列公开分享 -> 取列表签名直链 -> 剥离缩略图标记 -> Range 请求。
// 只在需要实测时手动跑(会访问 123 线上接口):
//
//	go test -tags live123share -v -run TestLiveUnlimitedLink ./drivers/123_share/ \
//	  -share IpPUVv-h1Dj -pwd JZMM
package _123Share

import (
	"context"
	"flag"
	"net/http"
	"strconv"
	"testing"

	_123 "github.com/OpenListTeam/OpenList/v4/drivers/123"
	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
)

var (
	liveShareKey = flag.String("share", "", "123 share key")
	liveSharePwd = flag.String("pwd", "", "123 share password")
)

func TestLiveUnlimitedLink(t *testing.T) {
	if *liveShareKey == "" {
		t.Skip("pass -share <shareKey> [-pwd <pwd>] to run")
	}
	if conf.Conf == nil {
		conf.Conf = conf.DefaultConfig(t.TempDir())
	}
	base.InitClient()
	d := &Pan123Share{}
	d.ShareKey = *liveShareKey
	d.SharePwd = *liveSharePwd
	ctx := context.Background()

	// 递归找第一个带 DownloadUrl 的视频文件。
	var find func(parent string, depth int) *File
	find = func(parent string, depth int) *File {
		if depth > 4 {
			return nil
		}
		files, err := d.getFilesAnon(ctx, parent)
		if err != nil {
			t.Fatalf("getFilesAnon(%s): %v", parent, err)
		}
		for i := range files {
			if !files[i].IsDir() && files[i].DownloadUrl != "" {
				if _, stripped := _123.StripThumbTransform(files[i].DownloadUrl); stripped {
					return &files[i]
				}
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
		t.Skip("no video file with a thumbnail DownloadUrl in this share")
	}
	t.Logf("file=%s size=%d parent=%d", f.FileName, f.Size, f.ParentFileId)

	link, err := d.thumbDirectLink(ctx, *f, "")
	if err != nil {
		t.Fatalf("thumbDirectLink: %v", err)
	}
	t.Logf("ttl=%v url=%s", *link.Expiration, link.URL)

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
