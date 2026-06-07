package thunder_browser

import (
	"context"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	log "github.com/sirupsen/logrus"
)

func (y *ThunderBrowser) createTempDir(ctx context.Context) error {
	transferDir := ""
	sleep := time.Second * 3
	time.Sleep(sleep)

	dir := &Files{
		ID:    "",
		Space: "",
	}
	for range 5 {
		err := y.MakeDir(ctx, dir, conf.TempDirName)
		if err != nil {
			log.Warnf("create Thunder temp dir failed: %v", err)
			if strings.Contains(err.Error(), "captcha_invalid") {
				time.Sleep(sleep)
				continue
			}
		}

		files, err := y.getFiles(ctx, dir, "")
		if err != nil {
			log.Warnf("Thunder list files failed: %v", err)
			return err
		}

		for _, file := range files {
			if file.GetName() == "我的转存" {
				transferDir = file.GetID()
			}
			if file.GetName() == conf.TempDirName {
				y.TempDirId = file.GetID()
				break
			}
		}

		log.Info("Thunder temp folder id: ", y.TempDirId)
		y.cleanupTempDir(ctx)
		return nil
	}
	y.TempDirId = transferDir
	log.Info("Thunder transfer folder id: ", y.TempDirId)
	return nil
}

func (y *ThunderBrowser) cleanupTempDir(ctx context.Context) {
	dir := &Files{
		ID:    y.TempDirId,
		Space: "",
	}

	files, err := y.getFiles(ctx, dir, "")
	if err != nil {
		log.Warnf("Thunder list files failed: %v", err)
		return
	}

	for _, file := range files {
		err := y.Remove(ctx, file)
		if err != nil {
			log.Warnf("Thunder remove file failed: %v", err)
		}
	}
}

func (y *ThunderBrowser) createOfflineDir(ctx context.Context) error {
	dir := &Files{
		ID:    "",
		Space: "",
	}
	err := y.MakeDir(ctx, dir, conf.OfflineDirName)
	if err != nil {
		log.Warnf("create Thunder offline dir failed: %v", err)
	}

	files, err := y.getFiles(ctx, dir, "")
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.GetName() == conf.OfflineDirName {
			y.OfflineDirId = file.GetID()
			break
		}
	}

	log.Info("Thunder offline folder id: ", y.OfflineDirId)
	return nil
}

func (c *Common) GetShareCaptchaToken() error {
	metas := map[string]string{
		"client_version": c.ClientVersion,
		"package_name":   c.PackageName,
		"user_id":        "0",
		"username":       "",
		"email":          "",
		"phone_number":   "",
	}
	metas["timestamp"], metas["captcha_sign"] = c.GetCaptchaSign()
	return c.refreshCaptchaToken("get:/drive/v1/share", metas)
}
