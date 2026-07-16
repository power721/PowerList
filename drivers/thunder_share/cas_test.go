package thunder_share

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	_189pc "github.com/OpenListTeam/OpenList/v4/drivers/189pc"
	"github.com/OpenListTeam/OpenList/v4/internal/casfile"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/stream"
)

func TestReadSharedCASInfo_ParsesThunderLinkStream(t *testing.T) {
	origLink := resolveThunderShareCASSourceLink
	origOpen := openThunderShareCASStream
	resolveThunderShareCASSourceLink = func(ctx context.Context, d *ThunderShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		return &model.Link{URL: "https://example.com/movie.cas"}, nil
	}
	openThunderShareCASStream = func(ctx context.Context, file model.Obj, link *model.Link) (model.FileStreamer, error) {
		return &stream.FileStream{Ctx: ctx, Obj: file, Reader: strings.NewReader(`{"name":"payload.mkv","size":7,"md5":"abc","sliceMd5":"def"}`)}, nil
	}
	t.Cleanup(func() {
		resolveThunderShareCASSourceLink = origLink
		openThunderShareCASStream = origOpen
	})

	info, err := (&ThunderShare{}).readSharedCASInfo(context.Background(), &model.Object{ID: "cas-id", Name: "movie.cas", Size: 80}, model.LinkArgs{})
	if err != nil {
		t.Fatalf("read shared CAS info: %v", err)
	}
	if info.Name != "payload.mkv" || info.Size != 7 || info.MD5 != "abc" || info.SliceMD5 != "def" {
		t.Fatalf("unexpected info: %#v", info)
	}
}

func TestReadSharedCASInfo_PropagatesParseError(t *testing.T) {
	origLink := resolveThunderShareCASSourceLink
	origOpen := openThunderShareCASStream
	resolveThunderShareCASSourceLink = func(ctx context.Context, d *ThunderShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		return &model.Link{URL: "https://example.com/bad.cas"}, nil
	}
	openThunderShareCASStream = func(ctx context.Context, file model.Obj, link *model.Link) (model.FileStreamer, error) {
		return &stream.FileStream{Ctx: ctx, Obj: file, Reader: strings.NewReader("not-cas")}, nil
	}
	t.Cleanup(func() {
		resolveThunderShareCASSourceLink = origLink
		openThunderShareCASStream = origOpen
	})

	_, err := (&ThunderShare{}).readSharedCASInfo(context.Background(), &model.Object{Name: "bad.cas"}, model.LinkArgs{})
	if err == nil || errors.Is(err, casfile.ErrMetadataTooLarge) {
		t.Fatalf("expected malformed payload error, got %v", err)
	}
}

func TestReadSharedCASInfo_ReturnsMissingThunderAccountForNilLink(t *testing.T) {
	origLink := resolveThunderShareCASSourceLink
	origOpen := openThunderShareCASStream
	resolveThunderShareCASSourceLink = func(ctx context.Context, d *ThunderShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		return nil, nil
	}
	openCalls := 0
	openThunderShareCASStream = func(ctx context.Context, file model.Obj, link *model.Link) (model.FileStreamer, error) {
		openCalls++
		return nil, errors.New("stream must not open")
	}
	t.Cleanup(func() {
		resolveThunderShareCASSourceLink = origLink
		openThunderShareCASStream = origOpen
	})

	_, err := (&ThunderShare{}).readSharedCASInfo(context.Background(), &model.Object{Name: "movie.cas"}, model.LinkArgs{})
	if err == nil || err.Error() != "找不到迅雷云盘帐号" {
		t.Fatalf("expected missing Thunder account error, got %v", err)
	}
	if openCalls != 0 {
		t.Fatalf("expected no stream open for nil link, got %d", openCalls)
	}
}

