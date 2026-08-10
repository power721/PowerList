package _189pc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/casfile"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/pkg/cron"
	"github.com/OpenListTeam/OpenList/v4/pkg/http_range"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
)

var linkSeamMu sync.Mutex
var checkinSeamMu sync.Mutex

func useCheckinMarketServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	checkinSeamMu.Lock()
	server := httptest.NewServer(handler)
	originalAPIURL := checkinAPIBaseURL
	originalMarketURL := checkinMarketBaseURL
	checkinAPIBaseURL = server.URL
	checkinMarketBaseURL = server.URL
	t.Cleanup(func() {
		checkinAPIBaseURL = originalAPIURL
		checkinMarketBaseURL = originalMarketURL
		server.Close()
		checkinSeamMu.Unlock()
	})
	return server
}

type stubFileStreamer struct {
	utils.Closers
	name string
}

func (s *stubFileStreamer) Read(_ []byte) (int, error) { return 0, errors.New("unexpected read") }
func (s *stubFileStreamer) GetSize() int64             { return 0 }
func (s *stubFileStreamer) GetDuration() int           { return 0 }
func (s *stubFileStreamer) GetName() string            { return s.name }
func (s *stubFileStreamer) ModTime() time.Time         { return time.Time{} }
func (s *stubFileStreamer) CreateTime() time.Time      { return time.Time{} }
func (s *stubFileStreamer) IsDir() bool                { return false }
func (s *stubFileStreamer) GetHash() utils.HashInfo    { return utils.HashInfo{} }
func (s *stubFileStreamer) GetID() string              { return "" }
func (s *stubFileStreamer) GetPath() string            { return "" }
func (s *stubFileStreamer) GetMimetype() string        { return "" }
func (s *stubFileStreamer) NeedStore() bool            { return false }
func (s *stubFileStreamer) IsForceStreamUpload() bool  { return false }
func (s *stubFileStreamer) GetExist() model.Obj        { return nil }
func (s *stubFileStreamer) SetExist(model.Obj)         {}
func (s *stubFileStreamer) RangeRead(http_range.Range) (io.Reader, error) {
	return nil, errors.New("unexpected rangeread")
}
func (s *stubFileStreamer) CacheFullAndWriter(*model.UpdateProgress, io.Writer) (model.File, error) {
	return nil, errors.New("unexpected cache")
}
func (s *stubFileStreamer) GetFile() model.File { return nil }

func TestScheduleDelayedCleanup_ZeroDelaySkipsRemove(t *testing.T) {
	driver := &Cloud189PC{Storage: model.Storage{ID: 189}}
	target := &Cloud189File{ID: "cleanup-id", Name: "payload.mkv"}

	removeCalls := 0

	linkSeamMu.Lock()
	origRemove := removeResolvedTempObj
	origDeleteDelay := getDeleteDelaySeconds
	removeResolvedTempObj = func(ctx context.Context, y *Cloud189PC, obj model.Obj) error {
		removeCalls++
		return nil
	}
	getDeleteDelaySeconds = func() int { return 0 }
	t.Cleanup(func() {
		removeResolvedTempObj = origRemove
		getDeleteDelaySeconds = origDeleteDelay
		linkSeamMu.Unlock()
	})

	driver.scheduleDelayedResolvedTempCleanup(context.Background(), target)

	if removeCalls != 0 {
		t.Fatalf("expected no cleanup when delete delay is zero, got %d", removeCalls)
	}
}

func TestResolveTransferredShareFile_NonCASUsesDirectLinkSeam(t *testing.T) {
	driver := &Cloud189PC{}
	nonCAS := &Cloud189File{Name: "movie.mkv"}

	directCalls := 0

	linkSeamMu.Lock()
	origLink := directLinkObj
	directLinkObj = func(ctx context.Context, y *Cloud189PC, obj model.Obj) (*model.Link, error) {
		directCalls++
		return &model.Link{URL: "https://example.com/direct"}, nil
	}
	t.Cleanup(func() {
		directLinkObj = origLink
		linkSeamMu.Unlock()
	})

	link, cleanupObj, err := driver.resolveTransferredShareFile(context.Background(), nonCAS)
	if err != nil {
		t.Fatalf("link non-cas transfer: %v", err)
	}
	if link.URL != "https://example.com/direct" {
		t.Fatalf("expected direct link, got %q", link.URL)
	}
	if cleanupObj != nonCAS {
		t.Fatalf("expected transferred object as cleanup target, got %#v", cleanupObj)
	}
	if directCalls != 1 {
		t.Fatalf("expected direct link seam once, got %d", directCalls)
	}
}

