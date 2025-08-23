package _189pc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	"github.com/OpenListTeam/OpenList/v4/pkg/cron"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	log "github.com/sirupsen/logrus"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (y *Cloud189PC) createTempDir(ctx context.Context) error {
	dir := &Cloud189File{
		ID: "-11",
	}
	_, err := y.MakeDir(ctx, dir, conf.TempDirName)
	if err != nil {
		log.Warnf("create temp dir failed: %v", err)
	}

	files, err := y.getFiles(ctx, y.TempDirId, false)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.GetName() == conf.TempDirName {
			y.TempDirId = file.GetID()
			break
		}
	}

	log.Info("189Cloud temp folder id: ", y.TempDirId)
	return nil
}

func (y *Cloud189PC) Checkin() {
	if !y.AutoCheckin {
		return
	}

	go y.checkin()
	y.cron = cron.NewCron(time.Hour * 24)
	y.cron.Do(func() {
		y.checkin()
	})
}

func (y *Cloud189PC) checkin() {
	url := API_URL + "/mkt/userSign.action"
	res, err := y.get(url, nil, nil)
	log.Infof("[%v] checkin result: %s", y.ID, string(res))
	if err != nil {
		log.Warnf("[%v] checkin failed: %v", y.ID, err)
	}

	res, err = y.get("https://m.cloud.189.cn/v2/drawPrizeMarketDetails.action?taskId=TASK_SIGNIN&activityId=ACT_SIGNIN", nil, nil)
	log.Infof("[%v] TASK_SIGNIN result: %s", y.ID, string(res))
	if err != nil {
		log.Warnf("[%v] TASK_SIGNIN failed: %v", y.ID, err)
	}

	//res, err = y.get("https://m.cloud.189.cn/v2/drawPrizeMarketDetails.action?taskId=TASK_SIGNIN_PHOTOS&activityId=ACT_SIGNIN", nil, nil)
	//log.Infof("TASK_SIGNIN_PHOTOS result: %s", string(res))
	//if err != nil {
	//	log.Warnf("TASK_SIGNIN_PHOTOS failed: %v", err)
	//}
}

func (y *Cloud189PC) GetShareLink(shareId int, file model.Obj) (*model.Link, error) {
	if y.Cookie == "" {
		return nil, errors.New("no cookie found")
	}

	url := "https://cloud.189.cn/api/portal/getNewVlcVideoPlayUrl.action"
	res, err := y.client.R().
		SetQueryParams(map[string]string{
			"shareId": strconv.Itoa(shareId),
			"fileId":  file.GetID(),
			"dt":      "1",
			"type":    "4",
		}).
		SetHeader("accept", "application/json;charset=UTF-8").
		SetHeader("cookie", y.Cookie).
		Get(url)

	log.Debugf("[%v] getShareLink result: %s", y.ID, res.String())
	if err != nil {
		log.Warnf("[%v] getShareLink failed: %v", y.ID, err)
	}

	url = utils.Json.Get(res.Body(), "normal", "url").ToString()
	if url != "" {
		res, err = y.client2.R().
			SetQueryParams(map[string]string{
				"shareId": strconv.Itoa(shareId),
				"fileId":  file.GetID(),
				"dt":      "1",
				"type":    "4",
			}).
			SetHeader("accept", "application/json;charset=UTF-8").
			SetHeader("cookie", y.Cookie).
			Get(url)
		newUrl := res.Header().Get("Location")
		if newUrl != "" {
			url = newUrl
		}
		exp := time.Hour
		link := &model.Link{
			Expiration: &exp,
			URL:        url,
			Header: http.Header{
				"User-Agent": []string{base.UserAgent},
			},
		}
		log.Debugf("使用直链播放：%v", url)
		return link, nil
	}

	msg := utils.Json.Get(res.Body(), "errorMsg").ToString()
	return nil, errors.New(msg)
}

