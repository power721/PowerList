package _189pc

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/casfile"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/pkg/http_range"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

var linkSeamMu sync.Mutex

type stubFileStreamer struct {
	utils.Closers
	name string
}

func (s *stubFileStreamer) Read(_ []byte) (int, error) { return 0, errors.New("unexpected read") }
func (s *stubFileStreamer) GetSize() int64              { return 0 }
func (s *stubFileStreamer) GetName() string             { return s.name }
func (s *stubFileStreamer) ModTime() time.Time          { return time.Time{} }
func (s *stubFileStreamer) CreateTime() time.Time       { return time.Time{} }
func (s *stubFileStreamer) IsDir() bool                 { return false }
func (s *stubFileStreamer) GetHash() utils.HashInfo     { return utils.HashInfo{} }
func (s *stubFileStreamer) GetID() string               { return "" }
func (s *stubFileStreamer) GetPath() string             { return "" }
func (s *stubFileStreamer) GetMimetype() string         { return "" }
func (s *stubFileStreamer) NeedStore() bool             { return false }
func (s *stubFileStreamer) IsForceStreamUpload() bool   { return false }
func (s *stubFileStreamer) GetExist() model.Obj         { return nil }
func (s *stubFileStreamer) SetExist(model.Obj)          {}
func (s *stubFileStreamer) RangeRead(http_range.Range) (io.Reader, error) {
	return nil, errors.New("unexpected rangeread")
}
func (s *stubFileStreamer) CacheFullAndWriter(*model.UpdateProgress, io.Writer) (model.File, error) {
	return nil, errors.New("unexpected cache")
}
func (s *stubFileStreamer) GetFile() model.File { return nil }

func TestLinkTransferredShareFile_NonCASUsesDirectLinkSeam(t *testing.T) {
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

	link, err := driver.linkTransferredShareFile(context.Background(), nonCAS)
	if err != nil {
		t.Fatalf("link non-cas transfer: %v", err)
	}
	if link.URL != "https://example.com/direct" {
		t.Fatalf("expected direct link, got %q", link.URL)
	}
	if directCalls != 1 {
		t.Fatalf("expected direct link seam once, got %d", directCalls)
	}
}

func TestLinkTransferredShareFile_CASRestoresPayloadNameEvenWhenDriverUsesCurrentName(t *testing.T) {
	driver := &Cloud189PC{
		Addition:  Addition{RestoreSourceUseCurrentName: true},
		TempDirId: "temp-dir-id",
	}
	casObj := &Cloud189File{Name: "renamed.mkv.cas"}

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
		return &Cloud189File{ID: "restored-id", Name: "payload.mkv"}, nil
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

	link, err := driver.linkTransferredShareFile(context.Background(), casObj)
	if err != nil {
		t.Fatalf("link cas transfer: %v", err)
	}
	if link.URL != "https://example.com/payload.mkv" {
		t.Fatalf("expected restored payload link, got %q", link.URL)
	}
	if openCalls != 1 || readCalls != 1 || restoreCalls != 1 || linkCalls != 1 {
		t.Fatalf("expected open/read/restore/link once, got open=%d read=%d restore=%d link=%d", openCalls, readCalls, restoreCalls, linkCalls)
	}
}

func TestLinkTransferredShareFile_CASRestoreFailureReturnsErrorAndDoesNotFallback(t *testing.T) {
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

	link, err := driver.linkTransferredShareFile(context.Background(), casObj)
	if err == nil || err.Error() != "restore failed" {
		t.Fatalf("expected restore failed error, got %v", err)
	}
	if link != nil {
		t.Fatalf("expected nil link on restore failure, got %#v", link)
	}
	if linkCalls != 0 {
		t.Fatalf("expected no fallback link call, got %d", linkCalls)
	}
}
