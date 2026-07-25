package quark

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/go-resty/resty/v2"
)

var quarkCheckinSeamMu sync.Mutex

func useQuarkCheckinServer(t *testing.T, handler http.Handler) {
	t.Helper()
	quarkCheckinSeamMu.Lock()
	server := httptest.NewServer(handler)
	originalURL := quarkGrowthBaseURL
	originalClient := quarkGrowthClient
	quarkGrowthBaseURL = server.URL
	quarkGrowthClient = func() *resty.Client { return resty.New() }
	t.Cleanup(func() {
		quarkGrowthBaseURL = originalURL
		quarkGrowthClient = originalClient
		server.Close()
		quarkCheckinSeamMu.Unlock()
	})
}

func TestQuarkCheckinAlreadySignedSkipsSignRequest(t *testing.T) {
	var paths []string
	useQuarkCheckinServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path != "/capacity/growth/info" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if r.Header.Get("Cookie") != "cookie-secret" {
			t.Fatalf("unexpected cookie %q", r.Header.Get("Cookie"))
		}
		if r.URL.Query().Get("pr") != "ucpro" || r.URL.Query().Get("fr") != "pc" || !r.URL.Query().Has("uc_param_str") {
			t.Fatalf("unexpected query %v", r.URL.Query())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"data":{"cap_sign":{"sign_daily":true,"sign_daily_reward":10485760,"sign_progress":2,"sign_target":7}}}`)
	}))

	d := &QuarkOrUC{Addition: Addition{Cookie: "cookie-secret"}}
	result, err := d.checkin()
	if err != nil {
		t.Fatalf("check in: %v", err)
	}
	want := quarkCheckinResult{AlreadySigned: true, RewardMiB: 10, Progress: 2, Target: 7}
	if result != want {
		t.Fatalf("unexpected result: want=%+v got=%+v", want, result)
	}
	if !reflect.DeepEqual(paths, []string{"/capacity/growth/info"}) {
		t.Fatalf("expected no sign POST, paths=%v", paths)
	}
}

func TestQuarkCheckinUnsignedSubmitsCyclicSign(t *testing.T) {
	var signBody map[string]bool
	useQuarkCheckinServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/capacity/growth/info":
			_, _ = io.WriteString(w, `{"code":0,"data":{"cap_sign":{"sign_daily":false,"sign_progress":3,"sign_target":7}}}`)
		case "/capacity/growth/sign":
			if r.Method != http.MethodPost {
				t.Fatalf("expected POST, got %s", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&signBody); err != nil {
				t.Fatalf("decode sign body: %v", err)
			}
			_, _ = io.WriteString(w, `{"code":0,"data":{"sign_daily_reward":20971520}}`)
		default:
			http.NotFound(w, r)
		}
	}))

	d := &QuarkOrUC{Addition: Addition{Cookie: "cookie-secret"}}
	result, err := d.checkin()
	if err != nil {
		t.Fatalf("check in: %v", err)
	}
	want := quarkCheckinResult{RewardMiB: 20, Progress: 4, Target: 7}
	if result != want {
		t.Fatalf("unexpected result: want=%+v got=%+v", want, result)
	}
	if !signBody["sign_cyclic"] {
		t.Fatalf("expected sign_cyclic=true, body=%v", signBody)
	}
}

func TestQuarkCheckinErrorRedactsAndBoundsCookie(t *testing.T) {
	cookie := "cookie-secret"
	useQuarkCheckinServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"code":400,"message":"`+cookie+strings.Repeat("x", quarkCheckinMessageLimit+50)+`"}`)
	}))

	d := &QuarkOrUC{Addition: Addition{Cookie: cookie}}
	_, err := d.checkin()
	if err == nil {
		t.Fatal("expected check-in error")
	}
	if strings.Contains(err.Error(), cookie) {
		t.Fatalf("cookie leaked in error: %v", err)
	}
	if len(err.Error()) > quarkCheckinMessageLimit+80 {
		t.Fatalf("expected bounded error, length=%d", len(err.Error()))
	}
}

