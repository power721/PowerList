package _123_open

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/OpenListTeam/OpenList/v4/drivers/base"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/internal/token"
	jsoniter "github.com/json-iterator/go"
)

var (
	AccessToken = "https://open-api.123pan.com/api/v1/access_token"
)

func expiresInToExpiredAt(expiresIn int64) (time.Time, error) {
	if expiresIn <= 0 {
		return time.Time{}, errors.New("invalid expires_in from official API")
	}
	return time.Now().UTC().Add(time.Duration(expiresIn) * time.Second), nil
}

type tokenManager struct {
	// accessToken  string
	expiredAt    time.Time
	mu           sync.Mutex
	blockRefresh bool
}

func (d *Open123) getAccessToken(forceRefresh bool) (string, error) {
	tm := d.tm
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if tm.blockRefresh {
		return "", errors.New("Authentication expired")
	}
	if !forceRefresh && d.AccessToken != "" && time.Now().Before(tm.expiredAt.Add(-5*time.Minute)) {
		return d.AccessToken, nil
	}
	if err := d.flushAccessToken(); err != nil {
		// token expired and failed to refresh, block further refresh attempts
		tm.blockRefresh = true
		return "", err
	}
	return d.AccessToken, nil
}

func (d *Open123) flushAccessToken() error {
	// OAuthProxy:用户无自有 client_id,经 oauth 代理刷新(扫码授权的 refresh_token)。
	// 代理地址由消费端通过 OAuthServer 传入,留空时回退默认。
	if d.OAuthProxy && d.RefreshToken != "" {
		oauthServer := strings.TrimSpace(d.OAuthServer)
		if oauthServer == "" {
			oauthServer = "https://oauth.litepan.top"
		}
		oauthRefreshURL := strings.TrimRight(oauthServer, "/") + "/api/oauth/refresh"
		resp, err := base.RestyClient.R().
			SetHeader("Content-Type", "application/json").
			SetBody(base.Json{"driver_type": "123云盘Open", "refresh_token": d.RefreshToken}).
			Post(oauthRefreshURL)
		if err != nil {
			return err
		}
		body := resp.Body()
		at := jsoniter.Get(body, "data", "access_token").ToString()
		if at == "" {
			at = jsoniter.Get(body, "data", "token_data", "access_token").ToString()
		}
		if at == "" {
			at = jsoniter.Get(body, "access_token").ToString()
		}
		if at == "" {
			msg := jsoniter.Get(body, "message").ToString()
			if msg == "" {
				msg = jsoniter.Get(body, "data", "error").ToString()
			}
			if msg == "" {
				msg = "empty access_token from oauth proxy"
			}
			return fmt.Errorf("123 open oauth refresh failed: %s", msg)
		}
		rt := jsoniter.Get(body, "data", "refresh_token").ToString()
		if rt == "" {
			rt = jsoniter.Get(body, "refresh_token").ToString()
		}
		if rt == "" {
			rt = d.RefreshToken
		}
		ei := jsoniter.Get(body, "data", "expires_in").ToInt()
		if ei <= 0 {
			ei = jsoniter.Get(body, "expires_in").ToInt()
		}
		expiredAt, err := expiresInToExpiredAt(int64(ei))
		if err != nil {
			return err
		}
		d.AccessToken = at
		d.RefreshToken = rt
		d.tm.expiredAt = expiredAt
		op.MustSaveDriverStorage(d)
		// 同步(可能轮换的)refresh_token 回 alist-tvbox,避免其重启后用旧 refresh_token。
		token.SaveAccountToken(conf.OPEN123, d.RefreshToken, int(d.ID))
		d.tm.blockRefresh = false
		return nil
	}

	// Official app renewapi response contains access_token, refresh_token and expires_in.
	if d.UseOnlineAPI && d.RefreshToken != "" && len(d.APIAddress) > 0 {
		var resp RefreshTokenResp
		_, err := base.RestyClient.R().
			SetResult(&resp).
			SetQueryParams(map[string]string{
				"refresh_ui": d.RefreshToken,
				"server_use": "true",
				"driver_txt": "123cloud_oa",
			}).
			Get(d.APIAddress)
		if err != nil {
			return err
		}

		if resp.AccessToken == "" || resp.RefreshToken == "" {
			errMessage := resp.ErrorDescription
			if errMessage == "" {
				errMessage = resp.Text
			}
			if errMessage == "" {
				errMessage = resp.Message
			}
			if errMessage == "" {
				errMessage = resp.Error
			}
			if errMessage != "" {
				return fmt.Errorf("failed to refresh token: %s", errMessage)
			}
			return fmt.Errorf("empty access_token or refresh_token returned from official API")
		}
		expiredAt, err := expiresInToExpiredAt(resp.ExpiresIn)
		if err != nil {
			return err
		}

		d.AccessToken = resp.AccessToken
		d.RefreshToken = resp.RefreshToken
		d.tm.expiredAt = expiredAt
		op.MustSaveDriverStorage(d)
		d.tm.blockRefresh = false
		return nil
	}

	// Developer API response contains code/message/data(accessToken, expiredAt).
	if d.ClientID != "" && d.ClientSecret != "" {
		req := base.RestyClient.R()
		req.SetHeaders(map[string]string{
			"platform":     "open_platform",
			"Content-Type": "application/json",
		})
		var resp AccessTokenResp
		req.SetBody(base.Json{
			"clientID":     d.ClientID,
			"clientSecret": d.ClientSecret,
		})
		req.SetResult(&resp)
		_, err := req.Execute(http.MethodPost, AccessToken)
		if err != nil {
			return err
		}
		if resp.Code != 0 {
			return fmt.Errorf("get access token failed: %s", resp.Message)
		}
		if resp.Data.AccessToken == "" || resp.Data.ExpiredAt == "" {
			return errors.New("invalid token payload from developer API")
		}
		expiredAt, err := time.Parse(time.RFC3339, resp.Data.ExpiredAt)
		if err != nil {
			return fmt.Errorf("parse expire time failed: %w", err)
		}
		d.AccessToken = resp.Data.AccessToken
		d.tm.expiredAt = expiredAt.UTC()
		op.MustSaveDriverStorage(d)
		d.tm.blockRefresh = false
		return nil
	}
	return errors.New("no valid authentication method available")
}
