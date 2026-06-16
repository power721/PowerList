package alist115

import (
	"testing"
)

func TestMapPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "removes emoji prefix",
			input:    "/🏷️我的115分享/folder/file.txt",
			expected: "/我的115分享/folder/file.txt",
		},
		{
			name:     "handles path without emoji",
			input:    "/我的115分享/folder/file.txt",
			expected: "/我的115分享/folder/file.txt",
		},
		{
			name:     "handles root path with emoji",
			input:    "/🏷️我的115分享/",
			expected: "/我的115分享/",
		},
		{
			name:     "handles root path without emoji",
			input:    "/我的115分享/",
			expected: "/我的115分享/",
		},
		{
			name:     "handles nested paths with emoji",
			input:    "/🏷️我的115分享/深层/目录/文件.mp4",
			expected: "/我的115分享/深层/目录/文件.mp4",
		},
		{
			name:     "handles empty path",
			input:    "",
			expected: "",
		},
		{
			name:     "handles single slash",
			input:    "/",
			expected: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MapPath(tt.input)
			if result != tt.expected {
				t.Errorf("MapPath(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