func TestResolveTransferredShareFile_CASRestoresPayloadNameEvenWhenDriverUsesCurrentName(t *testing.T) {
	driver := &Cloud189PC{
		Addition:  Addition{RestoreSourceUseCurrentName: true},
		TempDirId: "temp-dir-id",
	}
	casObj := &Cloud189File{Name: "renamed.mkv.cas"}
	restoredObj := &Cloud189File{ID: "restored-id", Name: "payload.mkv"}

	openCalls := 0
	readCalls := 0
	restoreCalls := 0
	linkCalls := 0

	linkSeamMu.Lock()
	origOpen := openTransferredCASStream
	origRead := readTransferredCASInfo
	origRestore := restoreTransferredCASFromInfo
	origLink := directLinkObj
	openTransferredCASStream = func(ctx context.Context, y *Cloud189PC, obj model.Obj) (model.FileStreamer, error) {
		openCalls++
		return &stubFileStreamer{name: obj.GetName()}, nil
	}
	readTransferredCASInfo = func(stream model.FileStreamer) (*casfile.Info, error) {
		readCalls++
		return &casfile.Info{Name: "payload.mkv", Size: 7, MD5: "abc", SliceMD5: "def"}, nil
	}
	restoreTransferredCASFromInfo = func(ctx context.Context, y *Cloud189PC, dstDir model.Obj, casFileName string, info *casfile.Info) (model.Obj, error) {
		restoreCalls++
		if y.RestoreSourceUseCurrentName {
			t.Fatalf("expected RestoreSourceUseCurrentName forced false, got true")
		}
		if dstDir.GetID() != driver.TempDirId {
			t.Fatalf("expected restore dst dir id %q, got %q", driver.TempDirId, dstDir.GetID())
		}
		if casFileName != casObj.GetName() {
			t.Fatalf("expected cas file name %q, got %q", casObj.GetName(), casFileName)
		}
		if info == nil || info.Name != "payload.mkv" {
			t.Fatalf("expected payload info, got %#v", info)
		}
		return restoredObj, nil
	}
	directLinkObj = func(ctx context.Context, y *Cloud189PC, obj model.Obj) (*model.Link, error) {
		linkCalls++
		return &model.Link{URL: "https://example.com/" + obj.GetName()}, nil
	}
	t.Cleanup(func() {
		openTransferredCASStream = origOpen
		readTransferredCASInfo = origRead
		restoreTransferredCASFromInfo = origRestore
		directLinkObj = origLink
		linkSeamMu.Unlock()
	})

	link, cleanupObj, err := driver.resolveTransferredShareFile(context.Background(), casObj)
	if err != nil {
		t.Fatalf("link cas transfer: %v", err)
	}
	if link.URL != "https://example.com/payload.mkv" {
		t.Fatalf("expected restored payload link, got %q", link.URL)
	}
	if cleanupObj != restoredObj {
		t.Fatalf("expected restored object as cleanup target, got %#v", cleanupObj)
	}
	if openCalls != 1 || readCalls != 1 || restoreCalls != 1 || linkCalls != 1 {
		t.Fatalf("expected open/read/restore/link once, got open=%d read=%d restore=%d link=%d", openCalls, readCalls, restoreCalls, linkCalls)
	}
}

func TestResolveTransferredShareFile_CASRestoreFailureReturnsErrorAndDoesNotFallback(t *testing.T) {
	driver := &Cloud189PC{TempDirId: "temp-dir-id"}
	casObj := &Cloud189File{Name: "movie.mkv.cas"}

	linkCalls := 0

	linkSeamMu.Lock()
	origOpen := openTransferredCASStream
	origRead := readTransferredCASInfo
	origRestore := restoreTransferredCASFromInfo
	origLink := directLinkObj
	openTransferredCASStream = func(ctx context.Context, y *Cloud189PC, obj model.Obj) (model.FileStreamer, error) {
		return &stubFileStreamer{name: obj.GetName()}, nil
	}
	readTransferredCASInfo = func(stream model.FileStreamer) (*casfile.Info, error) {
		return &casfile.Info{Name: "payload.mkv", Size: 7, MD5: "abc", SliceMD5: "def"}, nil
	}
	restoreTransferredCASFromInfo = func(ctx context.Context, y *Cloud189PC, dstDir model.Obj, casFileName string, info *casfile.Info) (model.Obj, error) {
		return nil, errors.New("restore failed")
	}
	directLinkObj = func(ctx context.Context, y *Cloud189PC, obj model.Obj) (*model.Link, error) {
		linkCalls++
		return &model.Link{URL: "https://example.com/" + obj.GetName()}, nil
	}
	t.Cleanup(func() {
		openTransferredCASStream = origOpen
		readTransferredCASInfo = origRead
		restoreTransferredCASFromInfo = origRestore
		directLinkObj = origLink
		linkSeamMu.Unlock()
	})

	link, cleanupObj, err := driver.resolveTransferredShareFile(context.Background(), casObj)
	if err == nil || err.Error() != "restore failed" {
		t.Fatalf("expected restore failed error, got %v", err)
	}
	if link != nil {
		t.Fatalf("expected nil link on restore failure, got %#v", link)
	}
	if cleanupObj != nil {
		t.Fatalf("expected nil cleanup target on restore failure, got %#v", cleanupObj)
	}
	if linkCalls != 0 {
		t.Fatalf("expected no fallback link call, got %d", linkCalls)
	}
}

