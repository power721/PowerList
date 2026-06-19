package index115

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestLinkResolverResolveReceiveCodePrefersNonEmptyRequestValue(t *testing.T) {
	resolver := &LinkResolver{}
	got := resolver.resolveReceiveCode("req-code", "share-code")
	if got != "req-code" {
		t.Fatalf("expected req-code, got %q", got)
	}
}

func TestLinkResolverResolveReceiveCodeFallsBackToShareValue(t *testing.T) {
	resolver := &LinkResolver{}
	got := resolver.resolveReceiveCode("", "share-code")
	if got != "share-code" {
		t.Fatalf("expected share-code, got %q", got)
	}
}

func TestLeaseRegistryRefreshesLease(t *testing.T) {
	registry := newLeaseRegistry(time.Minute)
	first := registry.Touch("cookie-hash:file-id")
	time.Sleep(10 * time.Millisecond)
	second := registry.Touch("cookie-hash:file-id")
	if !second.After(first) {
		t.Fatalf("expected lease to refresh, first=%v second=%v", first, second)
	}
}

func TestLinkResolverResolveSchedulesCleanupWithLease(t *testing.T) {
	client := &fakeShareDownloadClient{
		resolvedLink: ResolvedLink{URL: "https://example.com/play", ExpiredIn: 14400},
		sha1:         "sha1-value",
	}
	resolver := &LinkResolver{
		client: client,
		leases: newLeaseRegistry(5 * time.Millisecond),
		delay:  5 * time.Millisecond,
	}

	file := FileItem{FileID: "file1", ShareCode: "sw1", ReceiveCode: "share-code", SHA1: "sha1-value"}
	link, err := resolver.Resolve(context.Background(), LinkRequest{
		Cookie:    "UID=1;CID=2",
		ShareCode: "sw1",
		FileID:    "file1",
	}, file)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if link.URL != "https://example.com/play" {
		t.Fatalf("unexpected link: %+v", link)
	}

	time.Sleep(30 * time.Millisecond)

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.deletedSHA1) != 1 || client.deletedSHA1[0] != "sha1-value" {
		t.Fatalf("expected cleanup delete call, got %+v", client.deletedSHA1)
	}
}

func TestLinkResolverResolveReturnsErrorWhenClientMissing(t *testing.T) {
	resolver := &LinkResolver{}
	_, err := resolver.Resolve(context.Background(), LinkRequest{
		Cookie:    "cookie",
		ShareCode: "sw1",
		FileID:    "file1",
	}, FileItem{FileID: "file1", ShareCode: "sw1"})
	if !errors.Is(err, ErrLinkClientNotConfigured) {
		t.Fatalf("expected ErrLinkClientNotConfigured, got %v", err)
	}
}

type fakeShareDownloadClient struct {
	mu           sync.Mutex
	resolvedLink ResolvedLink
	sha1         string
	deletedSHA1  []string
}

func (f *fakeShareDownloadClient) ResolveShareLink(ctx context.Context, cookie string, shareCode string, receiveCode string, fileID string) (ResolvedLink, string, error) {
	return f.resolvedLink, f.sha1, nil
}

func (f *fakeShareDownloadClient) DeleteReceivedBySHA1(ctx context.Context, cookie string, sha1 string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletedSHA1 = append(f.deletedSHA1, sha1)
	return nil
}
