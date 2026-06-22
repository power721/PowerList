package index115

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/glebarez/go-sqlite"
)

type testShareRow struct {
	ShareCode     string
	ReceiveCode   string
	ShareTitle    string
	Status        string
	LastCrawledAt int64
}

type testFileRow struct {
	FileID    string
	ShareCode string
	ParentID  string
	Name      string
	Path      string
	Ext       string
	Size      int64
	IsDir     bool
	SHA1      string
	UpdatedAt int64
}

func TestStoreListSharesAggregatesByShareCode(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	store := openTestStore(t, dbPath)

	insertTestShare(t, store.db, testShareRow{
		ShareCode:     "sw1",
		ReceiveCode:   "rc1",
		ShareTitle:    "Share One",
		Status:        "ACTIVE",
		LastCrawledAt: 10,
	})
	insertTestFile(t, store.db, testFileRow{
		FileID:    "dir1",
		ShareCode: "sw1",
		ParentID:  "0",
		Name:      "RootDir",
		Path:      "/RootDir",
		IsDir:     true,
		UpdatedAt: 100,
	})
	insertTestFile(t, store.db, testFileRow{
		FileID:    "file1",
		ShareCode: "sw1",
		ParentID:  "0",
		Name:      "movie.mkv",
		Path:      "/movie.mkv",
		Ext:       ".mkv",
		Size:      1024,
		IsDir:     false,
		UpdatedAt: 200,
	})

	if err := store.RefreshShares(context.Background()); err != nil {
		t.Fatalf("RefreshShares() error = %v", err)
	}

	items, err := store.ListShares(context.Background())
	if err != nil {
		t.Fatalf("ListShares() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 share, got %d", len(items))
	}
	if items[0].ShareCode != "sw1" || items[0].ReceiveCode != "rc1" || items[0].ShareTitle != "Share One" {
		t.Fatalf("unexpected share item: %+v", items[0])
	}
	if items[0].FileCount != 1 || items[0].DirCount != 1 || items[0].UpdatedAt != 200 {
		t.Fatalf("unexpected aggregate counts: %+v", items[0])
	}
}

func TestStoreListChildrenUsesShareFallbackMetadata(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	store := openTestStore(t, dbPath)

	insertTestShare(t, store.db, testShareRow{
		ShareCode:     "sw2",
		ReceiveCode:   "",
		ShareTitle:    "",
		Status:        "ACTIVE",
		LastCrawledAt: 5,
	})
	insertTestFile(t, store.db, testFileRow{
		FileID:    "dir2",
		ShareCode: "sw2",
		ParentID:  "0",
		Name:      "Folder",
		Path:      "/Folder",
		IsDir:     true,
		UpdatedAt: 100,
	})
	// sw2 is a single-root share, so ListChildren at "0" collapses past the
	// redundant root folder and returns its children directly. Add a child to
	// exercise that path and still assert share fallback metadata is applied.
	insertTestFile(t, store.db, testFileRow{
		FileID:    "file2",
		ShareCode: "sw2",
		ParentID:  "dir2",
		Name:      "movie.mkv",
		Path:      "/movie.mkv",
		UpdatedAt: 100,
	})

	if err := store.RefreshShares(context.Background()); err != nil {
		t.Fatalf("RefreshShares() error = %v", err)
	}

	items, err := store.ListChildren(context.Background(), "sw2", "0")
	if err != nil {
		t.Fatalf("ListChildren() error = %v", err)
	}
	if len(items) != 1 || items[0].FileID != "file2" {
		t.Fatalf("expected collapsed child file2, got %+v", items)
	}
	if items[0].ReceiveCode != "" || items[0].ShareTitle != "sw2" {
		t.Fatalf("expected share fallback metadata, got %+v", items[0])
	}
}