func TestResolveExistingCASFile_RestoresThenLinks(t *testing.T) {
	driver := &Cloud189PC{TempDirId: "temp-dir-id"}
	casObj := &Cloud189File{Name: "movie.mkv.cas"}
	restoredObj := &Cloud189File{ID: "restored-id", Name: "payload.mkv"}

	openCalls := 0
	readCalls := 0
	restoreCalls := 0
	linkCalls := 0

	linkSeamMu.Lock()
	origOpen := openTransferredCASStream
	origRead := readTransferredCASInfo
	origFind := findResolvedCASFileByName
	origRestore := restoreTransferredCASFromInfo
	origLink := directLinkObj
	openTransferredCASStream = func(ctx context.Context, y *Cloud189PC, obj model.Obj) (model.FileStreamer, error) {
		openCalls++
		return &stubFileStreamer{name: obj.GetName()}, nil
	}
	readTransferredCASInfo = func(stream model.FileStreamer) (*casfile.Info, error) {
		readCalls++
		return &casfile.Info{Name: "payload.mkv", Size: 7, MD5: "abc", SliceMD5: "def"}, nil
	}
	findResolvedCASFileByName = func(ctx context.Context, y *Cloud189PC, name string, folderID string) (model.Obj, error) {
		return nil, errs.ObjectNotFound
	}
	restoreTransferredCASFromInfo = func(ctx context.Context, y *Cloud189PC, dstDir model.Obj, casFileName string, info *casfile.Info) (model.Obj, error) {
		restoreCalls++
		if y.RestoreSourceUseCurrentName {
			t.Fatal("expected RestoreSourceUseCurrentName forced false")
		}
		if dstDir.GetID() != driver.TempDirId {
			t.Fatalf("expected temp dir id %q, got %q", driver.TempDirId, dstDir.GetID())
		}
		return restoredObj, nil
	}
	directLinkObj = func(ctx context.Context, y *Cloud189PC, obj model.Obj) (*model.Link, error) {
		linkCalls++
		return &model.Link{URL: "https://example.com/" + obj.GetName()}, nil
	}
	t.Cleanup(func() {
		openTransferredCASStream = origOpen
		readTransferredCASInfo = origRead
		findResolvedCASFileByName = origFind
		restoreTransferredCASFromInfo = origRestore
		directLinkObj = origLink
		linkSeamMu.Unlock()
	})

	link, cleanupObj, err := driver.resolveExistingCASFile(context.Background(), casObj)
	if err != nil {
		t.Fatalf("resolve existing cas: %v", err)
	}
	if link.URL != "https://example.com/payload.mkv" {
		t.Fatalf("expected restored payload link, got %q", link.URL)
	}
	if cleanupObj != restoredObj {
		t.Fatalf("expected restored object as cleanup target, got %#v", cleanupObj)
	}
	if openCalls != 1 || readCalls != 1 || restoreCalls != 1 || linkCalls != 1 {
		t.Fatalf("unexpected call counts: open=%d read=%d restore=%d link=%d", openCalls, readCalls, restoreCalls, linkCalls)
	}
}

