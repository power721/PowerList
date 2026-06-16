package alist115

import (
	"strings"
)

// MapPath removes the specific emoji prefix added by webdavsim to 115 cloud storage paths.
// webdavsim prepends 🏷️ to mounted 115 share paths, and this function normalizes them
// for indexing and search.
//
// Supported transformation:
//   /🏷️我的115分享/ → /我的115分享/
//
// Note: Only the specific emoji 🏷️ from webdavsim is handled. Other emojis in paths
// are intentionally left unchanged as they may be legitimate user content.
//
// Example:
//   MapPath("/🏷️我的115分享/folder/file.txt") → "/我的115分享/folder/file.txt"
//   MapPath("/other-emoji😀/file.txt")        → "/other-emoji😀/file.txt" (unchanged)
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
