package baidu_share

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/drivers/baidu_netdisk"
	"github.com/OpenListTeam/OpenList/v4/internal/cache"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/go-resty/resty/v2"
)

// stubResolvers 替换两条取链路径为可控桩函数,隔离 op/storage,返回还原函数。
func stubResolvers(direct func(d *BaiduShare2, file model.Obj) (*model.Link, error),
	transfer func(ctx context.Context, d *BaiduShare2, file model.Obj, args model.LinkArgs) (*model.Link, error)) func() {
	origCache := baiduShareLinkCache
	origDirect := resolveShareDirectLink
	origTransfer := resolveBaiduShareLink
	origEnabled := baiduShareDirectEnabled
	baiduShareLinkCache = cache.NewKeyedCache[*model.Link](time.Hour)
	resolveShareDirectLink = direct
	resolveBaiduShareLink = transfer
	baiduShareDirectEnabled = func() bool { return true } // 现有用例测免转存路径,默认开
	return func() {
		baiduShareLinkCache = origCache
		resolveShareDirectLink = origDirect
		resolveBaiduShareLink = origTransfer
		baiduShareDirectEnabled = origEnabled
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

// 开关关 → 直接走转存,免转存路径不应被调用。
func TestBaiduShare2Link_DirectDisabledSkipsDirect(t *testing.T) {
	directCalls, transferCalls := 0, 0
	restore := stubResolvers(
		func(d *BaiduShare2, file model.Obj) (*model.Link, error) {
			directCalls++
			return &model.Link{URL: "https://d.pcs.baidu.com/dlna/" + file.GetID()}, nil
		},
		func(ctx context.Context, d *BaiduShare2, file model.Obj, args model.LinkArgs) (*model.Link, error) {
			transferCalls++
			return &model.Link{URL: "https://transfer/" + file.GetID()}, nil
		},
	)
	defer restore()                                        // stubResolvers 已捕获并还原 gate
	baiduShareDirectEnabled = func() bool { return false } // 覆盖为关

	d := &BaiduShare2{}
	file := &model.Object{ID: "file-1", Name: "v.mp4"}
	link, err := d.Link(context.Background(), file, model.LinkArgs{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if directCalls != 0 {
		t.Fatalf("开关关时不应调用免转存, got %d direct calls", directCalls)
	}
	if transferCalls != 1 {
		t.Fatalf("应直接走转存一次, got %d transfer calls", transferCalls)
	}
	if !strings.HasPrefix(link.URL, "https://transfer/") {
		t.Fatalf("expected 转存 link, got %s", link.URL)
	}
}

// SaveTo 服务端转存:批量 fs_id 一次请求、sekey 用解码后的 Token、目标目录取 dstDir 路径;
// 返回新建对象 fs_id 列表。非百度账号目标直接拒绝。
func TestBaiduShare2SaveTo(t *testing.T) {
	var gotPath, gotFsidList, gotSekey, gotShareId, gotFrom string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/share/transfer") {
			t.Errorf("unexpected path: %v", r.URL.Path)
		}
		_ = r.ParseForm()
		gotPath = r.Form.Get("path")
		gotFsidList = r.Form.Get("fsidlist")
		gotSekey = r.Form.Get("sekey")
		gotShareId = r.URL.Query().Get("shareid")
		gotFrom = r.URL.Query().Get("from")
		_, _ = w.Write([]byte(`{"errno":0,"extra":{"list":[` +
			`{"from_fs_id":111,"to":"/我的追剧/剧/第01集.mp4","to_fs_id":900001},` +
			`{"from_fs_id":222,"to":"/我的追剧/剧/第02集.mp4","to_fs_id":900002}]}}`))
	}))
	defer srv.Close()

	d := &BaiduShare2{ShareId: "123", ShareUk: "456", Token: "sec%2Bkey"}
	d.client = resty.New().SetBaseURL(srv.URL)
	bd := &baidu_netdisk.BaiduNetdisk{}
	bd.Cookie = "BDUSS=abc"
	dst := &model.Object{ID: "1", Name: "剧", Path: "/我的追剧/剧", IsFolder: true}
	objs := []model.Obj{
		&model.Object{ID: "111", Name: "第01集.mp4"},
		&model.Object{ID: "222", Name: "第02集.mp4"},
	}

	saved, err := d.SaveTo(context.Background(), bd, dst, objs)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if strings.Join(saved, ",") != "900001,900002" {
		t.Errorf("expected new fs_ids [900001 900002], got %v", saved)
	}
	if gotPath != "/我的追剧/剧" {
		t.Errorf("path param: got %q want /我的追剧/剧", gotPath)
	}
	if gotFsidList != "[111,222]" {
		t.Errorf("fsidlist param: got %q want [111,222]", gotFsidList)
	}
	if gotSekey != "sec+key" {
		t.Errorf("sekey param must be the decoded token: got %q want sec+key", gotSekey)
	}
	if gotShareId != "123" || gotFrom != "456" {
		t.Errorf("shareid/from params: got %q/%q want 123/456", gotShareId, gotFrom)
	}
}

