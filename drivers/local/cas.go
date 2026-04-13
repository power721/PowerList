package local

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"hash"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

const localCASSliceSize int64 = 10 * 1024 * 1024

type casUploadInfo struct {
	Name     string
	Size     int64
	MD5      string
	SliceMD5 string
}

type casPayload struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	MD5        string `json:"md5"`
	SliceMD5   string `json:"sliceMd5"`
	CreateTime string `json:"create_time"`
}

type casHasherWriter struct {
	fileMD5          hash.Hash
	sliceMD5         hash.Hash
	written          int64
	currentSliceSize int64
	sliceMD5Hexs     []string
}

func newCASHasherWriter() *casHasherWriter {
	return &casHasherWriter{
		fileMD5:  utils.MD5.NewFunc(),
		sliceMD5: utils.MD5.NewFunc(),
	}
}

func (w *casHasherWriter) Write(p []byte) (int, error) {
	total := len(p)
	for len(p) > 0 {
		remaining := localCASSliceSize - w.currentSliceSize
		n := len(p)
		if int64(n) > remaining {
			n = int(remaining)
		}
		chunk := p[:n]
		_, _ = w.fileMD5.Write(chunk)
		_, _ = w.sliceMD5.Write(chunk)
		w.written += int64(n)
		w.currentSliceSize += int64(n)
		p = p[n:]
		if w.currentSliceSize == localCASSliceSize {
			w.finishSlice()
		}
	}
	return total, nil
}

func (w *casHasherWriter) finishSlice() {
	if w.currentSliceSize == 0 {
		return
	}
	w.sliceMD5Hexs = append(w.sliceMD5Hexs, strings.ToUpper(hex.EncodeToString(w.sliceMD5.Sum(nil))))
	w.sliceMD5.Reset()
	w.currentSliceSize = 0
}

func (w *casHasherWriter) Info(name string) *casUploadInfo {
	if w.written > localCASSliceSize && w.currentSliceSize > 0 {
		w.finishSlice()
	}

	fileMD5Hex := hex.EncodeToString(w.fileMD5.Sum(nil))
	sliceMD5Hex := fileMD5Hex
	if w.written > localCASSliceSize {
		sliceMD5Hex = utils.GetMD5EncodeStr(strings.Join(w.sliceMD5Hexs, "\n"))
	}

	return &casUploadInfo{
		Name:     name,
		Size:     w.written,
		MD5:      fileMD5Hex,
		SliceMD5: sliceMD5Hex,
	}
}

func (d *Local) shouldUploadCAS(name string) bool {
	return (d.GenerateCAS || d.GenerateCASAndDeleteSource) && !strings.HasSuffix(strings.ToLower(name), ".cas")
}

func (d *Local) shouldDeleteSource() bool {
	return d.DeleteSource || d.GenerateCASAndDeleteSource
}

func (d *Local) uploadCAS(ctx context.Context, dstDir model.Obj, info *casUploadInfo) error {
	if info == nil || !d.shouldUploadCAS(info.Name) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	content, err := utils.Json.Marshal(casPayload{
		Name:       info.Name,
		Size:       info.Size,
		MD5:        info.MD5,
		SliceMD5:   info.SliceMD5,
		CreateTime: strconv.FormatInt(time.Now().Unix(), 10),
	})
	if err != nil {
		return err
	}
	content = []byte(base64.StdEncoding.EncodeToString(content))

	casPath := filepath.Join(dstDir.GetPath(), info.Name+".cas")
	if err = os.WriteFile(casPath, content, 0o666); err != nil {
		return err
	}
	d.updateDirSize(dstDir.GetPath())
	return nil
}

func (d *Local) deleteSource(ctx context.Context, fullPath string, info *casUploadInfo) error {
	if info == nil || !d.shouldDeleteSource() || !d.shouldUploadCAS(info.Name) {
		return nil
	}
	return d.Remove(ctx, &model.Object{
		Path: fullPath,
		Name: info.Name,
		Size: info.Size,
	})
}

func (d *Local) updateDirSize(dirPath string) {
	if d.directoryMap.Has(dirPath) {
		d.directoryMap.UpdateDirSize(dirPath)
		d.directoryMap.UpdateDirParents(dirPath)
	}
}
