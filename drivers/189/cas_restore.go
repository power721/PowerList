package _189

import (
	"context"
	"fmt"
	"io"
	"strconv"
	stdpath "path"
	"strings"

	"github.com/OpenListTeam/OpenList/v4/internal/casfile"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

const cloud189CASSliceSize int64 = 10 * 1024 * 1024

func (d *Cloud189) shouldRestoreSourceFromCAS(name string) bool {
	return d.RestoreSourceFromCAS && strings.HasSuffix(strings.ToLower(name), ".cas")
}

func (d *Cloud189) shouldDeleteCASAfterRestore(name string) bool {
	return d.DeleteCASAfterRestore && strings.HasSuffix(strings.ToLower(name), ".cas")
}

func (d *Cloud189) resolveRestoreSourceName(casFileName string, info *casfile.Info) (string, error) {
	restoreName := info.Name
	if d.RestoreSourceUseCurrentName {
		trimmedName, ok := trimCASSuffix(casFileName)
		if !ok {
			return "", fmt.Errorf("restore from .cas failed: current file name %q does not end with .cas", casFileName)
		}
		restoreName = strings.TrimSpace(trimmedName)
		if restoreName == "" {
			return "", fmt.Errorf("restore from .cas failed: current .cas file name %q has an empty source file name", casFileName)
		}
		if !hasUsableExtension(restoreName) {
			if sourceExt := normalizedSourceExtension(info.Name); sourceExt != "" {
				restoreName += sourceExt
			}
		}
	}
	if strings.ContainsAny(restoreName, `/\`) {
		return "", fmt.Errorf("restore from .cas failed: source file name %q contains a path", restoreName)
	}
	return restoreName, nil
}

func trimCASSuffix(name string) (string, bool) {
	const suffix = ".cas"
	if !strings.HasSuffix(strings.ToLower(name), suffix) {
		return "", false
	}
	return name[:len(name)-len(suffix)], true
}

func hasUsableExtension(name string) bool {
	ext := stdpath.Ext(name)
	return ext != "" && ext != "."
}

func normalizedSourceExtension(name string) string {
	ext := stdpath.Ext(strings.TrimSpace(name))
	if ext == "" || ext == "." {
		return ""
	}
	return ext
}

func readCASRestoreInfo(file model.FileStreamer) (*casfile.Info, error) {
	cache, err := file.CacheFullAndWriter(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("cache .cas file: %w", err)
	}
	if _, err = cache.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek .cas file: %w", err)
	}
	data, err := io.ReadAll(cache)
	if err != nil {
		return nil, fmt.Errorf("read .cas file: %w", err)
	}
	if _, err = cache.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind .cas file: %w", err)
	}
	info, err := casfile.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse .cas file: %w", err)
	}
	return info, nil
}

func parseAutoRestoreExistingCASPaths(raw string) []string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	seen := make(map[string]struct{})
	paths := make([]string, 0)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		cleaned := utils.FixAndCleanPath(line)
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		paths = append(paths, cleaned)
	}
	return paths
}

func (d *Cloud189) restoreSourceFromCAS(ctx context.Context, dstDir model.Obj, file model.FileStreamer) (model.Obj, error) {
	info, err := readCASRestoreInfo(file)
	if err != nil {
		return nil, err
	}
	return d.restoreSourceFromCASInfo(ctx, dstDir, file.GetName(), info)
}

func (d *Cloud189) restoreSourceFromCASInfo(ctx context.Context, dstDir model.Obj, casFileName string, info *casfile.Info) (model.Obj, error) {
	restoreName, err := d.resolveRestoreSourceName(casFileName, info)
	if err != nil {
		return nil, err
	}

	sessionKey, err := d.getSessionKey()
	if err != nil {
		return nil, err
	}
	d.sessionKey = sessionKey

	res, err := d.uploadRequest("/person/initMultiUpload", map[string]string{
		"parentFolderId": dstDir.GetID(),
		"fileName":       encode(restoreName),
		"fileSize":       strconv.FormatInt(info.Size, 10),
		"sliceSize":      strconv.FormatInt(cloud189CASSliceSize, 10),
		"fileMd5":        info.MD5,
		"sliceMd5":       info.SliceMD5,
	}, nil)
	if err != nil {
		return nil, err
	}

	uploadFileID := utils.Json.Get(res, "data", "uploadFileId").ToString()
	if uploadFileID == "" {
		return nil, fmt.Errorf("restore from .cas failed: upload session for %q is missing uploadFileId", restoreName)
	}
	fileDataExists := utils.Json.Get(res, "data", "fileDataExists").ToInt() == 1
	if !fileDataExists {
		res, err = d.uploadRequest("/person/checkTransSecond", map[string]string{
			"fileMd5":      info.MD5,
			"sliceMd5":     info.SliceMD5,
			"uploadFileId": uploadFileID,
		}, nil)
		if err != nil {
			return nil, err
		}
		fileDataExists = utils.Json.Get(res, "data", "fileDataExists").ToInt() == 1
	}
	if !fileDataExists {
		return nil, fmt.Errorf("restore from .cas failed: source data for %q was not found in 189Cloud", restoreName)
	}

	_, err = d.uploadRequest("/person/commitMultiUploadFile", map[string]string{
		"uploadFileId": uploadFileID,
		"fileMd5":      info.MD5,
		"sliceMd5":     info.SliceMD5,
		"lazyCheck":    "1",
		"opertype":     "3",
	}, nil)
	if err != nil {
		return nil, err
	}

	if obj, err := d.findFileByName(dstDir.GetID(), restoreName); err == nil {
		return obj, nil
	}
	return &model.Object{
		Name: restoreName,
		Size: info.Size,
	}, nil
}