func TestResolveExistingCASFile_ReusesExistingPayloadFile(t *testing.T) {
	driver := &Cloud189PC{TempDirId: "temp-dir-id"}
	casObj := &Cloud189File{Name: "movie.mkv.cas"}
	existingObj := &Cloud189File{ID: "existing-id", Name: "payload.mkv"}

	linkSeamMu.Lock()
	origOpen := openTransferredCASStream
	origRead := readTransferredCASInfo
	origFind := findResolvedCASFileByName
	origRestore := restoreTransferredCASFromInfo
	origLink := directLinkObj
	openTransferredCASStream = func(ctx context.Context, y *Cloud189PC, obj model.Obj) (model.FileStreamer, error) {
		return &stubFileStreamer{name: obj.GetName()}, nil
	}
	readTransferredCASInfo = func(stream model.FileStreamer) (*casfile.Info, error) {
		return &casfile.Info{Name: "payload.mkv", Size: 7, MD5: "abc", SliceMD5: "def"}, nil
	}
	findResolvedCASFileByName = func(ctx context.Context, y *Cloud189PC, name string, folderID string) (model.Obj, error) {
		return existingObj, nil
	}
	restoreTransferredCASFromInfo = func(ctx context.Context, y *Cloud189PC, dstDir model.Obj, casFileName string, info *casfile.Info) (model.Obj, error) {
		t.Fatal("did not expect restore when payload file already exists")
		return nil, nil
	}
	directLinkObj = func(ctx context.Context, y *Cloud189PC, obj model.Obj) (*model.Link, error) {
		return &model.Link{URL: "https://example.com/" + obj.GetName()}, nil
	}
	t.Cleanup(func() {
		openTransferredCASStream = origOpen
		readTransferredCASInfo = origRead
		findResolvedCASFileByName = origFind
		restoreTransferredCASFromInfo = origRestore
		directLinkObj = origLink
		linkSeamMu.Unlock()
	})

	link, cleanupObj, err := driver.resolveExistingCASFile(context.Background(), casObj)
	if err != nil {
		t.Fatalf("resolve existing cas with reuse: %v", err)
	}
	if link.URL != "https://example.com/payload.mkv" {
		t.Fatalf("expected reused payload link, got %q", link.URL)
	}
	if cleanupObj != existingObj {
		t.Fatalf("expected existing object as cleanup target, got %#v", cleanupObj)
	}
}

func TestResolveExistingCASFile_RestoreFailureDoesNotFallback(t *testing.T) {
	driver := &Cloud189PC{TempDirId: "temp-dir-id"}
	casObj := &Cloud189File{Name: "movie.mkv.cas"}

	linkCalls := 0

	linkSeamMu.Lock()
	origOpen := openTransferredCASStream
	origRead := readTransferredCASInfo
	origFind := findResolvedCASFileByName
	origRestore := restoreTransferredCASFromInfo
	origLink := directLinkObj
	openTransferredCASStream = func(ctx context.Context, y *Cloud189PC, obj model.Obj) (model.FileStreamer, error) {
		return &stubFileStreamer{name: obj.GetName()}, nil
	}
	readTransferredCASInfo = func(stream model.FileStreamer) (*casfile.Info, error) {
		return &casfile.Info{Name: "payload.mkv", Size: 7, MD5: "abc", SliceMD5: "def"}, nil
	}
	findResolvedCASFileByName = func(ctx context.Context, y *Cloud189PC, name string, folderID string) (model.Obj, error) {
		return nil, errs.ObjectNotFound
	}
	restoreTransferredCASFromInfo = func(ctx context.Context, y *Cloud189PC, dstDir model.Obj, casFileName string, info *casfile.Info) (model.Obj, error) {
		return nil, errors.New("restore failed")
	}
	directLinkObj = func(ctx context.Context, y *Cloud189PC, obj model.Obj) (*model.Link, error) {
		linkCalls++
		return &model.Link{URL: "https://example.com/" + obj.GetName()}, nil
	}
	t.Cleanup(func() {
		openTransferredCASStream = origOpen
		readTransferredCASInfo = origRead
		findResolvedCASFileByName = origFind
		restoreTransferredCASFromInfo = origRestore
		directLinkObj = origLink
		linkSeamMu.Unlock()
	})

	link, cleanupObj, err := driver.resolveExistingCASFile(context.Background(), casObj)
	if err == nil || err.Error() != "restore failed" {
		t.Fatalf("expected restore failed error, got %v", err)
	}
	if link != nil || cleanupObj != nil {
		t.Fatalf("expected nil link and cleanup target on restore failure, got link=%#v cleanup=%#v", link, cleanupObj)
	}
	if linkCalls != 0 {
		t.Fatalf("expected no fallback direct link call, got %d", linkCalls)
	}
}

