package _189pc

import (
	"context"
	stdpath "path"
	"strings"

	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/internal/stream"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

func (y *Cloud189PC) startAutoRestoreExistingCAS() error {
	removeAutoRestoreWatcher(y)
	if !y.AutoRestoreExistingCAS {
		return nil
	}
	paths := y.autoRestoreExistingCASPaths()
	if len(paths) == 0 {
		return nil
	}
	if err := insertAutoRestoreWatcher(y, paths); err != nil {
		return err
	}
	utils.Log.Infof("189pc: auto restore existing .cas enabled for %d path(s)", len(paths))
	return nil
}

func (y *Cloud189PC) restoreExistingCASFile(ctx context.Context, dirPath string, dstDir model.Obj, casFile model.Obj) error {
	casPath := stdpath.Join(dirPath, casFile.GetName())
	if !y.beginAutoRestore(casPath) {
		return nil
	}
	defer y.endAutoRestore(casPath)

	link, err := y.Link(ctx, casFile, model.LinkArgs{})
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
	restoreName, err := y.resolveRestoreSourceName(casFile.GetName(), info)
	if err != nil {
		return err
	}
	if _, err = y.findFileByName(ctx, restoreName, dstDir.GetID(), y.isFamily()); err == nil {
		if err = y.deleteRestoredCASFile(ctx, dirPath, casPath, casFile, restoreName, true); err != nil {
			utils.Log.Warnf("189pc: source %s already exists, but failed to delete %s: %v", restoreName, casPath, err)
		}
		utils.Log.Debugf("189pc: skip auto restore for %s because %s already exists", casPath, restoreName)
		return nil
	} else if !errs.IsObjectNotFound(err) {
		return err
	}

	if _, err = y.restoreSourceFromCASInfo(ctx, dstDir, casFile.GetName(), info); err != nil {
		return err
	}
	if err = y.deleteRestoredCASFile(ctx, dirPath, casPath, casFile, restoreName, false); err != nil {
		utils.Log.Warnf("189pc: auto restored %s from %s, but failed to delete the .cas file: %v", restoreName, casPath, err)
	}
	op.Cache.DeleteDirectory(y, dirPath)
	utils.Log.Infof("189pc: auto restored %s from %s", restoreName, casPath)
	return nil
}

func (y *Cloud189PC) deleteRestoredCASFile(ctx context.Context, dirPath, casPath string, casFile model.Obj, restoreName string, sourceAlreadyExists bool) error {
	if !y.shouldDeleteCASAfterRestore(casFile.GetName()) {
		return nil
	}
	if err := y.Remove(ctx, casFile); err != nil {
		return err
	}
	op.Cache.DeleteDirectory(y, dirPath)
	if sourceAlreadyExists {
		utils.Log.Infof("189pc: deleted %s because %s already exists", casPath, restoreName)
		return nil
	}
	utils.Log.Infof("189pc: deleted restored .cas file %s", casPath)
	return nil
}

func (y *Cloud189PC) beginAutoRestore(path string) bool {
	_, loaded := y.autoRestoreInFlight.LoadOrStore(path, struct{}{})
	return !loaded
}

func (y *Cloud189PC) endAutoRestore(path string) {
	y.autoRestoreInFlight.Delete(path)
}

func (y *Cloud189PC) findDirByPath(ctx context.Context, dirPath string) (model.Obj, error) {
	dirPath = utils.FixAndCleanPath(dirPath)
	current := model.Obj(&model.Object{
		ID:       y.rootFolderID(),
		Name:     "/",
		IsFolder: true,
	})
	if dirPath == "/" {
		return current, nil
	}
	for _, segment := range strings.Split(strings.Trim(dirPath, "/"), "/") {
		files, err := y.getFiles(ctx, current.GetID(), y.isFamily())
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

func (y *Cloud189PC) rootFolderID() string {
	if y.RootFolderID != "" {
		return y.RootFolderID
	}
	if y.isFamily() {
		return ""
	}
	return y.Config().DefaultRoot
}

func (y *Cloud189PC) autoRestoreExistingCASPaths() []string {
	return parseAutoRestoreExistingCASPaths(y.AutoRestoreExistingCASPaths)
}

func findNamedDir(files []model.Obj, name string) (model.Obj, error) {
	for _, file := range files {
		if file.IsDir() && file.GetName() == name {
			return file, nil
		}
	}
	return nil, errs.ObjectNotFound
}