func TestReadSharedCASInfo_RejectsOversizedMetadata(t *testing.T) {
	origLink := resolveThunderShareCASSourceLink
	origOpen := openThunderShareCASStream
	resolveThunderShareCASSourceLink = func(ctx context.Context, d *ThunderShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
		return &model.Link{URL: "https://example.com/large.cas"}, nil
	}
	openThunderShareCASStream = func(ctx context.Context, file model.Obj, link *model.Link) (model.FileStreamer, error) {
		return &stream.FileStream{Ctx: ctx, Obj: file, Reader: bytes.NewReader(bytes.Repeat([]byte("x"), casfile.MaxMetadataSize+1))}, nil
	}
	t.Cleanup(func() {
		resolveThunderShareCASSourceLink = origLink
		openThunderShareCASStream = origOpen
	})

	_, err := (&ThunderShare{}).readSharedCASInfo(context.Background(), &model.Object{Name: "large.cas"}, model.LinkArgs{})
	if !errors.Is(err, casfile.ErrMetadataTooLarge) {
		t.Fatalf("expected ErrMetadataTooLarge, got %v", err)
	}
}

func TestRestoreSharedCASInfo_TriesAccountsUntilSuccess(t *testing.T) {
	first := &_189pc.Cloud189PC{Storage: model.Storage{ID: 101}}
	second := &_189pc.Cloud189PC{Storage: model.Storage{ID: 202}}
	info := &casfile.Info{Name: "payload.mkv", Size: 7, MD5: "abc", SliceMD5: "def"}
	origCandidates := get189CloudPCCandidates
	origRestore := restore189CloudPCCAS
	get189CloudPCCandidates = func() []*_189pc.Cloud189PC { return []*_189pc.Cloud189PC{first, second} }
	var calls []uint
	restore189CloudPCCAS = func(ctx context.Context, cloud *_189pc.Cloud189PC, name string, got *casfile.Info) (*model.Link, error) {
		calls = append(calls, cloud.ID)
		if name != "movie.cas" || got != info {
			t.Fatalf("unexpected input name=%q info=%#v", name, got)
		}
		if cloud == first {
			return nil, errors.New("first failed")
		}
		return &model.Link{URL: "https://example.com/189/payload.mkv"}, nil
	}
	t.Cleanup(func() {
		get189CloudPCCandidates = origCandidates
		restore189CloudPCCAS = origRestore
	})

	link, err := restoreSharedCASInfo(context.Background(), "movie.cas", info)
	if err != nil {
		t.Fatalf("restore shared CAS: %v", err)
	}
	if link.URL != "https://example.com/189/payload.mkv" || !reflect.DeepEqual(calls, []uint{101, 202}) {
		t.Fatalf("unexpected link or calls: link=%#v calls=%v", link, calls)
	}
}

func TestDistinct189CloudPCCandidates_PrefersAndDeduplicates(t *testing.T) {
	preferred := &_189pc.Cloud189PC{Storage: model.Storage{ID: 202}}
	first := &_189pc.Cloud189PC{Storage: model.Storage{ID: 101}}
	duplicatePreferred := &_189pc.Cloud189PC{Storage: model.Storage{ID: 202}}
	third := &_189pc.Cloud189PC{Storage: model.Storage{ID: 303}}

	got := distinct189CloudPCCandidates(preferred, []*_189pc.Cloud189PC{first, duplicatePreferred, third})
	want := []*_189pc.Cloud189PC{preferred, first, third}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected candidates: got=%v want=%v", got, want)
	}
}

func TestRestoreSharedCASInfo_ReturnsMissingAccountError(t *testing.T) {
	origCandidates := get189CloudPCCandidates
	get189CloudPCCandidates = func() []*_189pc.Cloud189PC { return nil }
	t.Cleanup(func() { get189CloudPCCandidates = origCandidates })

	_, err := restoreSharedCASInfo(context.Background(), "movie.cas", &casfile.Info{Name: "payload.mkv"})
	if err == nil || err.Error() != "找不到天翼云盘帐号" {
		t.Fatalf("expected missing account error, got %v", err)
	}
}