func TestRestoreCASForPlayback_RestoresLinksAndSchedulesDetachedCleanup(t *testing.T) {
	type contextKey string
	const key contextKey = "request-id"

	driver := &Cloud189PC{
		Storage:   model.Storage{ID: 189},
		Addition:  Addition{RestoreSourceUseCurrentName: true},
		TempDirId: "temp-dir-id",
	}
	info := &casfile.Info{Name: "payload.mkv", Size: 7, MD5: "abc", SliceMD5: "def"}
	restoredObj := &Cloud189File{ID: "restored-id", Name: "payload.mkv"}
	parent := context.WithValue(context.Background(), key, "req-1")
	parent, cancel := context.WithCancel(parent)
	cancel()

	linkSeamMu.Lock()
	origFind := findResolvedCASFileByName
	origRestore := restoreTransferredCASFromInfo
	origLink := directLinkObj
	origSchedule := scheduleResolvedTempCleanup
	findResolvedCASFileByName = func(ctx context.Context, y *Cloud189PC, name string, folderID string) (model.Obj, error) {
		if name != "payload.mkv" || folderID != "temp-dir-id" {
			t.Fatalf("unexpected lookup name=%q folder=%q", name, folderID)
		}
		return nil, errs.ObjectNotFound
	}
	restoreTransferredCASFromInfo = func(ctx context.Context, y *Cloud189PC, dstDir model.Obj, casFileName string, got *casfile.Info) (model.Obj, error) {
		if y.RestoreSourceUseCurrentName {
			t.Fatal("expected payload-name semantics")
		}
		if casFileName != "renamed.cas" || got != info {
			t.Fatalf("unexpected restore input name=%q info=%#v", casFileName, got)
		}
		return restoredObj, nil
	}
	directLinkObj = func(ctx context.Context, y *Cloud189PC, obj model.Obj) (*model.Link, error) {
		return &model.Link{URL: "https://example.com/payload.mkv"}, nil
	}
	scheduled := false
	scheduleResolvedTempCleanup = func(ctx context.Context, y *Cloud189PC, obj model.Obj) {
		scheduled = true
		if err := ctx.Err(); err != nil {
			t.Fatalf("cleanup context remained canceled: %v", err)
		}
		if got := ctx.Value(key); got != "req-1" {
			t.Fatalf("expected request value, got %v", got)
		}
		if obj != restoredObj {
			t.Fatalf("unexpected cleanup target: %#v", obj)
		}
	}
	t.Cleanup(func() {
		findResolvedCASFileByName = origFind
		restoreTransferredCASFromInfo = origRestore
		directLinkObj = origLink
		scheduleResolvedTempCleanup = origSchedule
		linkSeamMu.Unlock()
	})

	link, err := driver.RestoreCASForPlayback(parent, "renamed.cas", info)
	if err != nil {
		t.Fatalf("restore CAS for playback: %v", err)
	}
	if link.URL != "https://example.com/payload.mkv" || !scheduled {
		t.Fatalf("unexpected link or cleanup state: link=%#v scheduled=%v", link, scheduled)
	}
}

func TestCloneDriverForCASRestore_ClonesCurrentFieldsAndResetsAutoRestoreState(t *testing.T) {
	cleanupCalled := 0
	cleanup := func() {
		cleanupCalled++
	}
	source := &Cloud189PC{
		Storage: model.Storage{
			ID:              189,
			MountPath:       "/189pc",
			CacheExpiration: 123,
			Remark:          "test-storage",
		},
		Addition: Addition{
			Username:                    "user",
			Password:                    "pass",
			Type:                        "family",
			FamilyID:                    "family-id",
			RestoreSourceUseCurrentName: true,
			AutoRestoreExistingCAS:      true,
		},
		identity:                "identity",
		client:                  resty.New(),
		loginParam:              &LoginParam{RsaUsername: "rsa-user", RsaPassword: "rsa-pass"},
		qrcodeParam:             &QRLoginParam{UUID: "uuid"},
		tokenInfo:               &AppSessionResp{AccessToken: "access", RefreshToken: "refresh"},
		uploadThread:            9,
		familyTransferFolder:    &Cloud189Folder{Name: "family-temp"},
		cleanFamilyTransferFile: cleanup,
		storageConfig: driver.Config{
			Name:              "189CloudPC",
			DefaultRoot:       "-11",
			OnlyProxy:         true,
			NoOverwriteUpload: true,
		},
		TempDirId: "temp-dir-id",
		cron:      cron.NewCron(time.Minute),
		client2: resty.New().SetRedirectPolicy(resty.RedirectPolicyFunc(func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		})),
	}
	source.ref = source
	source.autoRestoreInFlight.Store("busy.cas", struct{}{})

	cloned := cloneDriverForCASRestore(source)
	if cloned == source {
		t.Fatal("expected clone to allocate a distinct driver")
	}
	if !reflect.DeepEqual(source.Storage, cloned.Storage) {
		t.Fatalf("expected storage copied, got %#v", cloned.Storage)
	}
	if !reflect.DeepEqual(source.Addition, cloned.Addition) {
		t.Fatalf("expected addition copied, got %#v", cloned.Addition)
	}
	if cloned.identity != source.identity {
		t.Fatalf("expected identity %q, got %q", source.identity, cloned.identity)
	}
	if cloned.client != source.client {
		t.Fatalf("expected client pointer copied")
	}
	if cloned.loginParam != source.loginParam {
		t.Fatalf("expected loginParam pointer copied")
	}
	if cloned.qrcodeParam != source.qrcodeParam {
		t.Fatalf("expected qrcodeParam pointer copied")
	}
	if cloned.tokenInfo != source.tokenInfo {
		t.Fatalf("expected tokenInfo pointer copied")
	}
	if cloned.uploadThread != source.uploadThread {
		t.Fatalf("expected uploadThread %d, got %d", source.uploadThread, cloned.uploadThread)
	}
	if cloned.familyTransferFolder != source.familyTransferFolder {
		t.Fatalf("expected familyTransferFolder pointer copied")
	}
	if cloned.cleanFamilyTransferFile == nil {
		t.Fatal("expected cleanFamilyTransferFile copied")
	}
	cloned.cleanFamilyTransferFile()
	if cleanupCalled != 1 {
		t.Fatalf("expected cloned cleanup func to call original closure once, got %d", cleanupCalled)
	}
	if !reflect.DeepEqual(source.storageConfig, cloned.storageConfig) {
		t.Fatalf("expected storageConfig copied, got %#v", cloned.storageConfig)
	}
	if cloned.ref != source.ref {
		t.Fatalf("expected ref pointer copied")
	}
	if cloned.TempDirId != source.TempDirId {
		t.Fatalf("expected TempDirId %q, got %q", source.TempDirId, cloned.TempDirId)
	}
	if cloned.cron != source.cron {
		t.Fatalf("expected cron pointer copied")
	}
	if cloned.client2 != source.client2 {
		t.Fatalf("expected client2 pointer copied")
	}
	if !cloned.beginAutoRestore("busy.cas") {
		t.Fatal("expected clone autoRestoreInFlight to start empty")
	}
	if source.beginAutoRestore("busy.cas") {
		t.Fatal("expected source autoRestoreInFlight to keep existing state")
	}
}

