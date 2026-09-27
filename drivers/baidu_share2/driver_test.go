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
	if !strings.Contains(err.Error(), "errno=12") {
		t.Fatalf("transfer error must carry errno for diagnosis, got %v", err)
	}
}

// 半初始化自愈(2026-09-27 线上实证):getInfo 吃到降级页 → ShareId/ShareUk 空而 Token 在,
// 转存须先重新 Validate 补齐分享参数再发起,拿补齐后的 shareid/from 打 /share/transfer。
func TestBaiduShare2SaveTo_EmptyShareIdSelfHeals(t *testing.T) {
	restore := stubAccountCookie("")
	defer restore()

	pageCalls, transferCalls := 0, 0
	var gotShareId, gotFrom string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/share/transfer":
			transferCalls++
			gotShareId = r.URL.Query().Get("shareid")
			gotFrom = r.URL.Query().Get("from")
			_, _ = w.Write([]byte(`{"errno":0,"extra":{"list":[` +
				`{"from_fs_id":111,"to":"/t/a.nfo","to_fs_id":900001}]}}`))
		default: // getInfo 开页(/s/1abc)
			pageCalls++
			_, _ = w.Write([]byte(`<html><body>shareid: "888"; share_uk: "999";</body></html>`))
		}
	}))
	defer srv.Close()

	d := &BaiduShare2{Addition: Addition{Surl: "1abc"}, Token: "tok-but-no-shareid"}
	d.client = resty.New().SetBaseURL(srv.URL)
	bd := &baidu_netdisk.BaiduNetdisk{}
	bd.Cookie = "BDUSS=abc"
	dst := &model.Object{ID: "1", Name: "t", Path: "/t", IsFolder: true}

	saved, err := d.SaveTo(context.Background(), bd, dst, []model.Obj{&model.Object{ID: "111", Name: "a.nfo"}})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if strings.Join(saved, ",") != "900001" {
		t.Errorf("expected saved [900001], got %v", saved)
	}
	if pageCalls != 1 || transferCalls != 1 {
		t.Fatalf("expected 1 re-validate page + 1 transfer, got %d/%d", pageCalls, transferCalls)
	}
	if gotShareId != "888" || gotFrom != "999" {
		t.Errorf("transfer must carry healed shareid/from, got %q/%q", gotShareId, gotFrom)
	}
}

// 重验证后 shareid 仍解析不到(页面持续降级)时,须透出明确文案并拦下发,
// 不得拿空 shareid 打 /share/transfer 换回模糊的 errno=2「参数错误」。
func TestBaiduShare2SaveTo_EmptyShareIdStillMissingSurfacesClearError(t *testing.T) {
	restore := stubAccountCookie("")
	defer restore()

	transferCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/share/transfer" {
			transferCalls++
		}
		_, _ = w.Write([]byte(`<html><body>degraded page without shareid</body></html>`))
	}))
	defer srv.Close()

	d := &BaiduShare2{Addition: Addition{Surl: "1abc"}, Token: "tok-but-no-shareid"}
	d.client = resty.New().SetBaseURL(srv.URL)
	bd := &baidu_netdisk.BaiduNetdisk{}
	bd.Cookie = "BDUSS=abc"
	dst := &model.Object{ID: "1", Name: "t", Path: "/t", IsFolder: true}

	_, err := d.SaveTo(context.Background(), bd, dst, []model.Obj{&model.Object{ID: "111", Name: "a.nfo"}})
	if err == nil || !strings.Contains(err.Error(), "shareid") {
		t.Fatalf("expected clear shareid-missing error, got %v", err)
	}
	if strings.Contains(err.Error(), "参数错误") {
		t.Fatalf("must not surface baidu's vague errno=2 text, got %v", err)
	}
	if transferCalls != 0 {
		t.Fatalf("transfer must not fire with empty shareid, got %d calls", transferCalls)
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

// stubAccountCookie 隔离 op:单测里 GetFirstDriver 缓存未命中落 db 查询会死锁,恒返回空 Cookie。
func stubAccountCookie(cookie string) func() {
	orig := baiduAccountCookie
	baiduAccountCookie = func() string { return cookie }
	return func() { baiduAccountCookie = orig }
}

// List 入口校验失败(verify 连吃 -62 风控拿不到 randsk)且 Token 仍为空时,须直接返回 verify 的
// 真实错误,不再拿空 sekey 去 /share/list 白挨一发 -9「提取码验证失败」遮住根因
// (2026-09-24 线上实证:4 发 verify 全 -62,用户看到的却是 -9 文案,被带偏去查提取码/死链)。
func TestBaiduShare2List_EntryValidateFailSurfacesVerifyError(t *testing.T) {
	restore := stubAccountCookie("")
	defer restore()

	verifyCalls, listCalls := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/share/verify" {
			verifyCalls++
			_, _ = w.Write([]byte(`{"errno":-62,"request_id":1}`))
			return
		}
		listCalls++
		t.Errorf("must not call %v when entry validate failed", r.URL.Path)
	}))
	defer srv.Close()

	d := &BaiduShare2{Addition: Addition{Surl: "1abc", Pwd: "yptv"}}
	d.client = resty.New().SetBaseURL(srv.URL)

	_, err := d.List(context.Background(), &model.Object{Path: "/"}, model.ListArgs{})
	if err == nil || !strings.Contains(err.Error(), "百度风控") {
		t.Fatalf("expected -62 throttle error from verify, got %v", err)
	}
	if verifyCalls != 2 { // Validate 对 -62 内含一次 2s 退避重试
		t.Errorf("expected 2 verify calls (internal -62 retry), got %d", verifyCalls)
	}
	if listCalls != 0 {
		t.Errorf("/share/list must not be hit with empty sekey, got %d calls", listCalls)
	}
	if d.Token != "" {
		t.Errorf("token must stay empty on failed validate, got %q", d.Token)
	}
}

