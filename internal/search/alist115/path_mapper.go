package alist115

import (
	"strings"
)

// MapPath removes emoji prefix from webdavsim paths
// Converts /🏷️我的115分享/ → /我的115分享/
func MapPath(path string) string {
	if path == "" || path == "/" {
		return path
	}

	// Remove the emoji prefix 🏷️ (keeping the leading /)
	if strings.HasPrefix(path, "/🏷️") {
		return "/" + strings.TrimPrefix(path, "/🏷️")
	}

	return path
}
