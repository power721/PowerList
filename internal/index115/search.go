package index115

import (
	"context"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

type Searcher struct {
	store *Store
	index bleve.Index
}

func (s *Searcher) Search(ctx context.Context, req SearchRequest) ([]FileItem, int, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PerPage <= 0 {
		req.PerPage = 20
	}
	if req.PerPage > 100 {
		req.PerPage = 100
	}

	q := buildSearchQuery(req)
	offset := 0
	pageStart := (req.Page - 1) * req.PerPage
	resolvedTotal := 0
	items := make([]FileItem, 0, req.PerPage)

	for {
		searchReq := bleve.NewSearchRequestOptions(q, req.PerPage, offset, false)
		res, err := s.index.SearchInContext(ctx, searchReq)
		if err != nil {
			return nil, 0, err
		}
		if len(res.Hits) == 0 {
			break
		}

		ids := make([]string, 0, len(res.Hits))
		for _, hit := range res.Hits {
			ids = append(ids, hit.ID)
		}
		files, err := s.store.FilesByIDs(ctx, ids)
		if err != nil {
			return nil, 0, err
		}

		resolvedBatch := 0
		for _, id := range ids {
			item, ok := files[id]
			if !ok {
				continue
			}
			if resolvedTotal >= pageStart && len(items) < req.PerPage {
				items = append(items, item)
			}
			resolvedTotal++
			resolvedBatch++
		}

		offset += len(res.Hits)
		if len(res.Hits) < req.PerPage {
			break
		}
		if resolvedBatch == 0 && offset >= int(res.Total) {
			break
		}
	}

	return items, resolvedTotal, nil
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
