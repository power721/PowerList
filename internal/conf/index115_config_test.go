package conf

import (
	"path/filepath"
	"testing"
)

func TestDefaultConfigInitializesIndex115Paths(t *testing.T) {
	cfg := DefaultConfig("/tmp/openlist-data")
	if cfg.Index115.DBFile != filepath.Join("/tmp/openlist-data", "index115", "index.db") {
		t.Fatalf("unexpected index115 db path: %q", cfg.Index115.DBFile)
	}
	if cfg.Index115.BleveDir != filepath.Join("/tmp/openlist-data", "index115", "bleve") {
		t.Fatalf("unexpected index115 bleve path: %q", cfg.Index115.BleveDir)
	}
}
