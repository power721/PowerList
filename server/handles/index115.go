package handles

import (
	"context"
	"errors"
	"strconv"

	"github.com/OpenListTeam/OpenList/v4/internal/index115"
	"github.com/OpenListTeam/OpenList/v4/server/common"
	"github.com/gin-gonic/gin"
)

type index115HTTPService interface {
	Browse(ctx context.Context, req index115.BrowseRequest) ([]index115.FileItem, error)
	Search(ctx context.Context, req index115.SearchRequest) ([]index115.FileItem, int, error)
	Link(ctx context.Context, req index115.LinkRequest) (index115.ResolvedLink, error)
}

var index115Service index115HTTPService

func SetIndex115Service(service index115HTTPService) {
	index115Service = service
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
		common.ErrorResp(c, err, 400)
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
		Scope:     c.Query("scope"),
		ShareCode: c.Query("share_code"),
	})
	if err != nil {
		code := 400
		if errors.Is(err, index115.ErrEmptyQuery) {
			code = 400
		}
		common.ErrorResp(c, err, code)
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
		common.ErrorResp(c, err, 400)
		return
	}
	common.SuccessResp(c, gin.H{
		"url":        link.URL,
		"expired_in": link.ExpiredIn,
	})
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