func TestCollectTransferredShareCandidates_PreservesExactMatchAndDiagnostics(t *testing.T) {
	files := []model.Obj{
		&Cloud189File{ID: "old-id", Name: "movie.mkv.cas"},
		&Cloud189File{ID: "other-id", Name: "other-file.cas"},
		&Cloud189File{ID: "new-id", Name: "movie.mkv.cas"},
	}

	matched, candidates := collectTransferredShareCandidates(files, "movie.mkv.cas")

	if matched == nil {
		t.Fatal("expected a matching transferred share object")
	}
	if matched.GetID() != "old-id" {
		t.Fatalf("expected first exact match id old-id, got %q", matched.GetID())
	}
	wantCandidates := []string{
		"movie.mkv.cas(old-id)",
		"other-file.cas(other-id)",
		"movie.mkv.cas(new-id)",
	}
	if !reflect.DeepEqual(candidates, wantCandidates) {
		t.Fatalf("unexpected candidates:\nwant: %#v\ngot:  %#v", wantCandidates, candidates)
	}
}

func TestClaimVIPSpaceSendsRequiredParameters(t *testing.T) {
	var got url.Values
	useCheckinMarketServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/market/drawTargetSpace.action" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":"0","message":"成功"}`)
	}))

	driver := &Cloud189PC{
		Storage:   model.Storage{ID: 189},
		client:    resty.New(),
		tokenInfo: &AppSessionResp{UserSessionResp: UserSessionResp{SessionKey: "session-key"}},
	}

	if err := driver.claimVIPSpace(); err != nil {
		t.Fatalf("claim VIP space: %v", err)
	}
	if got.Get("activityId") != vipActivityID || got.Get("prizeId") != vipPrizeID {
		t.Fatalf("unexpected VIP parameters: %v", got)
	}
	if got.Get("sessionKey") != "session-key" {
		t.Fatalf("expected session key, got %q", got.Get("sessionKey"))
	}
	if got.Get("noCache") == "" {
		t.Fatal("expected cache-busting timestamp")
	}
}

func TestClaimVIPSpaceTreatsAlreadyClaimedAsComplete(t *testing.T) {
	useCheckinMarketServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":"1","message":"本月已领取"}`)
	}))
	driver := &Cloud189PC{
		client:    resty.New(),
		tokenInfo: &AppSessionResp{UserSessionResp: UserSessionResp{SessionKey: "session-key"}},
	}
	if err := driver.claimVIPSpace(); err != nil {
		t.Fatalf("already claimed VIP space should be complete: %v", err)
	}
}

func TestSafeActivityMessageRedactsSecretsAndBoundsOutput(t *testing.T) {
	secret := "access-token-secret"
	got := safeActivityMessage("request echoed "+secret, secret)
	if got != "request echoed [redacted]" {
		t.Fatalf("unexpected redaction result %q", got)
	}

	got = safeActivityMessage(strings.Repeat("x", maxActivityMessage+20), secret)
	if len(got) != maxActivityMessage+3 || !strings.HasSuffix(got, "...") {
		t.Fatalf("expected bounded activity message, got length %d and value %q", len(got), got)
	}
}

