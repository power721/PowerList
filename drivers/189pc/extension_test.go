package _189pc

import (
	"context"
	"sync"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
)

var linkSeamMu sync.Mutex

func TestLinkTransferredShareFile(t *testing.T) {
	driver := &Cloud189PC{}
	nonCAS := &Cloud189File{Name: "movie.mkv"}
	casFile := &Cloud189File{Name: "movie.mkv.cas"}

	directCalls := 0
	restoreCalls := 0

	linkSeamMu.Lock()
	origLink := linkTransferObj
	origRestore := restoreTransferredCASAndLink
	linkTransferObj = func(ctx context.Context, y *Cloud189PC, obj model.Obj) (*model.Link, error) {
		directCalls++
		return &model.Link{URL: "https://example.com/direct"}, nil
	}
	restoreTransferredCASAndLink = func(ctx context.Context, y *Cloud189PC, obj model.Obj) (*model.Link, error) {
		restoreCalls++
		return &model.Link{URL: "https://example.com/restored"}, nil
	}
	t.Cleanup(func() {
		linkTransferObj = origLink
		restoreTransferredCASAndLink = origRestore
		linkSeamMu.Unlock()
	})

	link, err := driver.linkTransferredShareFile(context.Background(), nonCAS)
	if err != nil {
		t.Fatalf("link non-cas transfer: %v", err)
	}
	if link.URL != "https://example.com/direct" {
		t.Fatalf("expected direct link, got %q", link.URL)
	}
	if directCalls != 1 || restoreCalls != 0 {
		t.Fatalf("expected direct path only, got direct=%d restore=%d", directCalls, restoreCalls)
	}

	link, err = driver.linkTransferredShareFile(context.Background(), casFile)
	if err != nil {
		t.Fatalf("link cas transfer: %v", err)
	}
	if link.URL != "https://example.com/restored" {
		t.Fatalf("expected restored link, got %q", link.URL)
	}
	if directCalls != 1 || restoreCalls != 1 {
		t.Fatalf("expected cas restore path once, got direct=%d restore=%d", directCalls, restoreCalls)
	}
}
