package index115

import (
	"context"
	"errors"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

type Searcher struct {
	store *Store
	index bleve.Index
}

func (s *Searcher) Search(ctx context.Context, req SearchRequest) ([]FileItem, int, error) {
	if s == nil {
		return nil, 0, errors.New("searcher is nil")
	}
	if s.index == nil {
		return nil, 0, errors.New("index is nil")
	}
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PerPage <= 0 {
		req.PerPage = 20
	}
	if req.PerPage > 100 {
		req.PerPage = 100
	}

	// Fetch only the requested page in a single bleve query. bleve returns the
	// full match count as res.Total, so there is no need to page through every
	// match to compute a total.
	q := buildSearchQuery(req)
	from := (req.Page - 1) * req.PerPage
	searchReq := bleve.NewSearchRequestOptions(q, req.PerPage, from, false)
	res, err := s.index.SearchInContext(ctx, searchReq)
	if err != nil {
		return nil, 0, err
	}
	if len(res.Hits) == 0 {
		return []FileItem{}, int(res.Total), nil
	}

	ids := make([]string, 0, len(res.Hits))
	for _, hit := range res.Hits {
		ids = append(ids, hit.ID)
	}
	// ids are bleve doc ids: a bare cid (legacy index) or "shareCode-fileId"
	// (current index). FilesBySearchIDs resolves both, scoping composite ids by
	// share so a shared cid lands in the right share.
	files, err := s.store.FilesBySearchIDs(ctx, ids)
	if err != nil {
		return nil, 0, err
	}

	// Preserve bleve's relevance order; drop any hits that no longer have a
	// store row (index/store drift) instead of backfilling from later pages.
	items := make([]FileItem, 0, len(ids))
	for _, id := range ids {
		if item, ok := files[id]; ok {
			items = append(items, item)
		}
	}
	return items, int(res.Total), nil
}

func buildSearchQuery(req SearchRequest) query.Query {
	match := bleve.NewMatchQuery(req.Query)
	if strings.TrimSpace(req.ShareCode) == "" {
		return match
	}
	boolQuery := bleve.NewBooleanQuery()
	boolQuery.AddMust(match)
	shareQuery := bleve.NewTermQuery(req.ShareCode)
	shareQuery.SetField("share_code")
	boolQuery.AddMust(shareQuery)
	return boolQuery
}
