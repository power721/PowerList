package guangyapan

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"strings"

	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

type GuangYaPan struct {
	model.Storage
	Addition
}

func (d *GuangYaPan) Config() driver.Config {
	return config
}

func (d *GuangYaPan) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *GuangYaPan) Init(ctx context.Context) error {
	return nil
}

func (d *GuangYaPan) Drop(ctx context.Context) error {
	return nil
}

func (d *GuangYaPan) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	return nil, errs.NotImplement
}

func (d *GuangYaPan) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	return nil, errs.NotImplement
}

func normalizeCaptchaUsername(phone string) string {
	p := strings.TrimSpace(phone)
	p = strings.ReplaceAll(p, " ", "")
	p = strings.TrimPrefix(p, "+")
	b := make([]rune, 0, len(p))
	for _, ch := range p {
		if ch >= '0' && ch <= '9' {
			b = append(b, ch)
		}
	}
	digits := string(b)
	if strings.HasPrefix(digits, "86") && len(digits) > 11 {
		digits = digits[2:]
	}
	return digits
}

func normalizePhoneE164(phone string) string {
	p := strings.TrimSpace(phone)
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "+") {
		compact := strings.ReplaceAll(p, " ", "")
		if strings.HasPrefix(compact, "+86") && len(compact) > 3 {
			return "+86 " + strings.TrimPrefix(compact, "+86")
		}
		return p
	}
	digits := normalizeCaptchaUsername(p)
	if len(digits) == 11 {
		return "+86 " + digits
	}
	return p
}

func normalizeDeviceID(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, "-", "")
	if len(v) != 32 {
		return ""
	}
	for _, ch := range v {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return ""
		}
	}
	return v
}

func randomDeviceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "0123456789abcdef0123456789abcdef"
	}
	return hex.EncodeToString(b)
}

func normalizeOSSEndpoint(endpoint, bucket string) string {
	ep := strings.TrimSpace(endpoint)
	if ep == "" {
		return ep
	}
	if !strings.HasPrefix(ep, "http://") && !strings.HasPrefix(ep, "https://") {
		ep = "https://" + ep
	}
	u, err := url.Parse(ep)
	if err != nil || u.Host == "" {
		return ep
	}
	if prefix := strings.TrimSpace(bucket); prefix != "" && strings.HasPrefix(u.Host, prefix+".") {
		u.Host = strings.TrimPrefix(u.Host, prefix+".")
	}
	return u.String()
}

func calcUploadPartSize(size int64) int64 {
	const (
		mb = int64(1024 * 1024)
		gb = int64(1024 * 1024 * 1024)
	)
	switch {
	case size <= 100*mb:
		return 1 * mb
	case size <= 16*gb:
		return 2 * mb
	case size <= 160*gb:
		return 4 * mb
	default:
		return 8 * mb
	}
}
