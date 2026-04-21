package guangyapan

import (
	"testing"
	"time"
)

func TestNormalizeDeviceID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "valid lowercase", in: "0123456789abcdef0123456789abcdef", want: "0123456789abcdef0123456789abcdef"},
		{name: "valid uppercase and dashes", in: "01234567-89AB-CDEF-0123-456789ABCDEF", want: "0123456789abcdef0123456789abcdef"},
		{name: "invalid length", in: "1234", want: ""},
		{name: "invalid chars", in: "zzzz456789abcdef0123456789abcdef", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeDeviceID(tt.in); got != tt.want {
				t.Fatalf("normalizeDeviceID(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeCaptchaUsername(t *testing.T) {
	if got := normalizeCaptchaUsername("+86 138 0013 8000"); got != "13800138000" {
		t.Fatalf("unexpected captcha username: %q", got)
	}
}

func TestNormalizePhoneE164(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "mainland digits", in: "13800138000", want: "+86 13800138000"},
		{name: "existing e164", in: "+86 13800138000", want: "+86 13800138000"},
		{name: "other format preserved", in: "+1 2025550101", want: "+1 2025550101"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizePhoneE164(tt.in); got != tt.want {
				t.Fatalf("normalizePhoneE164(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeOSSEndpoint(t *testing.T) {
	got := normalizeOSSEndpoint("gyp-bucket.oss-cn-guangzhou.aliyuncs.com", "gyp-bucket")
	want := "https://oss-cn-guangzhou.aliyuncs.com"
	if got != want {
		t.Fatalf("normalizeOSSEndpoint() = %q, want %q", got, want)
	}
}

func TestCalcUploadPartSize(t *testing.T) {
	const mb = int64(1024 * 1024)
	tests := []struct {
		name string
		size int64
		want int64
	}{
		{name: "small", size: 50 * mb, want: 1 * mb},
		{name: "medium", size: 5 * 1024 * mb, want: 2 * mb},
		{name: "large", size: 32 * 1024 * mb, want: 4 * mb},
		{name: "huge", size: 200 * 1024 * mb, want: 8 * mb},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := calcUploadPartSize(tt.size); got != tt.want {
				t.Fatalf("calcUploadPartSize(%d) = %d, want %d", tt.size, got, tt.want)
			}
		})
	}
}

func TestUnixOrZero(t *testing.T) {
	if got := unixOrZero(0); !got.IsZero() {
		t.Fatalf("unixOrZero(0) = %v, want zero time", got)
	}
	got := unixOrZero(1710000000)
	want := time.Unix(1710000000, 0)
	if !got.Equal(want) {
		t.Fatalf("unixOrZero() = %v, want %v", got, want)
	}
}
