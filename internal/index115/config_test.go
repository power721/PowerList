package index115

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/blevesearch/bleve/v2"
	_ "github.com/glebarez/go-sqlite"
)

func TestNewRuntimeRejectsMissingManifest(t *testing.T) {
	rootDir := t.TempDir()
	dbPath := filepath.Join(rootDir, "index.db")
	openRuntimeDB(t, dbPath)

	store, searcher, err := NewRuntime(context.Background(), dbPath, rootDir)
	if err == nil {
		t.Fatalf("expected error, got store=%v searcher=%v", store, searcher)
	}
}

func TestNewRuntimeOpensConfiguredManifestIndex(t *testing.T) {
	rootDir := t.TempDir()
	dbPath := filepath.Join(rootDir, "index.db")
	db := openRuntimeDB(t, dbPath)

	indexDir := filepath.Join(rootDir, "bleve", "index_000001")
	if err := os.MkdirAll(filepath.Dir(indexDir), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	index, err := bleve.New(indexDir, bleve.NewIndexMapping())
	if err != nil {
		t.Fatalf("bleve.New() error = %v", err)
	}
	if err := index.Index("f1", map[string]any{"name": "movie", "share_code": "sw1"}); err != nil {
		t.Fatalf("index.Index() error = %v", err)
	}
	if err := index.Close(); err != nil {
		t.Fatalf("index.Close() error = %v", err)
	}

	if _, err := db.Exec(`INSERT INTO index_manifest(id, version, index_path, status, built_at, file_count) VALUES (1, 1, ?, 'READY', 1, 1)`, "bleve/index_000001"); err != nil {
		t.Fatalf("insert manifest error = %v", err)
	}
	if _, err := db.Exec(`INSERT INTO share(share_code, receive_code, share_title, status, last_crawled_at) VALUES ('sw1', 'rc1', 'Share One', 'ACTIVE', 1)`); err != nil {
		t.Fatalf("insert share error = %v", err)
	}

	store, searcher, err := NewRuntime(context.Background(), dbPath, filepath.Join(rootDir, "bleve"))
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	if store == nil || searcher == nil || searcher.index == nil {
		t.Fatalf("expected initialized runtime, got store=%v searcher=%v", store, searcher)
	}
}

func TestNewRuntimeFallsBackToBleveDirBaseForAbsoluteManifestIndexPath(t *testing.T) {
	rootDir := t.TempDir()
	dbPath := filepath.Join(rootDir, "index.db")
	db := openRuntimeDB(t, dbPath)

	indexDir := filepath.Join(rootDir, "bleve", "index_000001")
	if err := os.MkdirAll(filepath.Dir(indexDir), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	index, err := bleve.New(indexDir, bleve.NewIndexMapping())
	if err != nil {
		t.Fatalf("bleve.New() error = %v", err)
	}
	if err := index.Close(); err != nil {
		t.Fatalf("index.Close() error = %v", err)
	}

	manifestPath := filepath.Join("/build-host/indexes", "index_000001")
	if _, err := db.Exec(`INSERT INTO index_manifest(id, version, index_path, status, built_at, file_count) VALUES (1, 1, ?, 'READY', 1, 1)`, manifestPath); err != nil {
		t.Fatalf("insert manifest error = %v", err)
	}
	if _, err := db.Exec(`INSERT INTO share(share_code, receive_code, share_title, status, last_crawled_at) VALUES ('sw1', 'rc1', 'Share One', 'ACTIVE', 1)`); err != nil {
		t.Fatalf("insert share error = %v", err)
	}

	store, searcher, err := NewRuntime(context.Background(), dbPath, filepath.Join(rootDir, "bleve"))
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	if store == nil || searcher == nil || searcher.index == nil {
		t.Fatalf("expected initialized runtime, got store=%v searcher=%v", store, searcher)
	}
}

func openRuntimeDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	stmts := []string{
		`CREATE TABLE share (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			share_code TEXT NOT NULL,
			receive_code TEXT NOT NULL DEFAULT '',
			share_title TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'ACTIVE',
			last_crawled_at INTEGER NOT NULL DEFAULT 0,
			group_id INTEGER
		);`,
		`CREATE TABLE share_group (
			group_id   INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			sort_order INTEGER NOT NULL
		);`,
		`CREATE TABLE file (
			file_id TEXT PRIMARY KEY,
			share_code TEXT NOT NULL,
			parent_id TEXT NOT NULL,
			name TEXT NOT NULL,
			path TEXT NOT NULL,
			ext TEXT NOT NULL DEFAULT '',
			size INTEGER NOT NULL DEFAULT 0,
			is_dir INTEGER NOT NULL DEFAULT 0,
			depth INTEGER NOT NULL DEFAULT 0,
			sha1 TEXT NOT NULL DEFAULT '',
			updated_at INTEGER,
			crawled_at INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE TABLE index_manifest (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			version INTEGER NOT NULL,
			index_path TEXT NOT NULL,
			status TEXT NOT NULL,
			built_at INTEGER NOT NULL,
			file_count INTEGER NOT NULL
		);`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("db.Exec(%q) error = %v", stmt, err)
		}
	}
	return db
}
