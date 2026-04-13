package _189

import (
	"context"
	stdpath "path"
	"strings"

	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/internal/stream"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	log "github.com/sirupsen/logrus"
)

func (d *Cloud189) startAutoRestoreExistingCAS() error {
	removeAutoRestoreWatcher(d)
	if !d.AutoRestoreExistingCAS {
		return nil
	}
	paths := d.autoRestoreExistingCASPaths()
	if len(paths) == 0 {
		return nil
	}
	if err := insertAutoRestoreWatcher(d, paths); err != nil {
		return err
	}
	log.Infof("189: auto restore existing .cas enabled for %d path(s)", len(paths))
	return nil
}

func (d *Cloud189) restoreExistingCASFile(ctx context.Context, dirPath string, dstDir model.Obj, casFile model.Obj) error {
	casPath := stdpath.Join(dirPath, casFile.GetName())
	if !d.beginAutoRestore(casPath) {
		return nil
	}
	defer d.endAutoRestore(casPath)

	link, err := d.Link(ctx, casFile, model.LinkArgs{})
	if err != nil {
		return err
	}
	casStream, err := stream.NewSeekableStream(&stream.FileStream{
		Ctx: ctx,
		Obj: casFile,
	}, link)
	if err != nil {
		return err
	}
	defer casStream.Close()

	info, err := readCASRestoreInfo(casStream)
	if err != nil {
		return err
	}
	restoreName, err := d.resolveRestoreSourceName(casFile.GetName(), info)
	if err != nil {
		return err
	}
	if _, err = d.findFileByName(dstDir.GetID(), restoreName); err == nil {
		if err = d.deleteRestoredCASFile(ctx, dirPath, casPath, casFile, restoreName, true); err != nil {
			log.Warnf("189: source %s already exists, but failed to delete %s: %v", restoreName, casPath, err)
		}
		log.Debugf("189: skip auto restore for %s because %s already exists", casPath, restoreName)
		return nil
	} else if !errs.IsObjectNotFound(err) {
		return err
	}

	if _, err = d.restoreSourceFromCASInfo(ctx, dstDir, casFile.GetName(), info); err != nil {
		return err
	}
	if err = d.deleteRestoredCASFile(ctx, dirPath, casPath, casFile, restoreName, false); err != nil {
		log.Warnf("189: auto restored %s from %s, but failed to delete the .cas file: %v", restoreName, casPath, err)
	}
	op.Cache.DeleteDirectory(d, dirPath)
	log.Infof("189: auto restored %s from %s", restoreName, casPath)
	return nil
}

func (d *Cloud189) deleteRestoredCASFile(ctx context.Context, dirPath, casPath string, casFile model.Obj, restoreName string, sourceAlreadyExists bool) error {
	if !d.shouldDeleteCASAfterRestore(casFile.GetName()) {
		return nil
	}
	if err := d.Remove(ctx, casFile); err != nil {
		return err
	}
	op.Cache.DeleteDirectory(d, dirPath)
	if sourceAlreadyExists {
		log.Infof("189: deleted %s because %s already exists", casPath, restoreName)
		return nil
	}
	log.Infof("189: deleted restored .cas file %s", casPath)
	return nil
}

func (d *Cloud189) beginAutoRestore(path string) bool {
	_, loaded := d.autoRestoreInFlight.LoadOrStore(path, struct{}{})
	return !loaded
}

func (d *Cloud189) endAutoRestore(path string) {
	d.autoRestoreInFlight.Delete(path)
}

func (d *Cloud189) findDirByPath(dirPath string) (model.Obj, error) {
	dirPath = utils.FixAndCleanPath(dirPath)
	current := model.Obj(&model.Object{
		ID:       d.rootFolderID(),
		Name:     "/",
		IsFolder: true,
	})
	if dirPath == "/" {
		return current, nil
	}
	for _, segment := range strings.Split(strings.Trim(dirPath, "/"), "/") {
		files, err := d.getFiles(current.GetID())
		if err != nil {
			return nil, err
		}
		next, err := findNamedDir(files, segment)
		if err != nil {
			return nil, err
		}
		current = next
	}
	return current, nil
}

func (d *Cloud189) rootFolderID() string {
	if d.RootFolderID != "" {
		return d.RootFolderID
	}
	return d.Config().DefaultRoot
}

func (d *Cloud189) autoRestoreExistingCASPaths() []string {
	return parseAutoRestoreExistingCASPaths(d.AutoRestoreExistingCASPaths)
}

func findNamedDir(files []model.Obj, name string) (model.Obj, error) {
	for _, file := range files {
		if file.IsDir() && file.GetName() == name {
			return file, nil
		}
	}
	return nil, errs.ObjectNotFound
}
