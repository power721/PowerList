package _115

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	driver115 "github.com/power721/115driver/pkg/driver"
)

func restoreShareVars(send, update string) func() {
	origSend, origUpdate := shareSendURL, shareUpdateURL
	shareSendURL, shareUpdateURL = send, update
	return func() {
		shareSendURL, shareUpdateURL = origSend, origUpdate
	}
}

// shareStubVars 把两个端点指到同一个 httptest 服务器(保留路径以供分派)。
func shareStubVars(srv *httptest.Server) func() {
	return restoreShareVars(srv.URL+"/share/send", srv.URL+"/share/updateshare")
}

// 永久分享两步链:share/send(ignore_warn=1 + user_id + file_ids)→
// updateshare(share_duration=-1,Referer cdnres);缺第二步只有 15 天。
func TestPan115CreatePermanentShare(t *testing.T) {
	type captured struct {
		path    string
		form    string
		referer string
	}
	var got []captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		var form strings.Builder
		for _, key := range []string{"user_id", "file_ids", "ignore_warn", "share_code", "share_duration"} {
			if v := r.Form.Get(key); v != "" {
				form.WriteString(key + "=" + v + ";")
			}
		}
		got = append(got, captured{r.URL.Path, form.String(), r.Header.Get("Referer")})
		switch r.URL.Path {
		case "/share/send":
			_, _ = w.Write([]byte(`{"state":true,"errno":0,"data":{"share_code":"swsexqo3hjs",` +
				`"receive_code":"6666","share_url":"https://115cdn.com/s/swsexqo3hjs",` +
				`"share_title":"剧名 (2024)","share_ex_duration":"15天"}}`))
		case "/share/updateshare":
			_, _ = w.Write([]byte(`{"state":true,"errno":0}`))
		default:
			_, _ = w.Write([]byte(`{"state":false,"error":"unexpected path"}`))
		}
	}))
	defer srv.Close()
	defer shareStubVars(srv)()

	client := driver115.New()
	client.UserID = 6338615
	d := &Pan115{}
	result, err := d.createPermanentShare(context.Background(), client,
		&model.Object{ID: "3189661663297662199", Name: "剧名 (2024)", IsFolder: true})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("requests = %d, want 2 (send + updateshare)", len(got))
	}
	if want := "user_id=6338615;file_ids=3189661663297662199;ignore_warn=1;"; got[0].form != want {
		t.Errorf("send form: got %q want %q", got[0].form, want)
	}
	if got[0].path != "/share/send" || got[0].referer != "https://115.com/" {
		t.Errorf("send endpoint/referer: got %s / %q", got[0].path, got[0].referer)
	}
	if want := "share_code=swsexqo3hjs;share_duration=-1;"; got[1].form != want {
		t.Errorf("update form: got %q want %q", got[1].form, want)
	}
	if got[1].path != "/share/updateshare" || got[1].referer != "https://cdnres.115.com/" {
		t.Errorf("update endpoint/referer: got %s / %q", got[1].path, got[1].referer)
	}
	if result.ShareCode != "swsexqo3hjs" || result.ReceiveCode != "6666" {
		t.Errorf("result: got %+v", result)
	}
}

// share/send 报错(state=false)时透传原始 message,由调用方决定重试。
func TestPan115CreatePermanentShare_ApiError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"state":false,"errno":990001,"error":"分享次数已达上限"}`))
	}))
	defer srv.Close()
	defer shareStubVars(srv)()

	client := driver115.New()
	client.UserID = 1
	d := &Pan115{}
	_, err := d.createPermanentShare(context.Background(), client, &model.Object{ID: "1", IsFolder: true})
	if err == nil || !strings.Contains(err.Error(), "分享次数已达上限") {
		t.Fatalf("expected api error passthrough, got %v", err)
	}
}

// updateshare 失败时整体报错且错误携带 share_code(供调用方登记孤儿分享)。
func TestPan115CreatePermanentShare_UpdateFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/share/send":
			_, _ = w.Write([]byte(`{"state":true,"data":{"share_code":"abc123","receive_code":"w816"}}`))
		default:
			_, _ = w.Write([]byte(`{"state":false,"error":"更新失败"}`))
		}
	}))
	defer srv.Close()
	defer shareStubVars(srv)()

	client := driver115.New()
	client.UserID = 1
	d := &Pan115{}
	_, err := d.createPermanentShare(context.Background(), client, &model.Object{ID: "1", IsFolder: true})
	if err == nil || !strings.Contains(err.Error(), "abc123") {
		t.Fatalf("expected share_code in error, got %v", err)
	}
}

// UserID 未知(LoginCheck 未跑)时直接拒绝,不发请求。
func TestPan115CreatePermanentShare_RequiresUserID(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	defer shareStubVars(srv)()

	client := driver115.New()
	d := &Pan115{}
	_, err := d.createPermanentShare(context.Background(), client, &model.Object{ID: "1", IsFolder: true})
	if err == nil || !strings.Contains(err.Error(), "user id") {
		t.Fatalf("expected user id guard, got %v", err)
	}
	if called {
		t.Fatal("no request should be sent without user id")
	}
}
