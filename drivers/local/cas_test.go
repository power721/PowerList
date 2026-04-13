package local

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/stream"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

func TestLocalPutGenerateCAS(t *testing.T) {
	root := t.TempDir()
	drv := &Local{
		Addition: Addition{
			RootPath:    driver.RootPath{RootFolderPath: root},
			GenerateCAS: true,
		},
	}
	if err := drv.Init(context.Background()); err != nil {
		t.Fatalf("init local driver: %v", err)
	}

	data := []byte("hello openlist cas")
	modTime := time.Unix(1710000000, 0)
	file := &stream.FileStream{
		Ctx: context.Background(),
		Obj: &model.Object{
			Name:     "hello.txt",
			Size:     int64(len(data)),
			Modified: modTime,
			Ctime:    modTime,
		},
		Reader: bytes.NewReader(data),
	}
	dstDir := &model.Object{
		Path:     root,
		Name:     filepath.Base(root),
		IsFolder: true,
	}

	if err := drv.Put(context.Background(), dstDir, file, func(float64) {}); err != nil {
		t.Fatalf("put file: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "hello.txt")); err != nil {
		t.Fatalf("stat source file: %v", err)
	}

	casContent, err := os.ReadFile(filepath.Join(root, "hello.txt.cas"))
	if err != nil {
		t.Fatalf("read cas file: %v", err)
	}
	rawPayload, err := base64.StdEncoding.DecodeString(string(casContent))
	if err != nil {
		t.Fatalf("decode cas file: %v", err)
	}

	var payload casPayload
	if err = utils.Json.Unmarshal(rawPayload, &payload); err != nil {
		t.Fatalf("unmarshal cas payload: %v", err)
	}

	if payload.Name != "hello.txt" {
		t.Fatalf("unexpected cas name: %s", payload.Name)
	}
	if payload.Size != int64(len(data)) {
		t.Fatalf("unexpected cas size: %d", payload.Size)
	}

	expectMD5 := utils.HashData(utils.MD5, data)
	if payload.MD5 != expectMD5 {
		t.Fatalf("unexpected cas md5: %s", payload.MD5)
	}
	if payload.SliceMD5 != expectMD5 {
		t.Fatalf("unexpected cas slice md5: %s", payload.SliceMD5)
	}
	if _, err = strconv.ParseInt(payload.CreateTime, 10, 64); err != nil {
		t.Fatalf("unexpected cas create time: %v", err)
	}
}

func TestLocalPutGenerateCASAndDeleteSource(t *testing.T) {
	root := t.TempDir()
	drv := &Local{
		Addition: Addition{
			RootPath:     driver.RootPath{RootFolderPath: root},
			GenerateCAS:  true,
			DeleteSource: true,
		},
	}
	if err := drv.Init(context.Background()); err != nil {
		t.Fatalf("init local driver: %v", err)
	}

	data := []byte("hello openlist cas delete source")
	file := &stream.FileStream{
		Ctx: context.Background(),
		Obj: &model.Object{
			Name:     "delete.txt",
			Size:     int64(len(data)),
			Modified: time.Unix(1710000000, 0),
		},
		Reader: bytes.NewReader(data),
	}
	dstDir := &model.Object{
		Path:     root,
		Name:     filepath.Base(root),
		IsFolder: true,
	}

	if err := drv.Put(context.Background(), dstDir, file, func(float64) {}); err != nil {
		t.Fatalf("put file: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "delete.txt")); !os.IsNotExist(err) {
		t.Fatalf("source file should be removed, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "delete.txt.cas")); err != nil {
		t.Fatalf("cas file should exist: %v", err)
	}
}