func TestGreenDailyCheckinUsesMergedSSOCookie(t *testing.T) {
	var paths []string
	useCheckinMarketServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/ssoLoginMerge.action":
			if r.URL.Query().Get("sessionKey") != "session-key" ||
				r.URL.Query().Get("sessionKeyFm") != "family-key" ||
				r.URL.Query().Get("eAccessToken") != "access-token" {
				t.Fatalf("unexpected SSO query: %v", r.URL.Query())
			}
			http.SetCookie(w, &http.Cookie{Name: "green-session", Value: "ready", Path: "/"})
			_, _ = io.WriteString(w, "ok")
		case "/market/signInNew.action":
			cookie, err := r.Cookie("green-session")
			if err != nil || cookie.Value != "ready" {
				t.Fatalf("green sign-in did not retain SSO cookie: cookie=%v err=%v", cookie, err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"result":true}`)
		case "/market/signInNewInfo.action":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":3}`)
		default:
			http.NotFound(w, r)
		}
	}))

	driver := &Cloud189PC{
		Storage: model.Storage{ID: 189},
		client:  resty.New(),
		tokenInfo: &AppSessionResp{
			AccessToken: "access-token",
			UserSessionResp: UserSessionResp{
				SessionKey:       "session-key",
				FamilySessionKey: "family-key",
			},
		},
	}

	client, err := driver.newGreenActivityClient()
	if err != nil {
		t.Fatalf("establish green SSO: %v", err)
	}
	if err := driver.greenDailyCheckin(client); err != nil {
		t.Fatalf("green daily check-in: %v", err)
	}
	want := []string{"/ssoLoginMerge.action", "/market/signInNew.action", "/market/signInNewInfo.action"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("unexpected request sequence: want=%v got=%v", want, paths)
	}
}

func TestRunGreenTasksExecutesOnlyIncompleteTasksInAllCategories(t *testing.T) {
	var listedTypes []string
	var executed []string
	useCheckinMarketServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/market/getGreenTaskList.action":
			taskType := r.URL.Query().Get("taskType")
			listedTypes = append(listedTypes, taskType)
			_, _ = io.WriteString(w, fmt.Sprintf(
				`{"data":[{"taskId":"done-%s","taskName":"done","status":true},{"taskId":"todo-%s","taskName":"todo","status":false}]}`,
				taskType, taskType,
			))
		case "/market/doGreenTask.action":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse task form: %v", err)
			}
			executed = append(executed, r.Form.Get("taskId"))
			_, _ = io.WriteString(w, `{"data":true}`)
		case "/market/getGreenLevelList.action":
			_, _ = io.WriteString(w, `{"data":{"userScore":42}}`)
		default:
			http.NotFound(w, r)
		}
	}))

	driver := &Cloud189PC{
		Storage:   model.Storage{ID: 189},
		tokenInfo: &AppSessionResp{UserSessionResp: UserSessionResp{SessionKey: "session-key"}},
	}
	client := resty.New()
	for _, taskType := range []string{"1", "2", "3"} {
		if err := driver.runGreenTasks(client, taskType); err != nil {
			t.Fatalf("run green task type %s: %v", taskType, err)
		}
	}
	if err := driver.queryGreenScore(client); err != nil {
		t.Fatalf("query green score: %v", err)
	}
	if !reflect.DeepEqual(listedTypes, []string{"1", "2", "3"}) {
		t.Fatalf("unexpected listed task types: %v", listedTypes)
	}
	if !reflect.DeepEqual(executed, []string{"todo-1", "todo-2", "todo-3"}) {
		t.Fatalf("expected only incomplete tasks, got %v", executed)
	}
}

