package _189pc

import (
	"bytes"
	"context"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

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

func (y *Cloud189PC) shouldUploadCAS(name string) bool {
	return (y.GenerateCAS || y.GenerateCASAndDeleteSource) && !strings.HasSuffix(strings.ToLower(name), ".cas")
}

func (y *Cloud189PC) shouldDeleteSource() bool {
	return y.DeleteSource || y.GenerateCASAndDeleteSource
}

func (y *Cloud189PC) uploadCAS(ctx context.Context, dstDir model.Obj, info *casUploadInfo) (model.Obj, error) {
	if info == nil || !y.shouldUploadCAS(info.Name) {
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

	uploadedCASObj, _, err := y.uploadFile(ctx, dstDir, casStream, func(float64) {})
	if err != nil {
		return nil, err
	}
	if uploadedCASObj != nil {
		return uploadedCASObj, nil
	}
	return casObj, nil
}

func (y *Cloud189PC) deleteSource(ctx context.Context, dstDir model.Obj, uploadedObj model.Obj, info *casUploadInfo) error {
	if info == nil || !y.shouldDeleteSource() || !y.shouldUploadCAS(info.Name) {
		return nil
	}
	if uploadedObj == nil {
		var err error
		uploadedObj, err = y.findFileByName(ctx, info.Name, dstDir.GetID(), y.isFamily())
		if err != nil {
			return err
		}
	}
	return y.Remove(ctx, uploadedObj)
}
