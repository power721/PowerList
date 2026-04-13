package casfile

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

type Info struct {
	Name       string
	Size       int64
	MD5        string
	SliceMD5   string
	CreateTime string
}

type payload struct {
	Name           string `json:"name"`
	Size           int64  `json:"size"`
	MD5            string `json:"md5"`
	SliceMD5       string `json:"sliceMd5"`
	SliceMD5Legacy string `json:"slice_md5"`
	CreateTime     string `json:"create_time"`
}

func Parse(data []byte) (*Info, error) {
	raw := strings.TrimSpace(strings.TrimPrefix(string(data), "\ufeff"))
	if raw == "" {
		return nil, errors.New("empty .cas content")
	}

	var jsonErr error
	if strings.HasPrefix(raw, "{") && strings.HasSuffix(raw, "}") {
		info, err := parsePayload([]byte(raw))
		if err == nil {
			return info, nil
		}
		jsonErr = err
	}

	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		if jsonErr != nil {
			return nil, fmt.Errorf("parse .cas content: %w", jsonErr)
		}
		return nil, fmt.Errorf("decode .cas content: %w", err)
	}

	info, err := parsePayload(decoded)
	if err != nil {
		return nil, fmt.Errorf("parse .cas payload: %w", err)
	}
	return info, nil
}

func parsePayload(data []byte) (*Info, error) {
	var p payload
	if err := utils.Json.Unmarshal(data, &p); err != nil {
		return nil, err
	}

	sliceMD5 := strings.TrimSpace(p.SliceMD5)
	if sliceMD5 == "" {
		sliceMD5 = strings.TrimSpace(p.SliceMD5Legacy)
	}

	info := &Info{
		Name:       strings.TrimSpace(p.Name),
		Size:       p.Size,
		MD5:        strings.TrimSpace(p.MD5),
		SliceMD5:   sliceMD5,
		CreateTime: strings.TrimSpace(p.CreateTime),
	}
	if err := info.validate(); err != nil {
		return nil, err
	}
	return info, nil
}

func (i *Info) validate() error {
	switch {
	case i.Name == "":
		return errors.New("missing file name")
	case i.Size < 0:
		return errors.New("invalid file size")
	case i.MD5 == "":
		return errors.New("missing file md5")
	case i.SliceMD5 == "":
		return errors.New("missing file slice md5")
	default:
		return nil
	}
}
