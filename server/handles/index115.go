package handles

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/OpenListTeam/OpenList/v4/internal/index115"
	"github.com/OpenListTeam/OpenList/v4/server/common"
	"github.com/gin-gonic/gin"
)

type index115HTTPService interface {
	Browse(ctx context.Context, req index115.BrowseRequest) ([]index115.FileItem, error)
	Search(ctx context.Context, req index115.SearchRequest) ([]index115.FileItem, int, error)
	Link(ctx context.Context, req index115.LinkRequest) (index115.ResolvedLink, error)
	Detail(ctx context.Context, fileID string) (index115.FileItem, bool, error)
}

var index115Service index115HTTPService

func SetIndex115Service(service index115HTTPService) {
	index115Service = service
}

var index115Reloader func() error

// SetIndex115Reloader registers the reload callback (wired by bootstrap) that
// reopens the index115 DB + bleve index from disk and swaps the live service.
func SetIndex115Reloader(f func() error) {
	index115Reloader = f
}

// Index115Reload reopens the index115 store/searcher from disk so an externally
// updated DB/index takes effect without restarting PowerList.
func Index115Reload(c *gin.Context) {
	if index115Reloader == nil {
		common.ErrorStrResp(c, "index115 reload not available", 503)
		return
	}
	if err := index115Reloader(); err != nil {
		common.ErrorResp(c, err, 500, true)
		return
	}
	common.SuccessResp(c)
}

func Index115Browse(c *gin.Context) {
	if index115Service == nil {
		common.ErrorStrResp(c, "index115 service not initialized", 503)
		return
	}

	items, err := index115Service.Browse(c.Request.Context(), index115.BrowseRequest{
		ShareCode:   c.Query("share_code"),
		ReceiveCode: c.Query("receive_code"),
		ParentID:    c.Query("parent_id"),
	})
	if err != nil {
		common.ErrorResp(c, err, index115BrowseErrorCode(err))
		return
	}
	common.SuccessResp(c, items)
}

func Index115Search(c *gin.Context) {
	if index115Service == nil {
		common.ErrorStrResp(c, "index115 service not initialized", 503)
		return
	}

	items, total, err := index115Service.Search(c.Request.Context(), index115.SearchRequest{
		Query:     c.Query("q"),
		Page:      parseInt(c.Query("page"), 1),
		PerPage:   parseInt(c.Query("per_page"), 20),
		ShareCode: c.Query("share_code"),
	})
	if err != nil {
		common.ErrorResp(c, err, index115SearchErrorCode(err))
		return
	}
	common.SuccessResp(c, gin.H{
		"query": c.Query("q"),
		"total": total,
		"items": items,
	})
}

func Index115Link(c *gin.Context) {
	if index115Service == nil {
		common.ErrorStrResp(c, "index115 service not initialized", 503)
		return
	}

	var req index115.LinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	link, err := index115Service.Link(c.Request.Context(), req)
	if err != nil {
		common.ErrorResp(c, err, index115LinkErrorCode(err))
		return
	}
	common.SuccessResp(c, gin.H{
		"url":        link.URL,
		"expired_in": link.ExpiredIn,
	})
}

func Index115Detail(c *gin.Context) {
	if index115Service == nil {
		common.ErrorStrResp(c, "index115 service not initialized", 503)
		return
	}

	id := strings.TrimSpace(c.Query("id"))
	if id == "" {
		common.ErrorStrResp(c, "id is required", 400)
		return
	}
	item, ok, err := index115Service.Detail(c.Request.Context(), id)
	if err != nil {
		common.ErrorResp(c, err, index115DetailErrorCode(err))
		return
	}
	if !ok {
		common.ErrorStrResp(c, "file not found", 404)
		return
	}
	common.SuccessResp(c, item)
}

func parseInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func index115BrowseErrorCode(err error) int {
	if errors.Is(err, index115.ErrStoreUnavailable) {
		return 503
	}
	return 503
}

func index115SearchErrorCode(err error) int {
	switch {
	case errors.Is(err, index115.ErrEmptyQuery):
		return 400
	case errors.Is(err, index115.ErrSearchUnavailable):
		return 503
	default:
		return 503
	}
}

func index115LinkErrorCode(err error) int {
	switch {
	case errors.Is(err, index115.ErrMissingLinkArg), errors.Is(err, index115.ErrDirectoryLink):
		return 400
	case errors.Is(err, index115.ErrFileNotFound):
		return 404
	case errors.Is(err, index115.ErrInvalidCookie):
		return 401
	case errors.Is(err, index115.ErrStoreUnavailable), errors.Is(err, index115.ErrLinkClientNotConfigured):
		return 503
	case errors.Is(err, index115.ErrLinkResolveFailed):
		return 502
	default:
		return 502
	}
}

func index115DetailErrorCode(err error) int {
	if errors.Is(err, index115.ErrStoreUnavailable) {
		return 503
	}
	return 503
}