func TestRestoreSharedCASInfo_JoinsAllFailures(t *testing.T) {
	firstErr := errors.New("first failed")
	secondErr := errors.New("second failed")
	first := &_189pc.Cloud189PC{Storage: model.Storage{ID: 101}}
	second := &_189pc.Cloud189PC{Storage: model.Storage{ID: 202}}
	origCandidates := get189CloudPCCandidates
	origRestore := restore189CloudPCCAS
	get189CloudPCCandidates = func() []*_189pc.Cloud189PC { return []*_189pc.Cloud189PC{first, second} }
	restore189CloudPCCAS = func(ctx context.Context, cloud *_189pc.Cloud189PC, name string, info *casfile.Info) (*model.Link, error) {
		if cloud == first {
			return nil, firstErr
		}
		return nil, secondErr
	}
	t.Cleanup(func() {
		get189CloudPCCandidates = origCandidates
		restore189CloudPCCAS = origRestore
	})

	_, err := restoreSharedCASInfo(context.Background(), "movie.cas", &casfile.Info{Name: "payload.mkv"})
	if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
		t.Fatalf("expected both causes, got %v", err)
	}
	for _, id := range []uint{101, 202} {
		if !strings.Contains(err.Error(), fmt.Sprintf("189CloudPC[%d]", id)) {
			t.Fatalf("expected account id %d in %q", id, err.Error())
		}
	}
}

func TestResolveCASPlayback_ReadsThenRestores(t *testing.T) {
	info := &casfile.Info{Name: "payload.mkv", Size: 7, MD5: "abc", SliceMD5: "def"}
	origRead := readThunderShareCASInfo
	origRestore := restoreThunderShareCASInfo
	readThunderShareCASInfo = func(ctx context.Context, d *ThunderShare, file model.Obj, args model.LinkArgs) (*casfile.Info, error) {
		return info, nil
	}
	restoreThunderShareCASInfo = func(ctx context.Context, name string, got *casfile.Info) (*model.Link, error) {
		if name != "movie.cas" || got != info {
			t.Fatalf("unexpected restore input name=%q info=%#v", name, got)
		}
		return &model.Link{URL: "https://example.com/189/payload.mkv"}, nil
	}
	t.Cleanup(func() {
		readThunderShareCASInfo = origRead
		restoreThunderShareCASInfo = origRestore
	})

	link, err := (&ThunderShare{}).resolveCASPlayback(context.Background(), &model.Object{Name: "movie.cas"}, model.LinkArgs{})
	if err != nil {
		t.Fatalf("resolve CAS playback: %v", err)
	}
	if link.URL != "https://example.com/189/payload.mkv" {
		t.Fatalf("unexpected link: %q", link.URL)
	}
}

func TestResolveCASPlayback_ParseFailureDoesNotRestore(t *testing.T) {
	parseErr := errors.New("invalid CAS")
	origRead := readThunderShareCASInfo
	origRestore := restoreThunderShareCASInfo
	readThunderShareCASInfo = func(ctx context.Context, d *ThunderShare, file model.Obj, args model.LinkArgs) (*casfile.Info, error) {
		return nil, parseErr
	}
	restoreCalls := 0
	restoreThunderShareCASInfo = func(ctx context.Context, name string, info *casfile.Info) (*model.Link, error) {
		restoreCalls++
		return nil, nil
	}
	t.Cleanup(func() {
		readThunderShareCASInfo = origRead
		restoreThunderShareCASInfo = origRestore
	})

	_, err := (&ThunderShare{}).resolveCASPlayback(context.Background(), &model.Object{Name: "bad.cas"}, model.LinkArgs{})
	if !errors.Is(err, parseErr) || restoreCalls != 0 {
		t.Fatalf("unexpected error or restore calls: err=%v calls=%d", err, restoreCalls)
	}
}