// 转存接口报错(errno!=0)时须把 show_msg 作为错误上抛,由调用方回退字节中转 copy。
func TestBaiduShare2SaveTo_ApiErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errno":12,"show_msg":"文件已存在"}`))
	}))
	defer srv.Close()

	d := &BaiduShare2{ShareId: "123", ShareUk: "456", Token: "seckey"}
	d.client = resty.New().SetBaseURL(srv.URL)
	bd := &baidu_netdisk.BaiduNetdisk{}
	dst := &model.Object{ID: "1", Name: "剧", Path: "/我的追剧/剧", IsFolder: true}

	_, err := d.SaveTo(context.Background(), bd, dst, []model.Obj{&model.Object{ID: "111", Name: "第01集.mp4"}})
	if err == nil || !strings.Contains(err.Error(), "文件已存在") {
		t.Fatalf("expected api error to propagate, got %v", err)
	}
}

// 目标存储不是百度网盘账号驱动(如夸克账号)时拒绝,不发起任何请求。
func TestBaiduShare2SaveTo_RejectsNonBaiduTarget(t *testing.T) {
	d := &BaiduShare2{ShareId: "123", ShareUk: "456", Token: "seckey"}
	dst := &model.Object{ID: "1", Name: "剧", Path: "/我的追剧/剧", IsFolder: true}

	_, err := d.SaveTo(context.Background(), nil, dst, []model.Obj{&model.Object{ID: "111", Name: "第01集.mp4"}})
	if err == nil || !strings.Contains(err.Error(), "百度网盘账号") {
		t.Fatalf("expected non-baidu target rejection, got %v", err)
	}
}

// baiduErrnoMessage 翻译契约:-9 混合态(sekey 瞬时过期与分享被取消同码)文案须含
// 「提取码验证失败」(命中 alist-tvbox 的会话过期正则归瞬时)且不含「已取消/失效/不存在」
// 连续串(命中其失效清理关键字会被误判死);105/-21 死链文案则反之,须含失效措辞。
func TestBaiduErrnoMessage(t *testing.T) {
	cases := []struct {
		errno int64
		body  string
		want  string
	}{
		{-9, `{"errno":-9,"err_msg":"","request_id":86674205666294610}`, "提取码验证失败"},
		{105, `{"errno":105,"err_msg":"","request_id":86755293159184387}`, "分享不存在"},
		{-21, ``, "分享已取消"},
		{-19, ``, "访问频率太快"},
		{-62, ``, "百度风控"},
		{-65, ``, "操作过于频繁"},
		{12, `{"errno":12,"show_msg":"文件已存在"}`, "文件已存在"},
	}
	for _, c := range cases {
		if got := baiduErrnoMessage(c.errno, c.body); !strings.Contains(got, c.want) {
			t.Errorf("errno %d: got %q, want contains %q", c.errno, got, c.want)
		}
	}
	for _, gone := range []string{"已取消", "失效", "不存在", "expired", "cancel"} {
		if msg := baiduErrnoMessage(-9, ""); strings.Contains(msg, gone) {
			t.Errorf("-9 文案不得含失效措辞 %q(瞬时态会被 alist-tvbox 误判死): %q", gone, msg)
		}
	}
	if msg := baiduErrnoMessage(105, ""); !strings.Contains(msg, "不存在") {
		t.Errorf("105 文案须含「不存在」让 alist-tvbox 判死清理: %q", msg)
	}
}

// getInfo 错误页检测:死链 HTTP 仍 200,靠 <title> 区分(线上实证「百度网盘-链接不存在」);
// 带码活链 302 后的输码页 title 正常且含 shareid,不得误伤。errno=-9 三形态(提取码错误/
// 链接不存在/sekey 过期)同码,HTML 开页是唯一可靠死活分界。
func TestBaiduShare2GetInfo_ErrorPageTitle(t *testing.T) {
	cases := []struct {
		name    string
		title   string
		wantErr string
	}{
		{"死链-链接不存在", "百度网盘-链接不存在", "分享不存在或已失效"},
		{"死链-已被取消", "百度网盘-分享的文件已经被取消", "分享不存在或已失效"},
		{"活链-输码页", "百度网盘 请输入提取码", ""},
		{"活链-分享页", "百度网盘-分享", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("<html><head><title>" + c.title + "</title></head><body>" +
					`shareid: "123"; share_uk: "456"; </body></html>`))
			}))
			defer srv.Close()
			d := &BaiduShare2{Addition: Addition{Surl: "1abc"}}
			d.client = resty.New().SetBaseURL(srv.URL)
			err := d.getInfo()
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("live share must not error: %v", err)
				}
				if d.ShareId != "123" || d.ShareUk != "456" {
					t.Fatalf("shareid/uk extraction broken: %q/%q", d.ShareId, d.ShareUk)
				}
			} else if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("dead share must surface %q, got %v", c.wantErr, err)
			}
		})
	}
}
