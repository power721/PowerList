package _139_grouplink

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	log "github.com/sirupsen/logrus"
	_139 "github.com/OpenListTeam/OpenList/v4/drivers/139"
	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"net/http"
)

const apiBase = "https://share-kd-njs.yun.139.com/yun-share/general/IOutLink/"
var idx int32 = 0

type GetDownloadUrlReq struct {
	UserDomainId string `json:"userDomainId"` 
	LinkId       string `json:"linkId"`       
	AssetsId     string `json:"assetsId"`     
}

type GetDownloadUrlResp struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Data    struct {
		DownLoadUrl   string `json:"downLoadUrl"`   
		CdnDownLoadUrl string `json:"cdnDownLoadUrl"`
	} `json:"data"`
}

func (y *Yun139GroupLink) httpPost(pathname string, data interface{}, auth bool) ([]byte, error) {
	u := apiBase + pathname
	req := base.RestyClient.R()

	req.SetHeaders(map[string]string{
		"Content-Type":     "application/json;charset=utf-8",
		"Referer":          "https://yun.139.com/",
		"User-Agent":       "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:147.0) Gecko/20100101 Firefox/147.0",
		"Origin":           "https://yun.139.com",
		"x-share-channel":  "0102", 
		"hcy-cool-flag":    "1",
	})

	if auth {
		driverIdx := int(atomic.LoadInt32(&idx) % int32(op.GetDriverCount("139Yun")))
		driver := op.GetFirstDriver("139Yun", driverIdx)
		if driver != nil {
			yun139 := driver.(*_139.Yun139)
			req.SetHeader("Authorization", "Basic "+yun139.Authorization)
		} else {
			log.Warn("未找到139Yun驱动，无法添加Authorization鉴权头")
		}
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	req.SetBody(jsonData)

	res, err := req.Execute(http.MethodPost, u)
	if err != nil {
		log.Warnf("HTTP请求失败: %v, url: %s", err, u)
		return nil, err
	}

	return res.Body(), nil
}

func (y *Yun139GroupLink) getDownloadUrl(fid string) (string, error) {
	if y.UserDomainId == "" {
		return "", errors.New("userDomainId未初始化，请先执行根目录探活")
	}
	req := GetDownloadUrlReq{
		UserDomainId: y.UserDomainId, 
		LinkId:       y.ShareId,      
		AssetsId:     fid,            
	}

	respBody, err := y.httpPost("getDownloadUrl", req, true)
	if err != nil {
		return "", fmt.Errorf("下载接口请求失败：%v", err)
	}

	var resp GetDownloadUrlResp
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", fmt.Errorf("下载响应解析失败：%v，body：%s", err, string(respBody))
	}

	if !resp.Success || resp.Code != "0000" {
		return "", fmt.Errorf("下载接口返回错误：%s（码：%s）", resp.Message, resp.Code)
	}

	if resp.Data.DownLoadUrl == "" {
		return "", errors.New("下载接口未返回有效高速直链")
	}

	log.Debugf("grouplink专属接口获取高速直链成功：%s", resp.Data.DownLoadUrl)
	return resp.Data.DownLoadUrl, nil
}

func (y *Yun139GroupLink) getShareInfo(pCaID string, page int) (GetOutLinkInfoResp, error) {
	var resp GetOutLinkInfoResp
	reqBody := GetOutLinkInfoReq{
		LinkId:         y.ShareId,
		Passwd:         y.SharePwd,
		CaSrt:          0,          
		CoSrt:          0,          
		SrtDr:          1,          
		PageNum:        page + 1,   
		PCaId:          pCaID,      
		PageSize:       100,        
		NextPageCursor: nil,       
	}

	body, err := y.httpPost("getOutLinkInfo", reqBody, false)
	if err != nil {
		return resp, err
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		log.Warnf("响应解析失败: %v, body: %s", err, string(body))
		return resp, err
	}

	if !resp.Success || resp.Code != "0000" {
		return resp, errors.New(resp.Message)
	}

	return resp, nil
}

func (y *Yun139GroupLink) list(pCaID string) ([]File, error) {
	files := make([]File, 0)
	probeResp, err := y.getShareInfo("root", 0) 
	if err != nil {
		return nil, fmt.Errorf("根目录探活获取子目录ID失败：%v", err)
	}
	if len(probeResp.Data.AssetsList) == 0 {
		return nil, errors.New("探活响应未返回任何目录/文件项")
	}
	realPCaId := probeResp.Data.AssetsList[0].AssetsId
	if realPCaId == "" {
		return nil, errors.New("探活响应未返回有效真实pCaId")
	}
	log.Debugf("根目录探活成功，获取真实文件目录ID：%s", realPCaId)

	y.UserDomainId = probeResp.Data.OutLink.OwnerUserId
	log.Debugf("探活成功，获取userDomainId：%s", y.UserDomainId)

	page := 0
	for {
		res, err := y.getShareInfo(realPCaId, page)
		if err != nil {
			return nil, fmt.Errorf("真实目录分页查询失败（page=%d）：%v", page, err)
		}
		for _, asset := range res.Data.AssetsList {
			file := fileToObj(asset)
			files = append(files, file)
		}

		if res.Data.NextPageCursor == nil || res.Data.NextPageCursor == "" {
			break
		}
		page++
	}

	log.Debugf("文件列表查询成功，共获取%d个文件", len(files))
	return files, nil
}
