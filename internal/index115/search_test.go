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

func TestSearcherSearchPreservesBleveOrder(t *testing.T) {
	fixture := newSearchFixture(t)

	fixture.indexDoc(t, "f2", map[string]any{
		"name":       "beta movie",
		"path":       "/beta movie",
		"share_code": "sw1",
	})
	fixture.indexDoc(t, "f1", map[string]any{
		"name":       "alpha movie",
		"path":       "/alpha movie",
		"share_code": "sw1",
	})

	insertTestShare(t, fixture.store.db, testShareRow{
		ShareCode:     "sw1",
		ReceiveCode:   "rc1",
		ShareTitle:    "Share One",
		Status:        "ACTIVE",
		LastCrawledAt: 10,
	})
	insertTestFile(t, fixture.store.db, testFileRow{
		FileID:    "f1",
		ShareCode: "sw1",
		ParentID:  "0",
		Name:      "alpha movie",
		Path:      "/alpha movie",
	})
	insertTestFile(t, fixture.store.db, testFileRow{
		FileID:    "f2",
		ShareCode: "sw1",
		ParentID:  "0",
		Name:      "beta movie",
		Path:      "/beta movie",
	})
	if err := fixture.store.RefreshShares(context.Background()); err != nil {
		t.Fatalf("RefreshShares() error = %v", err)
	}

	items, total, err := fixture.searcher.Search(context.Background(), SearchRequest{
		Query:   "movie",
		Page:    1,
		PerPage: 2,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total 2, got %d", total)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].FileID != "f2" || items[1].FileID != "f1" {
		t.Fatalf("unexpected ordering: %+v", items)
	}
}

func TestSearcherSearchDropsMissingSQLiteRows(t *testing.T) {
	fixture := newSearchFixture(t)

	fixture.indexDoc(t, "missing", map[string]any{
		"name":       "ghost movie",
		"path":       "/ghost movie",
		"share_code": "sw1",
	})

	items, total, err := fixture.searcher.Search(context.Background(), SearchRequest{
		Query:   "ghost",
		Page:    1,
		PerPage: 10,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if total != 0 {
		t.Fatalf("expected resolved total 0, got %d", total)
	}
	if len(items) != 0 {
		t.Fatalf("expected empty resolved page, got %+v", items)
	}
}

func TestSearcherSearchBackfillsMissingRowsToKeepPagesStable(t *testing.T) {
	fixture := newSearchFixture(t)

	fixture.indexDoc(t, "f1", map[string]any{
		"name":       "movie one",
		"path":       "/movie one",
		"share_code": "sw1",
	})
	fixture.indexDoc(t, "missing", map[string]any{
		"name":       "movie missing",
		"path":       "/movie missing",
		"share_code": "sw1",
	})
	fixture.indexDoc(t, "f2", map[string]any{
		"name":       "movie two",
		"path":       "/movie two",
		"share_code": "sw1",
	})

	insertTestShare(t, fixture.store.db, testShareRow{
		ShareCode:     "sw1",
		ReceiveCode:   "rc1",
		ShareTitle:    "Share One",
		Status:        "ACTIVE",
		LastCrawledAt: 10,
	})
	insertTestFile(t, fixture.store.db, testFileRow{
		FileID:    "f1",
		ShareCode: "sw1",
		ParentID:  "0",
		Name:      "movie one",
		Path:      "/movie one",
	})
	insertTestFile(t, fixture.store.db, testFileRow{
		FileID:    "f2",
		ShareCode: "sw1",
		ParentID:  "0",
		Name:      "movie two",
		Path:      "/movie two",
	})
	if err := fixture.store.RefreshShares(context.Background()); err != nil {
		t.Fatalf("RefreshShares() error = %v", err)
	}

	items, total, err := fixture.searcher.Search(context.Background(), SearchRequest{
		Query:   "movie",
		Page:    2,
		PerPage: 1,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if total != 2 {
		t.Fatalf("expected resolved total 2, got %d", total)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].FileID != "f2" {
		t.Fatalf("expected backfilled second page to return f2, got %+v", items[0])
	}
}

type searchFixture struct {
	store    *Store
	searcher *Searcher
	index    bleve.Index
}

func newSearchFixture(t *testing.T) *searchFixture {
	t.Helper()

	rootDir := t.TempDir()
	dbPath := filepath.Join(rootDir, "index.db")
	store := openTestStore(t, dbPath)

	indexPath := filepath.Join(rootDir, "bleve")
	index, err := bleve.New(indexPath, bleve.NewIndexMapping())
	if err != nil {
		t.Fatalf("bleve.New() error = %v", err)
	}
	t.Cleanup(func() { _ = index.Close() })
	t.Cleanup(func() { _ = os.RemoveAll(indexPath) })

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return &searchFixture{
		store:    store,
		searcher: &Searcher{store: store, index: index},
		index:    index,
	}
}

func (f *searchFixture) indexDoc(t *testing.T, id string, doc map[string]any) {
	t.Helper()
	if err := f.index.Index(id, doc); err != nil {
		t.Fatalf("index.Index(%q) error = %v", id, err)
	}
}
