package thunder_share

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

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
