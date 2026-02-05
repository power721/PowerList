package _115

import (
	"context"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	driver115 "github.com/power721/115driver/pkg/driver"
	log "github.com/sirupsen/logrus"
)

func (d *Pan115) GetClient() *driver115.Pan115Client {
	return d.client
}

func (d *Pan115) UploadAvailable() (bool, error) {
	return d.client.UploadAvailable()
}

func (d *Pan115) createTempDir(ctx context.Context) {
	root := d.Addition.RootID.RootFolderID
	d.TempDirId = root
	dir := &model.Object{
		ID: root,
	}
	var clean = false
	name := conf.TempDirName
	if setting.GetBool(conf.AliTo115) {
		_, _ = d.MakeDir(ctx, dir, name)
	}
	files, _ := d.getFiles(root)
	for _, file := range files {
		if file.Name == "最近接收" {
			d.ReceiveDirId = file.FileID
		}
		if file.Name == name {
			d.TempDirId = file.FileID
			clean = true
		}
	}
	if !setting.GetBool(conf.AliTo115) {
		d.TempDirId = d.ReceiveDirId
	}
	log.Infof("115 temp folder id: %v", d.TempDirId)
	log.Infof("115 receive folder id: %v", d.ReceiveDirId)
	if clean {
		d.cleanTempDir()
	}
}

func (d *Pan115) cleanTempDir() {
	files, _ := d.getFiles(d.TempDirId)
	for _, file := range files {
		log.Infof("删除115文件: %v %v 创建于 %v", file.GetName(), file.GetID(), file.CreateTime().Local())
		d.client.Delete(file.GetID())
		d.DeleteFile(file.Sha1)
	}
}

func (d *Pan115) DeleteTempFile(fullHash string) {
	files, _ := d.getFiles(d.TempDirId)
	for _, file := range files {
		if file.Sha1 == fullHash {
			log.Infof("删除115文件: %v %v 创建于 %v", file.GetName(), file.GetID(), file.CreateTime().Local())
			d.client.Delete(file.GetID())
			d.DeleteFile(file.Sha1)
		}
	}
}

func (d *Pan115) getReceiveDirId() {
	files, _ := d.getFiles("0")
	for _, file := range files {
		if file.Name == "最近接收" {
			d.ReceiveDirId = file.FileID
		}
	}
	log.Infof("115 receive folder id: %v", d.ReceiveDirId)
}

func (d *Pan115) DeleteReceivedFile(sha1 string) {
	if len(d.ReceiveDirId) == 0 {
		d.getReceiveDirId()
	}
	files, _ := d.getFiles(d.ReceiveDirId)
	for _, file := range files {
		if file.Sha1 == sha1 {
			log.Infof("[%v] 删除115文件: %v %v 创建于 %v", d.ID, file.GetName(), file.GetID(), file.CreateTime().Local())
			d.client.Delete(file.GetID())
			d.DeleteFile(file.Sha1)
		}
	}
}

func (d *Pan115) DeleteFile(id string) error {
	if d.DeleteCode == "" {
		return nil
	}

	return d.client.CleanRecycleBin(d.DeleteCode, id)
}

func (d *Pan115) RapidUpload(fileSize int64, fileName, dirID, preID, fileID string, stream model.FileStreamer) (*driver115.UploadInitResp, error) {
	return d.rapidUpload(fileSize, fileName, dirID, preID, fileID, stream)
}