func TestStoreFileByIDFindsFile(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	store := openTestStore(t, dbPath)

	insertTestShare(t, store.db, testShareRow{
		ShareCode:     "sw3",
		ReceiveCode:   "rc3",
		ShareTitle:    "Share Three",
		Status:        "ACTIVE",
		LastCrawledAt: 7,
	})
	insertTestFile(t, store.db, testFileRow{
		FileID:    "file3",
		ShareCode: "sw3",
		ParentID:  "0",
		Name:      "ep1.mp4",
		Path:      "/ep1.mp4",
		Ext:       ".mp4",
		Size:      300,
		IsDir:     false,
		SHA1:      "sha1-3",
		UpdatedAt: 123,
	})

	if err := store.RefreshShares(context.Background()); err != nil {
		t.Fatalf("RefreshShares() error = %v", err)
	}

	file, ok, err := store.FileByID(context.Background(), "file3")
	if err != nil {
		t.Fatalf("FileByID() error = %v", err)
	}
	if !ok {
		t.Fatal("expected file to exist")
	}
	if file.FileID != "file3" || file.ShareCode != "sw3" || file.ReceiveCode != "rc3" {
		t.Fatalf("unexpected file result: %+v", file)
	}
}

func openTestStore(t *testing.T, dbPath string) *Store {
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
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("db.Exec(%q) error = %v", stmt, err)
		}
	}

	return &Store{db: db}
}

