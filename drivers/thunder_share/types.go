package thunder_share

import (
	"github.com/OpenListTeam/OpenList/v4/drivers/thunder_browser"
)

type ShareInfo struct {
	Token string                  `json:"pass_code_token"`
	Files []thunder_browser.Files `json:"files"`
}
