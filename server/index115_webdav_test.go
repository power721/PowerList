package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/index115"
	"github.com/gin-gonic/gin"
)

func TestIndex115WebDAVPropfindRootListsShares(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	index115BrowseService = stubIndex115WebDAVService{
		rootItems: []index115.FileItem{
			{ShareCode: "sw1", ShareTitle: "Share One", Name: "Share One", IsDir: true},
		},
	}
	WebDav(router.Group("/dav"))

	req := httptest.NewRequest("PROPFIND", "/dav/index115", nil)
	req.Header.Set("Depth", "1")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "/dav/index115/Share%20One") {
		t.Fatalf("expected share href in response, got %s", w.Body.String())
	}
}

func TestIndex115WebDAVPropfindChildListsChildren(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	index115BrowseService = stubIndex115WebDAVService{
		rootItems: []index115.FileItem{
			{ShareCode: "sw1", ShareTitle: "Share One", Name: "Share One", IsDir: true},
		},
		childItems: map[string][]index115.FileItem{
			"sw1:0": {
				{FileID: "dir1", ShareCode: "sw1", Name: "Folder", IsDir: true, ParentID: "0"},
				{FileID: "file1", ShareCode: "sw1", Name: "movie.mkv", IsDir: false, ParentID: "0", Size: 123},
			},
		},
	}
	WebDav(router.Group("/dav"))

	req := httptest.NewRequest("PROPFIND", "/dav/index115/Share%20One", nil)
	req.Header.Set("Depth", "1")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "/dav/index115/Share%20One/Folder") {
		t.Fatalf("expected folder href in response, got %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "/dav/index115/Share%20One/movie.mkv") {
		t.Fatalf("expected file href in response, got %s", w.Body.String())
	}
}

func TestIndex115WebDAVDisablesMutations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	index115BrowseService = stubIndex115WebDAVService{}
	WebDav(router.Group("/dav"))

	req := httptest.NewRequest(http.MethodPut, "/dav/index115/Share%20One/new.txt", strings.NewReader("x"))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
}

type stubIndex115WebDAVService struct {
	rootItems  []index115.FileItem
	childItems map[string][]index115.FileItem
}

func (s stubIndex115WebDAVService) Browse(_ context.Context, req index115.BrowseRequest) ([]index115.FileItem, error) {
	if req.ShareCode == "" {
		return s.rootItems, nil
	}
	return s.childItems[req.ShareCode+":"+req.ParentID], nil
}

// stubGroupBrowseProvider returns canned children for the root, a group
// sentinel, and a real member share, exercising the group -> member crossing.
type stubGroupBrowseProvider struct{}

func (stubGroupBrowseProvider) Browse(_ context.Context, req index115.BrowseRequest) ([]index115.FileItem, error) {
	switch {
	case req.ShareCode == "":
		return []index115.FileItem{{ShareCode: "grp1", ShareTitle: "欧美剧", Name: "欧美剧", IsDir: true}}, nil
	case req.ShareCode == "grp1":
		return []index115.FileItem{{ShareCode: "swM", ShareTitle: "Member", Name: "Member", IsDir: true}}, nil
	case req.ShareCode == "swM":
		return []index115.FileItem{{FileID: "f1", ShareCode: "swM", Name: "movie.mkv", IsDir: false}}, nil
	}
	return nil, errors.New("unexpected browse")
}

func TestWebDAVResolveDrillsGroupIntoMember(t *testing.T) {
	prev := index115BrowseService
	index115BrowseService = stubGroupBrowseProvider{}
	t.Cleanup(func() { index115BrowseService = prev })

	fs := &index115WebDAVFS{}
	entry, err := fs.resolve(context.Background(), "/欧美剧/Member")
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	// Without the fix, childInfos re-browses the grp1 sentinel and returns the
	// Member node again instead of the actual file under swM.
	if len(entry.children) != 1 {
		t.Fatalf("children = %d, want 1 (movie.mkv): %+v", len(entry.children), entry.children)
	}
	if entry.children[0].Name() != "movie.mkv" {
		t.Fatalf("child = %q, want movie.mkv", entry.children[0].Name())
	}
}

func TestWebDAVResolveGroupListsMembers(t *testing.T) {
	prev := index115BrowseService
	index115BrowseService = stubGroupBrowseProvider{}
	t.Cleanup(func() { index115BrowseService = prev })

	fs := &index115WebDAVFS{}
	entry, err := fs.resolve(context.Background(), "/欧美剧")
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if len(entry.children) != 1 || entry.children[0].Name() != "Member" {
		t.Fatalf("children = %+v, want [Member]", entry.children)
	}
}
