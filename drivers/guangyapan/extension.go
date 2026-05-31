package guangyapan

import (
	"context"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	log "github.com/sirupsen/logrus"
)

func (d *GuangYaPan) createTempDir(ctx context.Context) {
	name := conf.TempDirName
	dir := &model.Object{
		ID: d.RootFolderID,
	}

	files, err := d.List(ctx, dir, model.ListArgs{})
	if err != nil {
		log.Warnf("list dir failed: %v", err)
	}
	for _, file := range files {
		if file.GetName() == name {
			d.TempDirId = file.GetID()
			break
		}
	}

	if d.TempDirId == "" {
		err = d.MakeDir(ctx, dir, name)
		if err == nil {
			files, err := d.List(ctx, dir, model.ListArgs{})
			if err != nil {
				log.Warnf("list dir failed: %v", err)
			}
			for _, file := range files {
				if file.GetName() == name {
					d.TempDirId = file.GetID()
					break
				}
			}
		} else {
			log.Warnf("create temp dir failed: %v", err)
		}
	}

	log.Debugf("GuangYaPan TempDirId: %v", d.TempDirId)
	d.cleanTempFile(ctx)
}

func (d *GuangYaPan) createOfflineDir(ctx context.Context) {
	name := conf.OfflineDirName
	dir := &model.Object{
		ID: d.RootFolderID,
	}

	files, err := d.List(ctx, dir, model.ListArgs{})
	if err != nil {
		log.Warnf("list dir failed: %v", err)
	}
	for _, file := range files {
		if file.GetName() == name {
			d.OfflineDirId = file.GetID()
			break
		}
	}

	if d.OfflineDirId == "" {
		err = d.MakeDir(ctx, dir, name)
		if err == nil {
			files, err := d.List(ctx, dir, model.ListArgs{})
			if err != nil {
				log.Warnf("list dir failed: %v", err)
			}
			for _, file := range files {
				if file.GetName() == name {
					d.OfflineDirId = file.GetID()
					break
				}
			}
		} else {
			log.Warnf("create temp dir failed: %v", err)
		}
	}

	log.Debugf("GuangYaPan offline: %v", d.OfflineDirId)
	d.cleanTempFile(ctx)
}

func (d *GuangYaPan) cleanTempFile(ctx context.Context) {
	if d.TempDirId == "" {
		return
	}

	dir := &model.Object{
		ID: d.TempDirId,
	}

	files, err := d.List(ctx, dir, model.ListArgs{})
	if err != nil {
		log.Warnf("list dir failed: %v", err)
	}
	for _, file := range files {
		log.Debugf("GuangYaPan delete temp file: %v %v", file.GetID(), file.GetName())
		d.Remove(ctx, file)
	}
}
