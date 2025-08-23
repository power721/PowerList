package _139_share

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	_139 "github.com/OpenListTeam/OpenList/v4/drivers/139"
	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	log "github.com/sirupsen/logrus"
	"io"
	"net/http"
	"time"
)

var (
	secretKey = []byte("PVGDwmcvfs1uV3d1")
)
var idx = 0

func (y *Yun139Share) httpPost(pathname string, data string, auth bool) ([]byte, error) {
	u := "https://share-kd-njs.yun.139.com/yun-share/richlifeApp/devapp/IOutLink/" + pathname
	req := base.RestyClient.R()
	req.SetHeaders(map[string]string{
		"Content-Type":  "application/json",
		"Referer":       "https://yun.139.com/",
		"User-Agent":    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/95.0.4638.54 Safari/537.36",
		"hcy-cool-flag": "1",
		"x-deviceinfo":  "||3|12.27.0|chrome|131.0.0.0|5c7c68368f048245e1ce47f1c0f8f2d0||windows 10|1536X695|zh-CN|||",
	})

	if auth {
		driver := op.GetFirstDriver("139Yun", idx)
		if driver != nil {
			yun139 := driver.(*_139.Yun139)
			req.SetHeader("Authorization", "Basic "+yun139.Authorization)
		}
	}

	req.SetBody(data)

	res, err := req.Execute(http.MethodPost, u)
	if err != nil {
		return nil, err
	}

	return res.Body(), nil
}

func (y *Yun139Share) getShareInfo(pCaID string, page int) (ListResp, error) {
	size := 200
	start := page*size + 1
	end := (page + 1) * size
	requestBody := map[string]interface{}{
		"getOutLinkInfoReq": map[string]interface{}{
			"account": "",
			"linkID":  y.ShareId,
			"passwd":  y.SharePwd,
			"caSrt":   1,
			"coSrt":   1,
			"srtDr":   0,
			"bNum":    start,
			"pCaID":   pCaID,
			"eNum":    end,
		},
	}

	var res ListResp

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return res, err
	}

	encrypted, err := encrypt(string(jsonData))
	if err != nil {
		return res, err
	}

	resp, err := y.httpPost("getOutLinkInfoV6", encrypted, false)
	if err != nil {
		return res, err
	}

	decrypted, err := decrypt(string(resp))
	if err != nil {
		return res, err
	}

	if err := json.Unmarshal([]byte(decrypted), &res); err != nil {
		return res, err
	}

	if res.Code != "0" {
		return res, errors.New(res.Desc)
	}

	return res, nil
}

func (y *Yun139Share) list(pCaID string) ([]File, error) {
	actualID := pCaID
	if pCaID == "" {
		actualID = "root"
	}

	files := make([]File, 0)

	log.Debugf("list files: %v", actualID)

	page := 0
	for {
		res, err := y.getShareInfo(actualID, page)
		if err != nil {
			return nil, err
		}

		log.Debugf("list count: %v next: %v, %v folders %v files", res.Data.Count, res.Data.Next, len(res.Data.Folders), len(res.Data.Files))

		for _, f := range res.Data.Folders {
			file := File{
				Name:  f.Name,
				Path:  f.Path,
				IsDir: true,
			}
			parsedTime, _ := time.Parse("20250416195740", f.UpdatedAt)
			file.Time = parsedTime
			files = append(files, file)
		}

		for _, f := range res.Data.Files {
			parsedTime, _ := time.Parse("20250416195740", f.UpdatedAt)
			f.Time = parsedTime
			f.IsDir = false
			files = append(files, f)
		}

		if len(res.Data.Next) == 0 {
			break
		}
		page++
	}

	log.Debugf("list get %v files", len(files))
	return files, nil
}

func (y *Yun139Share) link(yun139 *_139.Yun139, fid string) (string, error) {
	account := yun139.Account
	params := map[string]interface{}{
		"dlFromOutLinkReqV3": map[string]interface{}{
			"linkID":  y.ShareId,
			"account": account,
			"coIDLst": map[string]interface{}{
				"item": []string{fid},
			},
		},
		"commonAccountInfo": map[string]interface{}{
			"account":     account,
			"accountType": 1,
		},
	}

	jsonData, err := json.Marshal(params)
	if err != nil {
		return "", err
	}

	encrypted, err := encrypt(string(jsonData))
	if err != nil {
		return "", err
	}

	resp, err := y.httpPost("dlFromOutLinkV3", encrypted, true)
	if err != nil {
		return "", err
	}

	decrypted, err := decrypt(string(resp))
	if err != nil {
		return "", err
	}

	var res LinkResp
	if err := json.Unmarshal([]byte(decrypted), &res); err != nil {
		return "", err
	}

	if res.Code != "0" {
		return "", errors.New(res.Desc)
	}

	log.Debugf("link result: %v", decrypted)
	url := res.Data.ExtInfo.Url

	if len(url) == 0 {
		url = res.Data.Url
	}

	return url, nil
}

func encrypt(data string) (string, error) {
	log.Debugf("encrypt: %v", data)
	block, err := aes.NewCipher(secretKey)
	if err != nil {
		return "", err
	}

	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}

	paddedData := pkcs7Pad([]byte(data), aes.BlockSize)
	mode := cipher.NewCBCEncrypter(block, iv)
	encrypted := make([]byte, len(paddedData))
	mode.CryptBlocks(encrypted, paddedData)

	combined := append(iv, encrypted...)
	return base64.StdEncoding.EncodeToString(combined), nil
}

func decrypt(data string) (string, error) {
	combined, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", err
	}

	if len(combined) < aes.BlockSize {
		return "", errors.New("ciphertext too short")
	}

	iv := combined[:aes.BlockSize]
	encrypted := combined[aes.BlockSize:]

	block, err := aes.NewCipher(secretKey)
	if err != nil {
		return "", err
	}

	if len(encrypted)%aes.BlockSize != 0 {
		return "", errors.New("ciphertext is not a multiple of the block size")
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	decrypted := make([]byte, len(encrypted))
	mode.CryptBlocks(decrypted, encrypted)

	unpadded, err := pkcs7Unpad(decrypted, aes.BlockSize)
	if err != nil {
		return "", err
	}

	return string(unpadded), nil
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - (len(data) % blockSize)
	padText := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(data, padText...)
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data)%blockSize != 0 || len(data) == 0 {
		return nil, errors.New("invalid padding")
	}

	padding := int(data[len(data)-1])
	if padding < 1 || padding > blockSize {
		return nil, errors.New("invalid padding")
	}

	for i := len(data) - padding; i < len(data); i++ {
		if int(data[i]) != padding {
			return nil, errors.New("invalid padding")
		}
	}

	return data[:len(data)-padding], nil
}
