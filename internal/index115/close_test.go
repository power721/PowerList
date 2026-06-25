package index115

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/glebarez/go-sqlite"
)

// TestStoreCloseNilSafe guards the nil/zero receiver paths that reload relies on
// (it closes the previous store unconditionally without tracking open state).
func TestStoreCloseNilSafe(t *testing.T) {
	var s *Store
	if err := s.Close(); err != nil {
		t.Fatalf("nil Store.Close() = %v, want nil", err)
	}
	if err := (&Store{}).Close(); err != nil {
		t.Fatalf("zero Store.Close() = %v, want nil", err)
	}
}

func TestSearcherCloseNilSafe(t *testing.T) {
	var s *Searcher
	if err := s.Close(); err != nil {
		t.Fatalf("nil Searcher.Close() = %v, want nil", err)
	}
	if err := (&Searcher{}).Close(); err != nil {
		t.Fatalf("zero Searcher.Close() = %v, want nil", err)
	}
}

// TestStoreCloseClosesDB verifies Close releases the underlying handle so a
// reload does not leak file descriptors across repeated swaps.
func TestStoreCloseClosesDB(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	store := OpenStore(db)
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := db.PingContext(context.Background()); err == nil {
		t.Fatalf("expected error pinging closed db, got nil")
	}
}
