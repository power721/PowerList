package baidu_netdisk

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestBaiduDailyQuestionSubmitsDecodedAnswer(t *testing.T) {
	var submitted bool
	useBaiduActivityServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/act/v2/membergrowv2/getdailyquestion":
			_, _ = io.WriteString(w, `{"errno":0,"data":{"ask_id":12345,"answer":2}}`)
		case "/act/v2/membergrowv2/answerquestion":
			query := r.URL.Query()
			if query.Get("app_id") != "250528" || query.Get("web") != "5" || query.Get("ask_id") != "12345" || query.Get("answer") != "2" {
				t.Fatalf("unexpected answer query %v", query)
			}
			submitted = true
			_, _ = io.WriteString(w, `{"errno":0,"data":{"score":5}}`)
		default:
			http.NotFound(w, r)
		}
	}))

	d := &BaiduNetdisk{Addition: Addition{Cookie: "BDUSS=secret"}}
	answer, askID, err := d.getDailyQuestion()
	if err != nil {
		t.Fatalf("get daily question: %v", err)
	}
	result, err := d.answerDailyQuestion(answer, askID)
	if err != nil {
		t.Fatalf("answer daily question: %v", err)
	}
	if !submitted || result.Points != 5 {
		t.Fatalf("unexpected answer result: submitted=%v result=%+v", submitted, result)
	}
}

func TestBaiduDailyQuestionTreatsAlreadyAnsweredAsSuccess(t *testing.T) {
	useBaiduActivityServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"show_msg":"今日已回答，次数已用完"}`)
	}))

	result, err := (&BaiduNetdisk{Addition: Addition{Cookie: "BDUSS=secret"}}).answerDailyQuestion(2, 12345)
	if err != nil {
		t.Fatalf("already answered: %v", err)
	}
	if !result.AlreadyComplete {
		t.Fatalf("expected idempotent success, got %+v", result)
	}
}

func TestExecuteBaiduCheckinContinuesToQuestionAfterSigninFailure(t *testing.T) {
	var paths []string
	useBaiduActivityServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/rest/2.0/membership/level":
			w.WriteHeader(http.StatusBadGateway)
		case "/act/v2/membergrowv2/getdailyquestion":
			_, _ = io.WriteString(w, `{"data":{"ask_id":7,"answer":1}}`)
		case "/act/v2/membergrowv2/answerquestion":
			_, _ = io.WriteString(w, `{"data":{"score":3}}`)
		default:
			http.NotFound(w, r)
		}
	}))

	(&BaiduNetdisk{Addition: Addition{Cookie: "BDUSS=secret"}}).executeCheckin()
	want := []string{
		"/rest/2.0/membership/level",
		"/act/v2/membergrowv2/getdailyquestion",
		"/act/v2/membergrowv2/answerquestion",
	}
	if len(paths) != len(want) {
		t.Fatalf("unexpected paths: %v", paths)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("unexpected paths: want=%v got=%v", want, paths)
		}
	}
}

func TestBaiduDailyQuestionRejectsMissingIdentifiers(t *testing.T) {
	useBaiduActivityServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"answer":2}}`)
	}))

	_, _, err := (&BaiduNetdisk{Addition: Addition{Cookie: "BDUSS=secret"}}).getDailyQuestion()
	if err == nil || !strings.Contains(err.Error(), "ask_id") {
		t.Fatalf("expected missing ask_id error, got %v", err)
	}
}

type fakeBaiduCheckinScheduler struct {
	job     func()
	stopped bool
}

func (f *fakeBaiduCheckinScheduler) Do(job func()) { f.job = job }
func (f *fakeBaiduCheckinScheduler) Stop()         { f.stopped = true }

func TestStartBaiduCheckinEnabledReplacesScheduler(t *testing.T) {
	baiduCheckinSeamMu.Lock()
	originalFactory := newBaiduCheckinScheduler
	originalLaunch := launchBaiduCheckin
	t.Cleanup(func() {
		newBaiduCheckinScheduler = originalFactory
		launchBaiduCheckin = originalLaunch
		baiduCheckinSeamMu.Unlock()
	})

	var gotInterval time.Duration
	newScheduler := &fakeBaiduCheckinScheduler{}
	newBaiduCheckinScheduler = func(interval time.Duration) baiduCheckinScheduler {
		gotInterval = interval
		return newScheduler
	}
	launchCalls := 0
	launchBaiduCheckin = func(job func()) { launchCalls++ }

	oldScheduler := &fakeBaiduCheckinScheduler{}
	d := &BaiduNetdisk{
		Addition:         Addition{AutoCheckin: true},
		checkinScheduler: oldScheduler,
	}
	d.startCheckin()
	if !oldScheduler.stopped || launchCalls != 1 || gotInterval != 24*time.Hour {
		t.Fatalf("unexpected lifecycle: oldStopped=%v launches=%d interval=%v", oldScheduler.stopped, launchCalls, gotInterval)
	}
	if d.checkinScheduler != newScheduler || newScheduler.job == nil {
		t.Fatal("expected replacement scheduler with a job")
	}
	if err := d.Drop(context.Background()); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if !newScheduler.stopped || d.checkinScheduler != nil {
		t.Fatal("expected Drop to stop and clear scheduler")
	}
}

func TestStartBaiduCheckinDisabledDoesNothing(t *testing.T) {
	baiduCheckinSeamMu.Lock()
	originalFactory := newBaiduCheckinScheduler
	originalLaunch := launchBaiduCheckin
	t.Cleanup(func() {
		newBaiduCheckinScheduler = originalFactory
		launchBaiduCheckin = originalLaunch
		baiduCheckinSeamMu.Unlock()
	})

	factoryCalls, launchCalls := 0, 0
	newBaiduCheckinScheduler = func(interval time.Duration) baiduCheckinScheduler {
		factoryCalls++
		return &fakeBaiduCheckinScheduler{}
	}
	launchBaiduCheckin = func(job func()) { launchCalls++ }

	d := &BaiduNetdisk{Addition: Addition{AutoCheckin: false}}
	d.startCheckin()
	if d.checkinScheduler != nil || factoryCalls != 0 || launchCalls != 0 {
		t.Fatalf("disabled check-in started work: scheduler=%v factory=%d launch=%d", d.checkinScheduler, factoryCalls, launchCalls)
	}
}
