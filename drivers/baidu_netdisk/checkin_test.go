package baidu_netdisk

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-resty/resty/v2"
)

var baiduCheckinSeamMu sync.Mutex

func useBaiduActivityServer(t *testing.T, handler http.Handler) {
	t.Helper()
	baiduCheckinSeamMu.Lock()
	server := httptest.NewServer(handler)
	originalURL := baiduActivityBaseURL
	originalClient := baiduActivityClient
	baiduActivityBaseURL = server.URL
	baiduActivityClient = func() *resty.Client { return resty.New() }
	t.Cleanup(func() {
		baiduActivityBaseURL = originalURL
		baiduActivityClient = originalClient
		server.Close()
		baiduCheckinSeamMu.Unlock()
	})
}

func TestBaiduMembershipSigninReturnsPoints(t *testing.T) {
	useBaiduActivityServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/2.0/membership/level" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("app_id") != "250528" || query.Get("web") != "5" || query.Get("method") != "signin" {
			t.Fatalf("unexpected query %v", query)
		}
		if r.Header.Get("Cookie") != "BDUSS=cookie-secret; STOKEN=token-secret" {
			t.Fatalf("unexpected Cookie header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"points":10}`)
	}))

	d := &BaiduNetdisk{Addition: Addition{Cookie: "BDUSS=cookie-secret; STOKEN=token-secret"}}
	result, err := d.membershipSignin()
	if err != nil {
		t.Fatalf("membership signin: %v", err)
	}
	if result.AlreadyComplete || result.Points != 10 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestBaiduMembershipSigninTreatsAlreadySignedAsSuccess(t *testing.T) {
	useBaiduActivityServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"error_code":1,"error_msg":"今日已签到"}`)
	}))

	result, err := (&BaiduNetdisk{Addition: Addition{Cookie: "BDUSS=secret"}}).membershipSignin()
	if err != nil {
		t.Fatalf("already signed: %v", err)
	}
	if !result.AlreadyComplete {
		t.Fatalf("expected idempotent success, got %+v", result)
	}
}

func TestSafeBaiduActivityMessageRedactsCookieValuesAndBoundsOutput(t *testing.T) {
	cookie := "BDUSS=cookie-secret; STOKEN=token-secret"
	message := "echo cookie-secret token-secret " + strings.Repeat("x", baiduActivityMessageLimit+50)
	got := safeBaiduActivityMessage(message, cookie)
	if strings.Contains(got, "cookie-secret") || strings.Contains(got, "token-secret") {
		t.Fatalf("Cookie value leaked: %q", got)
	}
	if len(got) > baiduActivityMessageLimit+3 {
		t.Fatalf("message was not bounded: length=%d", len(got))
	}
}

func TestBaiduMembershipSigninRejectsMissingResult(t *testing.T) {
	useBaiduActivityServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))

	_, err := (&BaiduNetdisk{Addition: Addition{Cookie: "BDUSS=secret"}}).membershipSignin()
	if err == nil || !strings.Contains(err.Error(), "omitted points") {
		t.Fatalf("expected missing-points error, got %v", err)
	}
}

func TestBaiduMembershipSigninRejectsMalformedJSONWithoutLeakingCookie(t *testing.T) {
	useBaiduActivityServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{`)
	}))

	cookie := "BDUSS=cookie-secret"
	_, err := (&BaiduNetdisk{Addition: Addition{Cookie: cookie}}).membershipSignin()
	if err == nil {
		t.Fatal("expected malformed-JSON error")
	}
	if strings.Contains(err.Error(), cookie) || strings.Contains(err.Error(), "cookie-secret") {
		t.Fatalf("Cookie leaked in decode error: %v", err)
	}
}
