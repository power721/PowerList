package _189pc

import (
	"context"
	"errors"
	stdpath "path"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/tchap/go-patricia/v2/patricia"
)

var autoRestoreTrie = patricia.NewTrie()

func insertAutoRestoreWatcher(y *Cloud189PC, paths []string) error {
	mountPath := utils.GetActualMountPath(y.GetStorage().MountPath)
	for _, path := range paths {
		prefix := patricia.Prefix(autoRestoreWatcherPath(mountPath, path))
		existing := autoRestoreTrie.Get(prefix)
		if existing == nil {
			if !autoRestoreTrie.Insert(prefix, []*Cloud189PC{y}) {
				return errors.New("failed to insert auto restore watcher")
			}
			continue
		}
		drivers, ok := existing.([]*Cloud189PC)
		if !ok {
			continue
		}
		for _, driver := range drivers {
			if driver == y {
				goto next
			}
		}
		autoRestoreTrie.Set(prefix, append(drivers, y))
	next:
	}
	return nil
}

func removeAutoRestoreWatcher(y *Cloud189PC) {
	mountPath := utils.GetActualMountPath(y.GetStorage().MountPath)
	for _, path := range y.autoRestoreExistingCASPaths() {
		prefix := patricia.Prefix(autoRestoreWatcherPath(mountPath, path))
		existing := autoRestoreTrie.Get(prefix)
		if existing == nil {
			continue
		}
		drivers, ok := existing.([]*Cloud189PC)
		if !ok {
			continue
		}
		if len(drivers) == 1 && drivers[0] == y {
			autoRestoreTrie.Delete(prefix)
			continue
		}
		filtered := drivers[:0]
		for _, driver := range drivers {
			if driver != y {
				filtered = append(filtered, driver)
			}
		}
		if len(filtered) == 0 {
			autoRestoreTrie.Delete(prefix)
			continue
		}
		autoRestoreTrie.Set(prefix, filtered)
	}
}

func autoRestoreWatcherPath(mountPath string, path string) string {
	fullPath := utils.FixAndCleanPath(stdpath.Join(mountPath, path))
	if fullPath != "/" {
		fullPath = strings.TrimRight(fullPath, "/")
	}
	return fullPath
}

func handleAutoRestoreHook(ctx context.Context, path string, objs []model.Obj) {
	path = utils.FixAndCleanPath(path)
	_ = autoRestoreTrie.VisitPrefixes(patricia.Prefix(path), func(needPrefix patricia.Prefix, item patricia.Item) error {
		drivers, ok := item.([]*Cloud189PC)
		if !ok {
			return nil
		}
		needPath := string(needPrefix)
		restPath := strings.TrimPrefix(path, needPath)
		if len(restPath) > 0 && restPath[0] != '/' {
			return nil
		}
		for _, driver := range drivers {
			driver.handleAutoRestoreExistingCASUpdate(ctx, path, objs)
		}
		return nil
	})
}

func (y *Cloud189PC) handleAutoRestoreExistingCASUpdate(ctx context.Context, parent string, objs []model.Obj) {
	if !y.AutoRestoreExistingCAS || len(objs) == 0 {
		return
	}
	relParent := utils.FixAndCleanPath(strings.TrimPrefix(parent, utils.GetActualMountPath(y.GetStorage().MountPath)))
	dir, err := y.findDirByPath(context.WithoutCancel(ctx), relParent)
	if err != nil {
		utils.Log.Warnf("189pc: immediate auto restore resolve dir %s failed: %v", parent, err)
		return
	}
	restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Minute)
	defer cancel()
	for _, obj := range objs {
		raw := model.UnwrapObjName(obj)
		if raw.IsDir() || !strings.HasSuffix(strings.ToLower(raw.GetName()), ".cas") {
			continue
		}
		if err := y.restoreExistingCASFile(restoreCtx, relParent, dir, raw); err != nil {
			utils.Log.Warnf("189pc: immediate auto restore %s/%s failed: %v", relParent, raw.GetName(), err)
		}
	}
}

func init() {
	op.RegisterObjsUpdateHook(handleAutoRestoreHook)
}
