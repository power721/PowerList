package _189

import (
	"bytes"
	"context"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/stream"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

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

func (d *Cloud189) shouldUploadCAS(name string) bool {
	return (d.GenerateCAS || d.GenerateCASAndDeleteSource) && !strings.HasSuffix(strings.ToLower(name), ".cas")
}

func (d *Cloud189) shouldDeleteSource() bool {
	return d.DeleteSource || d.GenerateCASAndDeleteSource
}

func (d *Cloud189) uploadCAS(ctx context.Context, dstDir model.Obj, info *casUploadInfo) (model.Obj, error) {
	if info == nil || !d.shouldUploadCAS(info.Name) {
		return nil, nil
	}

	content, err := utils.Json.Marshal(casPayload{
		Name:       info.Name,
		Size:       info.Size,
		MD5:        info.MD5,
		SliceMD5:   info.SliceMD5,
		CreateTime: strconv.FormatInt(time.Now().Unix(), 10),
	})
	if err != nil {
		return nil, err
	}
	content = []byte(base64.StdEncoding.EncodeToString(content))

	now := time.Now()
	casObj := &model.Object{
		Name:     info.Name + ".cas",
		Size:     int64(len(content)),
		Modified: now,
		Ctime:    now,
		HashInfo: utils.NewHashInfo(utils.MD5, utils.HashData(utils.MD5, content)),
	}
	casStream := &stream.FileStream{
		Ctx:      ctx,
		Obj:      casObj,
		Reader:   bytes.NewReader(content),
		Mimetype: "text/plain",
	}

	if _, err = d.newUpload(ctx, dstDir, casStream, func(float64) {}); err != nil {
		return nil, err
	}
	return casObj, nil
}

func (d *Cloud189) deleteSource(ctx context.Context, dstDir model.Obj, info *casUploadInfo) error {
	if info == nil || !d.shouldDeleteSource() || !d.shouldUploadCAS(info.Name) {
		return nil
	}
	srcObj, err := d.findFileByName(dstDir.GetID(), info.Name)
	if err != nil {
		return err
	}
	return d.Remove(ctx, srcObj)
}

func (d *Cloud189) findFileByName(folderID, fileName string) (model.Obj, error) {
	files, err := d.getFiles(folderID)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if !file.IsDir() && file.GetName() == fileName {
			return file, nil
		}
	}
	return nil, errs.ObjectNotFound
}
