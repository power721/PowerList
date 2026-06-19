package bootstrap

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/blevesearch/bleve/v2"
	_ "github.com/glebarez/go-sqlite"
)

func TestInitIndex115ServiceReturnsErrorWhenManifestMissing(t *testing.T) {
	conf.Conf = conf.DefaultConfig(t.TempDir())
	conf.Conf.Index115.DBFile = filepath.Join(t.TempDir(), "missing.db")
	conf.Conf.Index115.BleveDir = filepath.Join(t.TempDir(), "missing-bleve")

	err := InitIndex115Service(context.Background())
	if err == nil {
		t.Fatal("expected init error")
	}
}

func TestInitIndex115ServiceUsesConfiguredPaths(t *testing.T) {
	rootDir := t.TempDir()
	dbPath := filepath.Join(rootDir, "index.db")
	bleveRoot := rootDir
	index, db := newRuntimeFixture(t, rootDir, dbPath)
	defer func() { _ = db.Close() }()
	if err := index.Close(); err != nil {
		t.Fatalf("index.Close() error = %v", err)
	}

	conf.Conf = conf.DefaultConfig(rootDir)
	conf.Conf.Index115.DBFile = dbPath
	conf.Conf.Index115.BleveDir = bleveRoot

	if err := InitIndex115Service(context.Background()); err != nil {
		t.Fatalf("InitIndex115Service() error = %v", err)
	}
}

func newRuntimeFixture(t *testing.T, rootDir, dbPath string) (closer interface{ Close() error }, db *sql.DB) {
	t.Helper()

	db = openIndex115RuntimeDB(t, dbPath)
	indexDir := filepath.Join(rootDir, "bleve", "index_000001")
	if _, err := db.Exec(`INSERT INTO index_manifest(id, version, index_path, status, built_at, file_count) VALUES (1, 1, ?, 'READY', 1, 1)`, "bleve/index_000001"); err != nil {
		t.Fatalf("insert manifest error = %v", err)
	}
	if _, err := db.Exec(`INSERT INTO share(share_code, receive_code, share_title, status, last_crawled_at) VALUES ('sw1', 'rc1', 'Share One', 'ACTIVE', 1)`); err != nil {
		t.Fatalf("insert share error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(indexDir), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	index, err := bleve.New(indexDir, bleve.NewIndexMapping())
	if err != nil {
		t.Fatalf("bleve.New() error = %v", err)
	}
	return index, db
}

func openIndex115RuntimeDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	stmts := []string{
		`CREATE TABLE share (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			share_code TEXT NOT NULL,
			receive_code TEXT NOT NULL DEFAULT '',
			share_title TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'ACTIVE',
			last_crawled_at INTEGER NOT NULL DEFAULT 0
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
