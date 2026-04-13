package _189

import (
	stdpath "path"
	"strings"

	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

func autoRestoreWatcherPath(mountPath string, path string) string {
	fullPath := utils.FixAndCleanPath(stdpath.Join(mountPath, path))
	if fullPath != "/" {
		fullPath = strings.TrimRight(fullPath, "/")
	}
	return fullPath
}
