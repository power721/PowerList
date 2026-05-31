package guangyapan

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/OpenListTeam/OpenList/v4/drivers/guangyapan"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/offline_download/tool"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
)

type GuangYaPan struct {
	refreshTaskCache bool
}

func (g *GuangYaPan) Name() string {
	return "GuangYaPan"
}

func (g *GuangYaPan) Items() []model.SettingItem {
	return nil
}

func (g *GuangYaPan) Run(task *tool.DownloadTask) error {
	return errs.NotSupport
}

func (g *GuangYaPan) Init() (string, error) {
	g.refreshTaskCache = false
	return "ok", nil
}

func (g *GuangYaPan) IsReady() bool {
	tempDir := setting.GetStr(conf.GuangYaPanTempDir)
	if tempDir == "" {
		return false
	}
	storage, _, err := op.GetStorageAndActualPath(tempDir)
	if err != nil {
		return false
	}
	if _, ok := storage.(*guangyapan.GuangYaPan); !ok {
		return false
	}
	return true
}

func (g *GuangYaPan) AddURL(args *tool.AddUrlArgs) (string, error) {
	g.refreshTaskCache = true
	storage, actualPath, err := op.GetStorageAndActualPath(args.TempDir)
	if err != nil {
		return "", err
	}
	driver, ok := storage.(*guangyapan.GuangYaPan)
	if !ok {
		return "", errors.New("unsupported storage driver for offline download, only GuangYaPan is supported")
	}

	ctx := context.Background()
	if err := op.MakeDir(ctx, storage, actualPath); err != nil {
		return "", err
	}
	parentDir, err := op.GetUnwrap(ctx, storage, actualPath)
	if err != nil {
		return "", err
	}

	task, err := driver.OfflineDownload(ctx, args.Url, parentDir, "")
	if err != nil {
		return "", fmt.Errorf("failed to add offline download task: %w", err)
	}
	return task.TaskID, nil
}

func (g *GuangYaPan) Remove(task *tool.DownloadTask) error {
	storage, _, err := op.GetStorageAndActualPath(task.TempDir)
	if err != nil {
		return err
	}
	driver, ok := storage.(*guangyapan.GuangYaPan)
	if !ok {
		return errors.New("unsupported storage driver for offline download, only GuangYaPan is supported")
	}
	ctx := context.Background()
	return driver.DeleteOfflineTasks(ctx, []string{task.GID}, false)
}

func (g *GuangYaPan) Status(task *tool.DownloadTask) (*tool.Status, error) {
	storage, _, err := op.GetStorageAndActualPath(task.TempDir)
	if err != nil {
		return nil, err
	}
	driver, ok := storage.(*guangyapan.GuangYaPan)
	if !ok {
		return nil, errors.New("unsupported storage driver for offline download, only GuangYaPan is supported")
	}
	tasks, err := g.GetTasks(driver)
	if err != nil {
		return nil, err
	}
	s := &tool.Status{
		Progress:  0,
		Completed: false,
		Status:    "the task has been deleted",
		Err:       nil,
	}
	for _, t := range tasks {
		if t.TaskID == task.GID {
			s.Progress = float64(t.Progress)
			s.TotalBytes = t.FileSize
			s.Completed = (t.Status == 2)
			switch t.Status {
			case 0:
				s.Status = "waiting"
			case 1:
				s.Status = "downloading"
			case 2:
				s.Status = "completed"
			case 3:
				s.Status = "error"
				s.Err = errors.New("download failed")
			case 4:
				s.Status = "paused"
			default:
				s.Status = strconv.Itoa(t.Status)
			}
			if t.TaskName != "" {
				s.Status = t.TaskName + " " + s.Status
			}
			return s, nil
		}
	}
	s.Err = errors.New("the task has been deleted")
	return s, nil
}

func init() {
	tool.Tools.Add(&GuangYaPan{})
}