func (y *Cloud189PC) Transfer(ctx context.Context, shareId int, fileId string, fileName string) (*model.Link, error) {
	if y.getTokenInfo() == nil {
		return nil, errors.New("no token found")
	}

	isFamily := y.isFamily()
	other := map[string]string{"shareId": strconv.Itoa(shareId)}

	log.Debug("create share save task")
	resp, err := y.CreateBatchTask("SHARE_SAVE", IF(isFamily, y.FamilyID, ""), y.TempDirId, other, BatchTaskInfo{
		FileId:   fileId,
		FileName: fileName,
		IsFolder: 0,
	})

	if err != nil && !strings.Contains(err.Error(), "there is a conflict with the target object") {
		return nil, err
	}

	log.Debug("wait task")
	err = y.WaitBatchTask("SHARE_SAVE", resp.TaskID, time.Second)
	if err != nil && !strings.Contains(err.Error(), "there is a conflict with the target object") {
		return nil, err
	}

	log.Debug("get files")
	files, err := y.getFiles(ctx, y.TempDirId, false)
	if err != nil {
		return nil, err
	}

	log.Debug("get new file")
	var transferFile model.Obj
	for _, file := range files {
		if file.GetName() == fileName {
			transferFile = file
			break
		}
	}

	if transferFile == nil || transferFile.GetID() == "" {
		return nil, errors.New("文件转存失败")
	}

	log.Debug("get new file link")
	link, err := y.Link(ctx, transferFile, model.LinkArgs{})

	go func() {
		delayTime := setting.GetInt(conf.DeleteDelayTime, 900)
		if delayTime == 0 {
			return
		}

		log.Infof("[%v] Delete 189 temp file %v after %v seconds.", y.ID, fileId, delayTime)
		time.Sleep(time.Duration(delayTime) * time.Second)

		log.Infof("[%v] Delete 189 temp file: %v %v", y.ID, fileId, fileName)
		removeErr := y.Remove(ctx, transferFile)
		if removeErr != nil {
			log.Infof("[%v] 天翼云盘删除文件:%s失败: %v", y.ID, fileName, removeErr)
			return
		}
		log.Debugf("[%v] 已删除天翼云盘下的文件: %v", y.ID, fileName)
		_, removeErr = y.CreateBatchTask("CLEAR_RECYCLE", "", "", nil, BatchTaskInfo{
			FileId:   transferFile.GetID(),
			FileName: transferFile.GetName(),
			IsFolder: 0,
		})
		if removeErr != nil {
			log.Infof("[%v] 天翼云盘清除回收站失败: %v", y.ID, removeErr)
		} else {
			log.Debugf("[%v] 天翼云盘清除回收站完成", y.ID)
		}
	}()

	return link, err
}

func RsaEncode(origData []byte, j_rsakey string, hex bool) string {
	publicKey := []byte("-----BEGIN PUBLIC KEY-----\n" + j_rsakey + "\n-----END PUBLIC KEY-----")
	block, _ := pem.Decode(publicKey)
	pubInterface, _ := x509.ParsePKIXPublicKey(block.Bytes)
	pub := pubInterface.(*rsa.PublicKey)
	b, err := rsa.EncryptPKCS1v15(rand.Reader, pub, origData)
	if err != nil {
		log.Errorf("err: %s", err.Error())
	}
	res := base64.StdEncoding.EncodeToString(b)
	if hex {
		return b64tohex(res)
	}
	return res
}

var b64map = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

var BI_RM = "0123456789abcdefghijklmnopqrstuvwxyz"

func int2char(a int) string {
	return strings.Split(BI_RM, "")[a]
}

func b64tohex(a string) string {
	d := ""
	e := 0
	c := 0
	for i := 0; i < len(a); i++ {
		m := strings.Split(a, "")[i]
		if m != "=" {
			v := strings.Index(b64map, m)
			if 0 == e {
				e = 1
				d += int2char(v >> 2)
				c = 3 & v
			} else if 1 == e {
				e = 2
				d += int2char(c<<2 | v>>4)
				c = 15 & v
			} else if 2 == e {
				e = 3
				d += int2char(c)
				d += int2char(v >> 2)
				c = 3 & v
			} else {
				e = 0
				d += int2char(c<<2 | v>>4)
				d += int2char(15 & v)
			}
		}
	}
	if e == 1 {
		d += int2char(c << 2)
	}
	return d
}