func insertTestShare(t *testing.T, db *sql.DB, row testShareRow) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO share(share_code, receive_code, share_title, status, last_crawled_at) VALUES (?, ?, ?, ?, ?)`,
		row.ShareCode, row.ReceiveCode, row.ShareTitle, row.Status, row.LastCrawledAt,
	)
	if err != nil {
		t.Fatalf("insert share error = %v", err)
	}
}

func insertTestFile(t *testing.T, db *sql.DB, row testFileRow) {
	t.Helper()
	isDir := 0
	if row.IsDir {
		isDir = 1
	}
	_, err := db.Exec(
		`INSERT INTO file(file_id, share_code, parent_id, name, path, ext, size, is_dir, depth, sha1, updated_at, crawled_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, 0)`,
		row.FileID, row.ShareCode, row.ParentID, row.Name, row.Path, row.Ext, row.Size, isDir, row.SHA1, row.UpdatedAt,
	)
	if err != nil {
		t.Fatalf("insert file error = %v", err)
	}
}

func TestRefreshSharesDerivesRootFolderID(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	store := openTestStore(t, dbPath)

	// sw1: single root dir -> RootFolderID derived as "d1".
	insertTestShare(t, store.db, testShareRow{
		ShareCode: "sw1", ReceiveCode: "rc1", ShareTitle: "Movies",
		Status: "ACTIVE", LastCrawledAt: 1,
	})
	insertTestFile(t, store.db, testFileRow{FileID: "d1", ShareCode: "sw1", ParentID: "0", Name: "Movies", Path: "/Movies", IsDir: true, UpdatedAt: 10})
	insertTestFile(t, store.db, testFileRow{FileID: "f1", ShareCode: "sw1", ParentID: "d1", Name: "a.mkv", Path: "/a.mkv", UpdatedAt: 20})

	// sw2: two root dirs -> "" (no collapse).
	insertTestShare(t, store.db, testShareRow{
		ShareCode: "sw2", ReceiveCode: "rc2", ShareTitle: "Mix",
		Status: "ACTIVE", LastCrawledAt: 1,
	})
	insertTestFile(t, store.db, testFileRow{FileID: "d2a", ShareCode: "sw2", ParentID: "0", Name: "A", Path: "/A", IsDir: true, UpdatedAt: 10})
	insertTestFile(t, store.db, testFileRow{FileID: "d2b", ShareCode: "sw2", ParentID: "0", Name: "B", Path: "/B", IsDir: true, UpdatedAt: 10})

	// sw3: single root that is a FILE -> "" (no folder to collapse).
	insertTestShare(t, store.db, testShareRow{
		ShareCode: "sw3", ReceiveCode: "rc3", ShareTitle: "Lone",
		Status: "ACTIVE", LastCrawledAt: 1,
	})
	insertTestFile(t, store.db, testFileRow{FileID: "f3", ShareCode: "sw3", ParentID: "0", Name: "lone.mkv", Path: "/lone.mkv", UpdatedAt: 10})

	if err := store.RefreshShares(context.Background()); err != nil {
		t.Fatalf("RefreshShares() error = %v", err)
	}
	if got := store.shares["sw1"].RootFolderID; got != "d1" {
		t.Fatalf("sw1 RootFolderID = %q, want %q", got, "d1")
	}
	if got := store.shares["sw2"].RootFolderID; got != "" {
		t.Fatalf("sw2 RootFolderID = %q, want %q (multi-root)", got, "")
	}
	if got := store.shares["sw3"].RootFolderID; got != "" {
		t.Fatalf("sw3 RootFolderID = %q, want %q (single root file)", got, "")
	}
}

func TestListChildrenCollapsesSingleRootFolder(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	store := openTestStore(t, dbPath)

	insertTestShare(t, store.db, testShareRow{
		ShareCode: "sw1", ReceiveCode: "rc1", ShareTitle: "Movies",
		Status: "ACTIVE", LastCrawledAt: 1,
	})
	insertTestFile(t, store.db, testFileRow{FileID: "d1", ShareCode: "sw1", ParentID: "0", Name: "Movies", Path: "/Movies", IsDir: true, UpdatedAt: 10})
	insertTestFile(t, store.db, testFileRow{FileID: "f1", ShareCode: "sw1", ParentID: "d1", Name: "a.mkv", Path: "/a.mkv", Ext: ".mkv", Size: 1024, UpdatedAt: 20})

	if err := store.RefreshShares(context.Background()); err != nil {
		t.Fatalf("RefreshShares() error = %v", err)
	}

	// Share root collapses: the redundant root folder d1 is skipped, f1 returned.
	items, err := store.ListChildren(context.Background(), "sw1", "0")
	if err != nil {
		t.Fatalf("ListChildren() error = %v", err)
	}
	if len(items) != 1 || items[0].FileID != "f1" {
		t.Fatalf("expected collapsed child f1 only, got %+v", items)
	}

	// resolveFullPath terminates at the root folder: path has no "Movies" prefix.
	file, ok, err := store.FileWithFullPath(context.Background(), "f1")
	if err != nil {
		t.Fatalf("FileWithFullPath() error = %v", err)
	}
	if !ok {
		t.Fatal("expected file f1 to exist")
	}
	if file.Path != "/a.mkv" {
		t.Fatalf("Path = %q, want %q (root folder name must be dropped)", file.Path, "/a.mkv")
	}
}

func TestListChildrenNoCollapseWhenMultiRoot(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	store := openTestStore(t, dbPath)

	insertTestShare(t, store.db, testShareRow{
		ShareCode: "sw1", ReceiveCode: "rc1", ShareTitle: "Mix",
		Status: "ACTIVE", LastCrawledAt: 1,
	})
	insertTestFile(t, store.db, testFileRow{FileID: "d1", ShareCode: "sw1", ParentID: "0", Name: "A", Path: "/A", IsDir: true, UpdatedAt: 10})
	insertTestFile(t, store.db, testFileRow{FileID: "d2", ShareCode: "sw1", ParentID: "0", Name: "B", Path: "/B", IsDir: true, UpdatedAt: 10})

	if err := store.RefreshShares(context.Background()); err != nil {
		t.Fatalf("RefreshShares() error = %v", err)
	}

	items, err := store.ListChildren(context.Background(), "sw1", "0")
	if err != nil {
		t.Fatalf("ListChildren() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected both root dirs (no collapse), got %d: %+v", len(items), items)
	}
}
