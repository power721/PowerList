package _123rapid

import (
	"strings"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
)

func sha1Hex() string { return strings.Repeat("a", utils.SHA1.Width) }
func md5Hex() string  { return strings.Repeat("b", utils.MD5.Width) }

func TestSourceValid(t *testing.T) {
	cases := []struct {
		name string
		src  Source
		want bool
	}{
		{"sha1 ok", Source{HashType: utils.SHA1, Hash: sha1Hex(), Name: "a.mp4", Size: 1}, true},
		{"md5 ok", Source{HashType: utils.MD5, Hash: md5Hex(), Name: "a.mp4", Size: 1}, true},
		{"sha1 upper ok", Source{HashType: utils.SHA1, Hash: strings.ToUpper(sha1Hex()), Name: "a", Size: 2}, true},
		{"nil hashtype", Source{HashType: nil, Hash: sha1Hex(), Name: "a", Size: 1}, false},
		{"empty name", Source{HashType: utils.SHA1, Hash: sha1Hex(), Name: "", Size: 1}, false},
		{"zero size", Source{HashType: utils.SHA1, Hash: sha1Hex(), Name: "a", Size: 0}, false},
		{"sha1 too short", Source{HashType: utils.SHA1, Hash: "abc", Name: "a", Size: 1}, false},
		{"md5 too long", Source{HashType: utils.MD5, Hash: sha1Hex(), Name: "a", Size: 1}, false},
		{"unknown type", Source{HashType: &fakeHashType, Hash: sha1Hex(), Name: "a", Size: 1}, false},
		{"empty hash", Source{HashType: utils.SHA1, Hash: "", Name: "a", Size: 1}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.src.valid(); got != c.want {
				t.Fatalf("valid=%v want %v", got, c.want)
			}
		})
	}
}

// fakeHashType 既非 SHA1 也非 MD5,用于校验 valid() 的 default 分支。
var fakeHashType = utils.HashType{}

func TestSourceCacheKey(t *testing.T) {
	s := Source{HashType: utils.MD5, Hash: strings.ToUpper(md5Hex()), Name: "x", Size: 7}
	k := s.cacheKey()
	// 缓存键必须小写化 hash,且含 width 与 size,避免不同文件/大小撞键。
	if !strings.Contains(k, strings.ToLower(md5Hex())) {
		t.Fatalf("cacheKey %q 未小写化 hash", k)
	}
	if !strings.HasSuffix(k, ":7") {
		t.Fatalf("cacheKey %q 未含 size", k)
	}
	// 相同源稳定。
	if s.cacheKey() != k {
		t.Fatalf("cacheKey 不稳定")
	}
	// 不同 size 不同键。
	s2 := s
	s2.Size = 8
	if s2.cacheKey() == k {
		t.Fatalf("不同 size 应有不同 cacheKey")
	}
}
