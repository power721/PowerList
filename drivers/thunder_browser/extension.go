package thunder_browser

import (
	"context"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	log "github.com/sirupsen/logrus"
)

func (y *ThunderBrowser) createTempDir(ctx context.Context) error {
	dir := &Files{
		ID:    "",
		Space: "",
	}
	err := y.MakeDir(ctx, dir, conf.TempDirName)
	if err != nil {
		log.Warnf("create Thunder temp dir failed: %v", err)
	}

	files, err := y.getFiles(ctx, dir, "")
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.GetName() == conf.TempDirName {
			y.TempDirId = file.GetID()
			break
		}
	}

	log.Info("Thunder temp folder id: ", y.TempDirId)
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
