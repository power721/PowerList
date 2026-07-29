package _123_open

import (
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	log "github.com/sirupsen/logrus"
)

// 临时目录与延时清理,镜像 drivers/quark_uc/extension.go + drivers/quark_uc_share deleteDelay。
// 跨盘秒传(drivers/123_rapid)产生的文件落到 alist-tvbox-temp,播放后延时移入回收站,
// 避免占满 123 Open 账号存储。

// GetTempFolder 查找/创建根目录下的 alist-tvbox-temp 临时目录,记录其 fileId。
// 找到则清理上次崩溃遗留;找不到则新建。Init 时 best-effort 调用。
func (d *Open123) GetTempFolder() {
	if id, ok := d.findTempFolder(); ok {
		d.TempDirId = id
		log.Infof("[%d] 123 Open temp folder id: %d", d.ID, d.TempDirId)
		d.CleanTempFolder()
		return
	}
	d.CreateTempFolder()
}

// CreateTempFolder 在根目录创建 alist-tvbox-temp 并记录其 fileId。
func (d *Open123) CreateTempFolder() {
	if err := d.mkdir(0, conf.TempDirName); err != nil {
		// 已存在等情形:重列根目录兜底找回。
		log.Warnf("[%d] create 123 Open temp folder error: %v", d.ID, err)
	}
	if id, ok := d.findTempFolder(); ok {
		d.TempDirId = id
		log.Infof("[%d] create 123 Open temp folder id: %d", d.ID, d.TempDirId)
	}
}

// CleanTempFolder 清理 temp 目录下残留文件(上次崩溃/未及删除的遗留)。
func (d *Open123) CleanTempFolder() {
	if d.TempDirId == 0 {
		return
	}
	files, err := d.listAllFiles(d.TempDirId)
	if err != nil {
		log.Warnf("[%d] list 123 Open temp folder error: %v", d.ID, err)
		return
	}
	for _, f := range files {
		go d.DeleteFile(f.FileId)
	}
}

// DeleteFile 将临时文件移入回收站(123 Open API 仅暴露 trash,无永久删除)。
func (d *Open123) DeleteFile(fileID int64) {
	if fileID <= 0 {
		return
	}
	if err := d.trash(fileID); err != nil {
		log.Warnf("[%d] delete 123 Open temp file %d failed: %v", d.ID, fileID, err)
	}
}

// DeleteDelay 延时删除秒传产生的临时文件。镜像 quark_uc_share deleteDelay。
// delayTime=0 关闭清理(退回不清理旧行为);<5 钳到 5 秒。
func (d *Open123) DeleteDelay(fileID int64) {
	if fileID <= 0 {
		return
	}
	delayTime := setting.GetInt(conf.DeleteDelayTime, 900)
	if delayTime == 0 {
		return
	}
	if delayTime < 5 {
		delayTime = 5
	}
	log.Infof("[%d] Delete 123 Open temp file %d after %d seconds.", d.ID, fileID, delayTime)
	time.Sleep(time.Duration(delayTime) * time.Second)
	d.DeleteFile(fileID)
}

// findTempFolder 在根目录查找名为 alist-tvbox-temp 的非回收文件夹。
func (d *Open123) findTempFolder() (int64, bool) {
	files, err := d.listAllFiles(0)
	if err != nil {
		log.Warnf("[%d] list 123 Open root error: %v", d.ID, err)
		return 0, false
	}
	for _, f := range files {
		if f.IsDir() && f.FileName == conf.TempDirName {
			return f.FileId, true
		}
	}
	return 0, false
}

// listAllFiles 列出 parentFileId 下全部非回收文件(分页直到 LastFileId==-1)。
func (d *Open123) listAllFiles(parentFileId int64) ([]File, error) {
	out := make([]File, 0, 64)
	last := int64(0)
	for page := 0; page < 1000; page++ {
		resp, err := d.getFiles(parentFileId, 100, last)
		if err != nil {
			return nil, err
		}
		for _, f := range resp.Data.FileList {
			if f.Trashed == 0 {
				out = append(out, f)
			}
		}
		if resp.Data.LastFileId == -1 || resp.Data.LastFileId == last {
			break
		}
		last = resp.Data.LastFileId
	}
	return out, nil
}
