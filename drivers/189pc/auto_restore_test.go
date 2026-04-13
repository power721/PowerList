package _189pc

import "testing"

func TestParseAutoRestoreExistingCASPaths(t *testing.T) {
	got := parseAutoRestoreExistingCASPaths("/movies\n/movies\nseries\r\n")
	if len(got) != 2 {
		t.Fatalf("expected 2 unique paths, got %v", got)
	}
	if got[0] != "/movies" || got[1] != "/series" {
		t.Fatalf("unexpected parsed paths: %v", got)
	}
}

func TestAutoRestoreWatcherPath(t *testing.T) {
	got := autoRestoreWatcherPath("/mount", "/movies/")
	if got != "/mount/movies" {
		t.Fatalf("unexpected watcher path: %s", got)
	}
}
