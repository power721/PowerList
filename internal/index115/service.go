package index115

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrEmptyQuery        = errors.New("query cannot be empty")
	ErrMissingLinkArg    = errors.New("cookie, share_code and file_id are required")
	ErrFileNotFound      = errors.New("file not found")
	ErrDirectoryLink     = errors.New("cannot link directory")
	ErrSearchUnavailable = errors.New("search unavailable")
	ErrStoreUnavailable  = errors.New("index115 store unavailable")
	ErrInvalidCookie     = errors.New("invalid or expired 115 cookie")
	ErrLinkResolveFailed = errors.New("failed to resolve 115 link")
)

var MyIndex115Service *Service

type StoreReader interface {
	ListShares(ctx context.Context) ([]ShareSummary, error)
	ListChildren(ctx context.Context, shareCode, parentID string) ([]FileItem, error)
	FileByID(ctx context.Context, fileID string) (FileItem, bool, error)
	FileWithFullPath(ctx context.Context, fileID string) (FileItem, bool, error)
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
	MyIndex115Service = &Service{
		store:  store,
		search: search,
		linker: linker,
	}
	return MyIndex115Service
}

func (s *Service) Browse(ctx context.Context, req BrowseRequest) ([]FileItem, error) {
	if s == nil {
		return nil, errors.New("browse service is nil")
	}
	if s.store == nil {
		return nil, errors.New("browse store is nil")
	}
	if req.ShareCode == "" {
		shares, err := s.store.ListShares(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
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
	if parentID == "" || parentID == "/" {
		parentID = "0"
	}
	items, err := s.store.ListChildren(ctx, req.ShareCode, parentID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return items, nil
}

func (s *Service) Search(ctx context.Context, req SearchRequest) ([]FileItem, int, error) {
	if strings.TrimSpace(req.Query) == "" {
		return nil, 0, ErrEmptyQuery
	}
	if s.search == nil {
		return nil, 0, ErrSearchUnavailable
	}
	items, total, err := s.search.Search(ctx, req)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", ErrSearchUnavailable, err)
	}
	return items, total, nil
}

func (s *Service) Link(ctx context.Context, req LinkRequest) (ResolvedLink, error) {
	if req.Cookie == "" || req.ShareCode == "" || req.FileID == "" {
		return ResolvedLink{}, ErrMissingLinkArg
	}
	file, ok, err := s.store.FileByID(ctx, req.FileID)
	if err != nil {
		return ResolvedLink{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if !ok || file.ShareCode != req.ShareCode {
		return ResolvedLink{}, ErrFileNotFound
	}
	if file.IsDir {
		return ResolvedLink{}, ErrDirectoryLink
	}
	return s.linker.Resolve(ctx, req, file)
}

// Detail returns a single file by id with its Path rebuilt as the full
// within-share path (parent_id chain walked). Used by clients that play
// through a mounted storage using the assembled path.
func (s *Service) Detail(ctx context.Context, fileID string) (FileItem, bool, error) {
	if s == nil {
		return FileItem{}, false, errors.New("detail service is nil")
	}
	if s.store == nil {
		return FileItem{}, false, errors.New("detail store is nil")
	}
	return s.store.FileWithFullPath(ctx, fileID)
}
