package _189pc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/casfile"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/pkg/cron"
	"github.com/OpenListTeam/OpenList/v4/pkg/http_range"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/go-resty/resty/v2"
)

var linkSeamMu sync.Mutex

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

func TestResolveTransferredShareFile_NonCASUsesDirectLinkSeam(t *testing.T) {
	driver := &Cloud189PC{}
	nonCAS := &Cloud189File{Name: "movie.mkv"}

	directCalls := 0

	linkSeamMu.Lock()
	origLink := linkTransferObj
	linkTransferObj = func(ctx context.Context, y *Cloud189PC, obj model.Obj) (*model.Link, error) {
		directCalls++
		return &model.Link{URL: "https://example.com/direct"}, nil
	}
	t.Cleanup(func() {
		linkTransferObj = origLink
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
	origLink := linkTransferObj
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
	linkTransferObj = func(ctx context.Context, y *Cloud189PC, obj model.Obj) (*model.Link, error) {
		linkCalls++
		return &model.Link{URL: "https://example.com/" + obj.GetName()}, nil
	}
	t.Cleanup(func() {
		openTransferredCASStream = origOpen
		readTransferredCASInfo = origRead
		restoreTransferredCASFromInfo = origRestore
		linkTransferObj = origLink
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
	origLink := linkTransferObj
	openTransferredCASStream = func(ctx context.Context, y *Cloud189PC, obj model.Obj) (model.FileStreamer, error) {
		return &stubFileStreamer{name: obj.GetName()}, nil
	}
	readTransferredCASInfo = func(stream model.FileStreamer) (*casfile.Info, error) {
		return &casfile.Info{Name: "payload.mkv", Size: 7, MD5: "abc", SliceMD5: "def"}, nil
	}
	restoreTransferredCASFromInfo = func(ctx context.Context, y *Cloud189PC, dstDir model.Obj, casFileName string, info *casfile.Info) (model.Obj, error) {
		return nil, errors.New("restore failed")
	}
	linkTransferObj = func(ctx context.Context, y *Cloud189PC, obj model.Obj) (*model.Link, error) {
		linkCalls++
		return &model.Link{URL: "https://example.com/" + obj.GetName()}, nil
	}
	t.Cleanup(func() {
		openTransferredCASStream = origOpen
		readTransferredCASInfo = origRead
		restoreTransferredCASFromInfo = origRestore
		linkTransferObj = origLink
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
