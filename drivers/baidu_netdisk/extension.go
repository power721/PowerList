package baidu_netdisk

import (
	"context"
	"errors"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
	stdpath "path"
)

func (d *BaiduNetdisk) createTempDir() error {
	d.TempDirId = "/"
	var newDir File
	_, err := d.create(stdpath.Join("/", conf.TempDirName), 0, 1, "", "", &newDir, 0, 0)
	if err != nil {
		log.Warnf("create temp dir failed: %v", err)
	}

	files, err := d.getFiles("/")
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.ServerFilename == conf.TempDirName {
			d.TempDirId = file.Path
			break
		}
	}

	log.Infof("Baidu temp dir: %v", d.TempDirId)

	d.cleanTempFile()
	return nil
}

func (d *BaiduNetdisk) cleanTempFile() {
	if d.TempDirId == "/" {
		return
	}

	files, err := d.getFiles(d.TempDirId)
	if err != nil {
		return
	}

	for _, file := range files {
		log.Infof("Delete Baidu temp file: %v %v", file.FsId, file.Path)
		d.Remove(context.Background(), fileToObj(file))
	}
}

func (d *BaiduNetdisk) verifyCookie() error {
	client := resty.New().
		SetBaseURL("https://pan.baidu.com").
		SetHeader("User-Agent", "netdisk").
		SetHeader("Referer", "https://pan.baidu.com")

	query := map[string]string{
		"app_id":     "250528",
		"method":     "query",
		"clienttype": "0",
		"web":        "1",
		"dp-logid":   "",
	}
	respJson := struct {
		ErrorCode int64  `json:"error_code"`
		ErrorMsg  string `json:"error_msg"`
		Info      struct {
			Username string `json:"username"`
			UK       int64  `json:"uk"`
			State    int    `json:"loginstate"`
			IsVip    int    `json:"is_vip"`
			IsSVip   int    `json:"is_svip"`
		} `json:"user_info"`
	}{}

	res, err := client.R().
		SetQueryParams(query).
		SetHeader("Cookie", d.Cookie).
		SetResult(&respJson).
		Post("/rest/2.0/membership/user/info")
	if err != nil {
		log.Warnf("cookie error: %v", err)
		return err
	}
	if d.UK != respJson.Info.UK {
		return errors.New("cookie and token mismatch")
	}
	log.Debugf("user info: %v", res.String())
	log.Infof("cookie user info: %v", respJson.Info)
	return nil
}