func TestRunGreenTasksContinuesAfterOneTaskSubmissionFails(t *testing.T) {
	var executed []string
	useCheckinMarketServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/market/getGreenTaskList.action":
			_, _ = io.WriteString(w, `{"data":[{"taskId":"first","taskName":"first","status":false},{"taskId":"second","taskName":"second","status":false}]}`)
		case "/market/doGreenTask.action":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse task form: %v", err)
			}
			executed = append(executed, r.Form.Get("taskId"))
			if r.Form.Get("taskId") == "first" {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			_, _ = io.WriteString(w, `{"data":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	driver := &Cloud189PC{
		tokenInfo: &AppSessionResp{UserSessionResp: UserSessionResp{SessionKey: "session-key"}},
	}
	if err := driver.runGreenTasks(resty.New(), "1"); err == nil {
		t.Fatal("expected the failed task submission to be reported")
	}
	if !reflect.DeepEqual(executed, []string{"first", "second"}) {
		t.Fatalf("expected later task to run after failure, got %v", executed)
	}
}

func TestCheckinContinuesToGreenScoreAfterEarlierFailures(t *testing.T) {
	var requested []string
	var listedTaskTypes []string
	useCheckinMarketServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/mkt/userSign.action":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"code":"FAILED","message":"ordinary failed"}`)
		case "/v2/drawPrizeMarketDetails.action":
			_, _ = io.WriteString(w, `{}`)
		case "/market/drawTargetSpace.action":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, `{}`)
		case "/ssoLoginMerge.action":
			http.SetCookie(w, &http.Cookie{Name: "green-session", Value: "ready", Path: "/"})
			_, _ = io.WriteString(w, "ok")
		case "/market/signInNew.action":
			_, _ = io.WriteString(w, `{"result":false,"message":"temporary failure"}`)
		case "/market/signInNewInfo.action":
			_, _ = io.WriteString(w, `{"data":2}`)
		case "/market/getGreenTaskList.action":
			taskType := r.URL.Query().Get("taskType")
			listedTaskTypes = append(listedTaskTypes, taskType)
			if taskType == "1" {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			_, _ = io.WriteString(w, `{"data":[]}`)
		case "/market/getGreenLevelList.action":
			_, _ = io.WriteString(w, `{"data":{"userScore":99}}`)
		default:
			http.NotFound(w, r)
		}
	}))

	driver := &Cloud189PC{
		Storage: model.Storage{ID: 189},
		client:  resty.New(),
		tokenInfo: &AppSessionResp{
			AccessToken: "access-token",
			UserSessionResp: UserSessionResp{
				SessionKey:       "session-key",
				SessionSecret:    "0123456789abcdef",
				FamilySessionKey: "family-key",
			},
		},
	}
	driver.checkin()

	foundScore := false
	for _, path := range requested {
		if path == "/market/getGreenLevelList.action" {
			foundScore = true
			break
		}
	}
	if !foundScore {
		t.Fatalf("expected score lookup after earlier failures, requests=%v", requested)
	}
	if !reflect.DeepEqual(listedTaskTypes, []string{"1", "2", "3"}) {
		t.Fatalf("expected all task categories, got %v", listedTaskTypes)
	}
}

func TestRunGreenActivityStopsAfterSSOFailure(t *testing.T) {
	var requested []string
	useCheckinMarketServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		w.WriteHeader(http.StatusBadGateway)
	}))
	driver := &Cloud189PC{
		client: resty.New(),
		tokenInfo: &AppSessionResp{
			AccessToken: "access-token",
			UserSessionResp: UserSessionResp{
				SessionKey:       "session-key",
				FamilySessionKey: "family-key",
			},
		},
	}
	if err := driver.runGreenActivity(); err == nil {
		t.Fatal("expected green activity to fail when SSO fails")
	}
	if !reflect.DeepEqual(requested, []string{"/ssoLoginMerge.action"}) {
		t.Fatalf("expected green workflow to stop after SSO, requests=%v", requested)
	}
}

func TestGreenActivityLogsRedactAllSessionTokens(t *testing.T) {
	const accessToken = "access-token-secret"
	useCheckinMarketServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/market/signInNew.action":
			_, _ = io.WriteString(w, `{"result":true}`)
		case "/market/signInNewInfo.action":
			_, _ = io.WriteString(w, `{"data":"access-token-secret"}`)
		case "/market/getGreenTaskList.action":
			_, _ = io.WriteString(w, `{"data":[{"taskId":"task","taskName":"access-token-secret","status":false}]}`)
		case "/market/doGreenTask.action":
			_, _ = io.WriteString(w, `{"data":true}`)
		case "/market/getGreenLevelList.action":
			_, _ = io.WriteString(w, `{"data":{"userScore":"access-token-secret"}}`)
		default:
			http.NotFound(w, r)
		}
	}))

	driver := &Cloud189PC{
		Storage: model.Storage{ID: 189},
		tokenInfo: &AppSessionResp{
			AccessToken: accessToken,
			UserSessionResp: UserSessionResp{
				SessionKey:       "session-key-secret",
				FamilySessionKey: "family-key-secret",
			},
		},
	}
	var output bytes.Buffer
	logger := log.StandardLogger()
	originalOutput := logger.Out
	logger.SetOutput(&output)
	t.Cleanup(func() {
		logger.SetOutput(originalOutput)
	})

	client := resty.New()
	if err := driver.greenDailyCheckin(client); err != nil {
		t.Fatalf("green daily check-in: %v", err)
	}
	if err := driver.runGreenTasks(client, "1"); err != nil {
		t.Fatalf("run green tasks: %v", err)
	}
	if err := driver.queryGreenScore(client); err != nil {
		t.Fatalf("query green score: %v", err)
	}
	if strings.Contains(output.String(), accessToken) {
		t.Fatalf("access token leaked into logs: %s", output.String())
	}
}
