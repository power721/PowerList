package _189

import (
	"context"
	"errors"
	stdpath "path"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	log "github.com/sirupsen/logrus"
	"github.com/tchap/go-patricia/v2/patricia"
)

var autoRestoreTrie = patricia.NewTrie()

func insertAutoRestoreWatcher(d *Cloud189, paths []string) error {
	mountPath := utils.GetActualMountPath(d.GetStorage().MountPath)
	for _, path := range paths {
		prefix := patricia.Prefix(autoRestoreWatcherPath(mountPath, path))
		existing := autoRestoreTrie.Get(prefix)
		if existing == nil {
			if !autoRestoreTrie.Insert(prefix, []*Cloud189{d}) {
				return errors.New("failed to insert auto restore watcher")
			}
			continue
		}
		drivers, ok := existing.([]*Cloud189)
		if !ok {
			continue
		}
		for _, driver := range drivers {
			if driver == d {
				goto next
			}
		}
		autoRestoreTrie.Set(prefix, append(drivers, d))
	next:
	}
	return nil
}

func removeAutoRestoreWatcher(d *Cloud189) {
	mountPath := utils.GetActualMountPath(d.GetStorage().MountPath)
	for _, path := range d.autoRestoreExistingCASPaths() {
		prefix := patricia.Prefix(autoRestoreWatcherPath(mountPath, path))
		existing := autoRestoreTrie.Get(prefix)
		if existing == nil {
			continue
		}
		drivers, ok := existing.([]*Cloud189)
		if !ok {
			continue
		}
		if len(drivers) == 1 && drivers[0] == d {
			autoRestoreTrie.Delete(prefix)
			continue
		}
		filtered := drivers[:0]
		for _, driver := range drivers {
			if driver != d {
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
		drivers, ok := item.([]*Cloud189)
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

func (d *Cloud189) handleAutoRestoreExistingCASUpdate(ctx context.Context, parent string, objs []model.Obj) {
	if !d.AutoRestoreExistingCAS || len(objs) == 0 {
		return
	}
	relParent := utils.FixAndCleanPath(strings.TrimPrefix(parent, utils.GetActualMountPath(d.GetStorage().MountPath)))
	dir, err := d.findDirByPath(relParent)
	if err != nil {
		log.Warnf("189: immediate auto restore resolve dir %s failed: %v", parent, err)
		return
	}
	restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Minute)
	defer cancel()
	for _, obj := range objs {
		raw := model.UnwrapObjName(obj)
		if raw.IsDir() || !strings.HasSuffix(strings.ToLower(raw.GetName()), ".cas") {
			continue
		}
		if err := d.restoreExistingCASFile(restoreCtx, relParent, dir, raw); err != nil {
			log.Warnf("189: immediate auto restore %s/%s failed: %v", relParent, raw.GetName(), err)
		}
	}
}

func init() {
	op.RegisterObjsUpdateHook(handleAutoRestoreHook)
}
