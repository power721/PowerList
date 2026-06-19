package index115

import (
	"context"
	"errors"
	"strings"
)

var (
	ErrEmptyQuery     = errors.New("query cannot be empty")
	ErrMissingLinkArg = errors.New("cookie, share_code and file_id are required")
	ErrFileNotFound   = errors.New("file not found")
	ErrDirectoryLink  = errors.New("cannot link directory")
)

type StoreReader interface {
	ListShares(ctx context.Context) ([]ShareSummary, error)
	ListChildren(ctx context.Context, shareCode, parentID string) ([]FileItem, error)
	FileByID(ctx context.Context, fileID string) (FileItem, bool, error)
}

type SearchReader interface {
	Search(ctx context.Context, req SearchRequest) ([]FileItem, int, error)
}

type Linker interface {
	Resolve(ctx context.Context, req LinkRequest, file FileItem) (ResolvedLink, error)
}

type Service struct {
	store  StoreReader
	search SearchReader
	linker Linker
}

func NewService(store StoreReader, search SearchReader, linker Linker) *Service {
	return &Service{
		store:  store,
		search: search,
		linker: linker,
	}
}

func (s *Service) Browse(ctx context.Context, req BrowseRequest) ([]FileItem, error) {
	if req.ShareCode == "" {
		shares, err := s.store.ListShares(ctx)
		if err != nil {
			return nil, err
		}
		items := make([]FileItem, 0, len(shares))
		for _, share := range shares {
			name := share.ShareTitle
			if name == "" {
				name = share.ShareCode
			}
			items = append(items, FileItem{
				ShareCode:   share.ShareCode,
				ReceiveCode: share.ReceiveCode,
				ShareTitle:  share.ShareTitle,
				Name:        name,
				Path:        "/" + name,
				IsDir:       true,
				UpdatedAt:   share.UpdatedAt,
			})
		}
		return items, nil
	}

	parentID := req.ParentID
	if parentID == "" {
		parentID = "0"
	}
	return s.store.ListChildren(ctx, req.ShareCode, parentID)
}

func (s *Service) Search(ctx context.Context, req SearchRequest) ([]FileItem, int, error) {
	if strings.TrimSpace(req.Query) == "" {
		return nil, 0, ErrEmptyQuery
	}
	return s.search.Search(ctx, req)
}

func (s *Service) Link(ctx context.Context, req LinkRequest) (ResolvedLink, error) {
	if req.Cookie == "" || req.ShareCode == "" || req.FileID == "" {
		return ResolvedLink{}, ErrMissingLinkArg
	}
	file, ok, err := s.store.FileByID(ctx, req.FileID)
	if err != nil {
		return ResolvedLink{}, err
	}
	if !ok || file.ShareCode != req.ShareCode {
		return ResolvedLink{}, ErrFileNotFound
	}
	if file.IsDir {
		return ResolvedLink{}, ErrDirectoryLink
	}
	return s.linker.Resolve(ctx, req, file)
}
