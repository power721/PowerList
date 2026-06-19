package handles

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/index115"
	"github.com/OpenListTeam/OpenList/v4/server/common"
	"github.com/gin-gonic/gin"
)

func TestIndex115SearchRejectsEmptyQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	index115Service = stubIndex115HTTPService{}
	router.GET("/index115/search", Index115Search)

	req := httptest.NewRequest(http.MethodGet, "/index115/search?q=", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var resp common.Resp[any]
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if resp.Code != 400 {
		t.Fatalf("expected response code 400, got %+v", resp)
	}
}

func TestIndex115BrowseRootReturnsSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	index115Service = stubIndex115HTTPService{
		browseItems: []index115.FileItem{{ShareCode: "sw1", ShareTitle: "S1", Name: "S1", IsDir: true}},
	}
	router.GET("/index115/browse", Index115Browse)

	req := httptest.NewRequest(http.MethodGet, "/index115/browse", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var resp common.Resp[[]index115.FileItem]
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if resp.Code != 200 {
		t.Fatalf("expected response code 200, got %+v", resp)
	}
	if len(resp.Data) != 1 || resp.Data[0].ShareCode != "sw1" {
		t.Fatalf("unexpected data: %+v", resp.Data)
	}
}

func TestIndex115LinkBindsRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	index115Service = stubIndex115HTTPService{
		link: index115.ResolvedLink{URL: "https://example.com/play", ExpiredIn: 14400},
	}
	router.POST("/index115/link", Index115Link)

	body := `{"cookie":"UID=1;CID=2","share_code":"sw1","file_id":"file1"}`
	req := httptest.NewRequest(http.MethodPost, "/index115/link", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var resp common.Resp[map[string]any]
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if resp.Code != 200 {
		t.Fatalf("expected response code 200, got %+v", resp)
	}
	if resp.Data["url"] != "https://example.com/play" {
		t.Fatalf("unexpected link payload: %+v", resp.Data)
	}
}

type stubIndex115HTTPService struct {
	browseItems []index115.FileItem
	searchItems []index115.FileItem
	searchTotal int
	link        index115.ResolvedLink
	err         error
}

func (s stubIndex115HTTPService) Browse(ctx context.Context, req index115.BrowseRequest) ([]index115.FileItem, error) {
	return s.browseItems, s.err
}

func (s stubIndex115HTTPService) Search(ctx context.Context, req index115.SearchRequest) ([]index115.FileItem, int, error) {
	if strings.TrimSpace(req.Query) == "" {
		return nil, 0, index115.ErrEmptyQuery
	}
	return s.searchItems, s.searchTotal, s.err
}

func (s stubIndex115HTTPService) Link(ctx context.Context, req index115.LinkRequest) (index115.ResolvedLink, error) {
	return s.link, s.err
}
