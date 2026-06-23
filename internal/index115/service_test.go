package index115

import (
	"context"
	"errors"
	"testing"
)

func TestServiceBrowseRootReturnsShares(t *testing.T) {
	svc := &Service{
		store: stubStore{
			shares: []ShareSummary{{ShareCode: "sw1", ShareTitle: "S1", ReceiveCode: "rc1"}},
		},
	}

	items, err := svc.Browse(context.Background(), BrowseRequest{})
	if err != nil {
		t.Fatalf("Browse() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].ShareCode != "sw1" || items[0].Name != "S1" || !items[0].IsDir {
		t.Fatalf("unexpected root item: %+v", items[0])
	}
}

func TestServiceSearchRejectsEmptyQuery(t *testing.T) {
	svc := &Service{}
	_, _, err := svc.Search(context.Background(), SearchRequest{})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestServiceSearchReturnsUnavailableWhenSearcherMissing(t *testing.T) {
	svc := &Service{}
	_, _, err := svc.Search(context.Background(), SearchRequest{Query: "movie"})
	if !errors.Is(err, ErrSearchUnavailable) {
		t.Fatalf("expected ErrSearchUnavailable, got %v", err)
	}
}

func TestServiceLinkRejectsDirectory(t *testing.T) {
	svc := &Service{
		store: stubStore{
			file: FileItem{FileID: "dir1", ShareCode: "sw1", IsDir: true},
			ok:   true,
		},
		linker: &LinkResolver{client: &fakeShareDownloadClient{}},
	}

	_, err := svc.Link(context.Background(), LinkRequest{
		Cookie:    "cookie",
		ShareCode: "sw1",
		FileID:    "dir1",
	})
	if err == nil {
		t.Fatal("expected directory link error")
	}
}

type stubStore struct {
	shares []ShareSummary
	groups []GroupInfo
	items  []FileItem
	file   FileItem
	ok     bool
	err    error
}

func (s stubStore) ListShares(ctx context.Context) ([]ShareSummary, error) {
	return s.shares, s.err
}

func (s stubStore) ListGroups(ctx context.Context) ([]GroupInfo, error) {
	return s.groups, s.err
}

func (s stubStore) ListChildren(ctx context.Context, shareCode, parentID string) ([]FileItem, error) {
	return s.items, s.err
}

func (s stubStore) FileByID(ctx context.Context, fileID string) (FileItem, bool, error) {
	return s.file, s.ok, s.err
}

func (s stubStore) FileWithFullPath(ctx context.Context, fileID string) (FileItem, bool, error) {
	return s.file, s.ok, s.err
}

type stubSearcher struct {
	items []FileItem
	total int
	err   error
}

func (s stubSearcher) Search(ctx context.Context, req SearchRequest) ([]FileItem, int, error) {
	return s.items, s.total, s.err
}

type stubResolver struct {
	link ResolvedLink
	err  error
}

func (s stubResolver) Resolve(ctx context.Context, req LinkRequest, file FileItem) (ResolvedLink, error) {
	if s.err != nil {
		return ResolvedLink{}, s.err
	}
	return s.link, nil
}

func TestServiceLinkRejectsMissingFile(t *testing.T) {
	svc := &Service{
		store:  stubStore{ok: false},
		linker: stubResolver{},
	}

	_, err := svc.Link(context.Background(), LinkRequest{
		Cookie:    "cookie",
		ShareCode: "sw1",
		FileID:    "missing",
	})
	if !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("expected ErrFileNotFound, got %v", err)
	}
}

func TestServiceBrowseRootListsGroupsThenLooseShares(t *testing.T) {
	svc := &Service{
		store: stubStore{
			groups: []GroupInfo{{ID: 1, Name: "欧美剧"}, {ID: 2, Name: "纪录片"}},
			shares: []ShareSummary{
				{ShareCode: "swG", ShareTitle: "Grouped", GroupID: 1},
				{ShareCode: "swL", ShareTitle: "Loose", GroupID: 0},
			},
		},
	}

	items, err := svc.Browse(context.Background(), BrowseRequest{})
	if err != nil {
		t.Fatalf("Browse() error = %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 root items (2 groups + 1 loose), got %d: %+v", len(items), items)
	}
	if items[0].ShareCode != "grp1" || items[0].Name != "欧美剧" || !items[0].IsDir {
		t.Fatalf("group item 0 = %+v", items[0])
	}
	if items[1].ShareCode != "grp2" || items[1].Name != "纪录片" {
		t.Fatalf("group item 1 = %+v", items[1])
	}
	if items[2].ShareCode != "swL" || items[2].Name != "Loose" {
		t.Fatalf("loose item = %+v (grouped swG must NOT appear at root)", items[2])
	}
}

func TestServiceBrowseGroupSentinelListsMembersOnly(t *testing.T) {
	svc := &Service{
		store: stubStore{
			shares: []ShareSummary{
				{ShareCode: "swG1", ShareTitle: "M1", GroupID: 1},
				{ShareCode: "swG2", ShareTitle: "M2", GroupID: 1},
				{ShareCode: "swO", ShareTitle: "Other", GroupID: 2},
			},
		},
	}

	items, err := svc.Browse(context.Background(), BrowseRequest{ShareCode: "grp1"})
	if err != nil {
		t.Fatalf("Browse() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 members, got %d: %+v", len(items), items)
	}
	codes := []string{items[0].ShareCode, items[1].ShareCode}
	if codes[0] != "swG1" || codes[1] != "swG2" {
		t.Fatalf("members = %v, want [swG1 swG2]", codes)
	}
}

func TestServiceBrowseUnknownGroupIsEmpty(t *testing.T) {
	svc := &Service{store: stubStore{}}
	items, err := svc.Browse(context.Background(), BrowseRequest{ShareCode: "grp99"})
	if err != nil {
		t.Fatalf("Browse() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected empty, got %+v", items)
	}
}