// List 遇瞬时 -9(sekey 过期)清 Token 重验证,重验证也失败时须透出 verify 的错误,
// 而非沿用旧列表响应的 -9 文案——文案错会误导排查,也会让 alist-tvbox 把风控误归类。
func TestBaiduShare2List_RevalidateFailSurfacesVerifyError(t *testing.T) {
	restore := stubAccountCookie("")
	defer restore()

	listCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/share/list":
			listCalls++
			_, _ = w.Write([]byte(`{"errno":-9,"show_msg":"提取码验证失败,请重试","list":[]}`))
		case "/share/verify":
			_, _ = w.Write([]byte(`{"errno":105,"err_msg":""}`))
		}
	}))
	defer srv.Close()

	d := &BaiduShare2{Addition: Addition{Surl: "1abc", Pwd: "yptv"}, Token: "stale-sekey"}
	d.client = resty.New().SetBaseURL(srv.URL)

	_, err := d.List(context.Background(), &model.Object{Path: "/"}, model.ListArgs{})
	if err == nil || !strings.Contains(err.Error(), "errno=105") {
		t.Fatalf("expected re-validate error (errno=105) to surface, got %v", err)
	}
	if strings.Contains(err.Error(), "提取码验证失败") {
		t.Fatalf("stale -9 list message must not mask the real cause, got %v", err)
	}
	if listCalls != 1 {
		t.Errorf("list should run once then bail on failed re-validate, got %d calls", listCalls)
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
			restore := stubAccountCookie("")
			defer restore()
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

// getInfo 开页必须带账号 Cookie(对齐 verify 的防风控做法):裸 netdisk UA 高频开页吃到无
// shareid 的降级页,会让存储停留在「Token 有效但 ShareId 空」的半初始化态(2026-09-27 线上
// 实证:126 个分享连续中招,转存全数 errno=2「参数错误」)。
func TestBaiduShare2GetInfo_SendsAccountCookie(t *testing.T) {
	restore := stubAccountCookie("BDUSS=acct")
	defer restore()

	var gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		_, _ = w.Write([]byte(`<html><body>shareid: "123"; share_uk: "456";</body></html>`))
	}))
	defer srv.Close()

	d := &BaiduShare2{Addition: Addition{Surl: "1abc"}}
	d.client = resty.New().SetBaseURL(srv.URL)
	if err := d.getInfo(); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if gotCookie != "BDUSS=acct" {
		t.Fatalf("getInfo must send account cookie, got %q", gotCookie)
	}
}

// stubTransferCooldown 隔离全局转存熔断状态,测试间互不污染。
func stubTransferCooldown(t time.Time) func() {
	orig := baiduTransferCooldown
	baiduTransferCooldown = t
	return func() { baiduTransferCooldown = orig }
}

// 转存频控熔断:转存兜底报 -70(转存太快)即进入冷却,冷却期内快速失败零请求,
// 不让 strm 风暴的百余次尝试白发还加深限流(2026-09-27 线上实证窗口跨 3 小时周期)。
func TestBaiduShare2Link_TransferRateLimitTripCooldown(t *testing.T) {
	restore := stubTransferCooldown(time.Time{})
	defer restore()

	transferCalls := 0
	r := stubResolvers(
		func(d *BaiduShare2, file model.Obj) (*model.Link, error) {
			return nil, errors.New("direct off")
		},
		func(ctx context.Context, d *BaiduShare2, file model.Obj, args model.LinkArgs) (*model.Link, error) {
			transferCalls++
			return nil, errors.New("转存太快，请稍后再试(errno=-70)")
		},
	)
	defer r()
	baiduShareDirectEnabled = func() bool { return false }

	d := &BaiduShare2{}
	file := &model.Object{ID: "file-1", Name: "v.mp4"}

	_, err := d.Link(context.Background(), file, model.LinkArgs{})
	if err == nil || !strings.Contains(err.Error(), "errno=-70") {
		t.Fatalf("expected -70 error, got %v", err)
	}
	if !time.Now().Before(baiduTransferCooldown) {
		t.Fatalf("cooldown must be armed after -70, got %v", baiduTransferCooldown)
	}
	_, err = d.Link(context.Background(), file, model.LinkArgs{})
	if err == nil || !strings.Contains(err.Error(), "冷却") {
		t.Fatalf("expected fast-fail cooldown error, got %v", err)
	}
	if transferCalls != 1 {
		t.Fatalf("cooldown must fast-fail without firing transfer, got %d calls", transferCalls)
	}
}