func TestQuarkCheckinMissingGrowthDataReturnsError(t *testing.T) {
	useQuarkCheckinServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	d := &QuarkOrUC{Addition: Addition{Cookie: "cookie-secret"}}
	if _, err := d.checkin(); err == nil || !strings.Contains(err.Error(), "cap_sign") {
		t.Fatalf("expected missing cap_sign error, got %v", err)
	}
}

type fakeQuarkCheckinScheduler struct {
	job     func()
	stopped bool
}

func (f *fakeQuarkCheckinScheduler) Do(job func()) { f.job = job }
func (f *fakeQuarkCheckinScheduler) Stop()         { f.stopped = true }

func TestStartCheckinEnabledQuarkReplacesScheduler(t *testing.T) {
	quarkCheckinSeamMu.Lock()
	originalFactory := newQuarkCheckinScheduler
	originalLaunch := launchQuarkCheckin
	var gotInterval time.Duration
	newScheduler := &fakeQuarkCheckinScheduler{}
	newQuarkCheckinScheduler = func(interval time.Duration) quarkCheckinScheduler {
		gotInterval = interval
		return newScheduler
	}
	launchCalls := 0
	launchQuarkCheckin = func(job func()) { launchCalls++ }
	t.Cleanup(func() {
		newQuarkCheckinScheduler = originalFactory
		launchQuarkCheckin = originalLaunch
		quarkCheckinSeamMu.Unlock()
	})

	oldScheduler := &fakeQuarkCheckinScheduler{}
	d := &QuarkOrUC{
		Addition:         Addition{AutoCheckin: true},
		config:           driver.Config{Name: "Quark"},
		checkinScheduler: oldScheduler,
	}
	d.startCheckin()
	if !oldScheduler.stopped || launchCalls != 1 || gotInterval != 24*time.Hour {
		t.Fatalf("unexpected lifecycle state: oldStopped=%v launches=%d interval=%v", oldScheduler.stopped, launchCalls, gotInterval)
	}
	if d.checkinScheduler != newScheduler || newScheduler.job == nil {
		t.Fatal("expected scheduled check-in job")
	}
	if err := d.Drop(context.Background()); err != nil {
		t.Fatalf("drop driver: %v", err)
	}
	if !newScheduler.stopped || d.checkinScheduler != nil {
		t.Fatal("expected Drop to stop and clear scheduler")
	}
}

func TestStartCheckinSkipsDisabledQuarkAndUC(t *testing.T) {
	quarkCheckinSeamMu.Lock()
	originalFactory := newQuarkCheckinScheduler
	originalLaunch := launchQuarkCheckin
	factoryCalls, launchCalls := 0, 0
	newQuarkCheckinScheduler = func(interval time.Duration) quarkCheckinScheduler {
		factoryCalls++
		return &fakeQuarkCheckinScheduler{}
	}
	launchQuarkCheckin = func(job func()) { launchCalls++ }
	t.Cleanup(func() {
		newQuarkCheckinScheduler = originalFactory
		launchQuarkCheckin = originalLaunch
		quarkCheckinSeamMu.Unlock()
	})

	cases := []QuarkOrUC{
		{Addition: Addition{AutoCheckin: false}, config: driver.Config{Name: "Quark"}},
		{Addition: Addition{AutoCheckin: true}, config: driver.Config{Name: "UC"}},
	}
	for i := range cases {
		cases[i].startCheckin()
		if cases[i].checkinScheduler != nil {
			t.Fatalf("case %d unexpectedly created scheduler", i)
		}
	}
	if factoryCalls != 0 || launchCalls != 0 {
		t.Fatalf("unexpected calls: factory=%d launch=%d", factoryCalls, launchCalls)
	}
}
