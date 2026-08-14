package index115

import (
	"context"
	"path/filepath"
	"testing"
)

// TestRefreshSharesReadsVisibleColumn: an index that ships the visible column
// marks bulk (search-only) shares Visible=false while curated shares stay
// browsable.
func TestRefreshSharesReadsVisibleColumn(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	store := openTestStore(t, dbPath)
	if _, err := store.db.Exec(`ALTER TABLE share ADD COLUMN visible INTEGER NOT NULL DEFAULT 1`); err != nil {
		t.Fatalf("add visible column: %v", err)
	}

	insertTestShare(t, store.db, testShareRow{ShareCode: "swCur", ReceiveCode: "r1", ShareTitle: "Curated", Status: "ACTIVE"})
	insertTestShare(t, store.db, testShareRow{ShareCode: "swBulk", ReceiveCode: "r2", ShareTitle: "Bulk", Status: "ACTIVE"})
	if _, err := store.db.Exec(`UPDATE share SET visible = 0 WHERE share_code = 'swBulk'`); err != nil {
		t.Fatal(err)
	}
	insertTestFile(t, store.db, testFileRow{FileID: "f1", ShareCode: "swCur", ParentID: "0", Name: "a.mkv", UpdatedAt: 1})
	insertTestFile(t, store.db, testFileRow{FileID: "f2", ShareCode: "swBulk", ParentID: "0", Name: "b.mkv", UpdatedAt: 2})

	if err := store.RefreshShares(context.Background()); err != nil {
		t.Fatalf("RefreshShares() error = %v", err)
	}
	if !store.shares["swCur"].Visible {
		t.Fatalf("swCur.Visible = false, want true")
	}
	if store.shares["swBulk"].Visible {
		t.Fatalf("swBulk.Visible = true, want false")
	}

	summaries, err := store.ListShares(context.Background())
	if err != nil {
		t.Fatalf("ListShares() error = %v", err)
	}
	for _, s := range summaries {
		switch s.ShareCode {
		case "swCur":
			if !s.Visible {
				t.Fatalf("ListShares swCur.Visible = false, want true")
			}
		case "swBulk":
			if s.Visible {
				t.Fatalf("ListShares swBulk.Visible = true, want false")
			}
		}
	}

	// direct browse into the search-only share still works
	items, err := store.ListChildren(context.Background(), "swBulk", "0")
	if err != nil {
		t.Fatalf("ListChildren(swBulk) error = %v", err)
	}
	if len(items) != 1 || items[0].Name != "b.mkv" {
		t.Fatalf("swBulk children = %+v, want b.mkv (search-only share stays browsable directly)", items)
	}
}

// TestRefreshSharesDefaultsVisibleWithoutColumn: an older index without the
// visible column keeps every share browsable.
func TestRefreshSharesDefaultsVisibleWithoutColumn(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")
	store := openTestStore(t, dbPath) // schema has no visible column

	insertTestShare(t, store.db, testShareRow{ShareCode: "sw1", ReceiveCode: "r1", ShareTitle: "S", Status: "ACTIVE"})
	insertTestFile(t, store.db, testFileRow{FileID: "f1", ShareCode: "sw1", ParentID: "0", Name: "a.mkv", UpdatedAt: 1})

	if err := store.RefreshShares(context.Background()); err != nil {
		t.Fatalf("RefreshShares() error = %v", err)
	}
	if !store.shares["sw1"].Visible {
		t.Fatalf("sw1.Visible = false, want true fallback without column")
	}
}

// TestServiceBrowseHidesSearchOnlyShares: homepage and group listings exclude
// Visible=false shares.
func TestServiceBrowseHidesSearchOnlyShares(t *testing.T) {
	svc := &Service{
		store: stubStore{
			groups: []GroupInfo{{ID: 1, Name: "欧美剧"}},
			shares: []ShareSummary{
				{ShareCode: "swBulkLoose", ShareTitle: "Hidden", GroupID: 0, Visible: false},
				{ShareCode: "swL", ShareTitle: "Loose", GroupID: 0, Visible: true},
				{ShareCode: "swBulkInGroup", ShareTitle: "HiddenMember", GroupID: 1, Visible: false},
				{ShareCode: "swG", ShareTitle: "Member", GroupID: 1, Visible: true},
			},
		},
	}

	items, err := svc.Browse(context.Background(), BrowseRequest{})
	if err != nil {
		t.Fatalf("Browse() error = %v", err)
	}
	if len(items) != 2 { // group dir + swL
		t.Fatalf("homepage items = %+v, want group + swL only", items)
	}
	if items[1].ShareCode != "swL" {
		t.Fatalf("homepage loose item = %+v, want swL", items[1])
	}

	members, err := svc.Browse(context.Background(), BrowseRequest{ShareCode: "grp1"})
	if err != nil {
		t.Fatalf("Browse(grp1) error = %v", err)
	}
	if len(members) != 1 || members[0].ShareCode != "swG" {
		t.Fatalf("grp1 members = %+v, want [swG] only", members)
	}
}