// 冷却到期后恢复探测:不把临时频控变成永久失败。
func TestBaiduShare2Link_CooldownExpiredProbesAgain(t *testing.T) {
	restore := stubTransferCooldown(time.Now().Add(-time.Minute))
	defer restore()

	transferCalls := 0
	r := stubResolvers(
		nil,
		func(ctx context.Context, d *BaiduShare2, file model.Obj, args model.LinkArgs) (*model.Link, error) {
			transferCalls++
			return &model.Link{URL: "https://transfer/" + file.GetID()}, nil
		},
	)
	defer r()
	baiduShareDirectEnabled = func() bool { return false }

	d := &BaiduShare2{}
	link, err := d.Link(context.Background(), &model.Object{ID: "f1"}, model.LinkArgs{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if transferCalls != 1 || !strings.HasPrefix(link.URL, "https://transfer/") {
		t.Fatalf("expired cooldown must probe transfer again, got %d calls", transferCalls)
	}
}

// 熔断只作用于转存兜底:免转存主路径命中时,即便转存冷却中也照常返回直链。
func TestBaiduShare2Link_DirectBypassesTransferCooldown(t *testing.T) {
	restore := stubTransferCooldown(time.Now().Add(10 * time.Minute))
	defer restore()

	transferCalls := 0
	r := stubResolvers(
		func(d *BaiduShare2, file model.Obj) (*model.Link, error) {
			return &model.Link{URL: "https://d.pcs.baidu.com/dlna/" + file.GetID()}, nil
		},
		func(ctx context.Context, d *BaiduShare2, file model.Obj, args model.LinkArgs) (*model.Link, error) {
			transferCalls++
			return nil, errors.New("must not reach transfer")
		},
	)
	defer r()

	d := &BaiduShare2{}
	link, err := d.Link(context.Background(), &model.Object{ID: "f1"}, model.LinkArgs{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if transferCalls != 0 || !strings.HasPrefix(link.URL, "https://d.pcs.baidu.com/dlna/") {
		t.Fatalf("direct path must bypass cooldown, got %d transfer calls", transferCalls)
	}
}

// sekey 按存储缓存:免转存默认开后,刮削风暴逐链 verify+开页会复刻 -62 风控,
// 命中缓存零请求;失效(Delete)后重新取;取失败透出错误交调用方回退。
func TestAcquireDlnaSekey_CacheAndRefetch(t *testing.T) {
	origCache, origFetch := baiduDirectSekeyCache, fetchFreshSekey
	baiduDirectSekeyCache = cache.NewKeyedCache[string](30 * time.Minute)
	fetchCalls := 0
	fetchFreshSekey = func(d *BaiduShare2, accountCookie string) (string, error) {
		fetchCalls++
		return "sekey-" + strings.ToLower(accountCookie), nil
	}
	defer func() {
		baiduDirectSekeyCache, fetchFreshSekey = origCache, origFetch
	}()

	d := &BaiduShare2{}
	s1, cached1, err := acquireDlnaSekey(d, "BDUSS=x")
	if err != nil || s1 != "sekey-bduss=x" || cached1 {
		t.Fatalf("first acquire must fetch fresh: %q %v %v", s1, cached1, err)
	}
	s2, cached2, err := acquireDlnaSekey(d, "BDUSS=x")
	if err != nil || s2 != s1 || !cached2 || fetchCalls != 1 {
		t.Fatalf("second acquire must hit cache: %q %v %v fetch=%d", s2, cached2, err, fetchCalls)
	}
	baiduDirectSekeyCache.Delete("0")
	if _, cached3, _ := acquireDlnaSekey(d, "BDUSS=x"); cached3 || fetchCalls != 2 {
		t.Fatalf("after invalidate must refetch: cached=%v fetch=%d", cached3, fetchCalls)
	}

	fetchFreshSekey = func(d *BaiduShare2, accountCookie string) (string, error) {
		return "", errors.New("risk controlled")
	}
	baiduDirectSekeyCache.Delete("0")
	if _, _, err := acquireDlnaSekey(d, "BDUSS=x"); err == nil {
		t.Fatalf("fetch failure must surface error")
	}
}
