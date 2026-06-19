package server

import (
	"context"
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
