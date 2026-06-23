package index115

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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
	ListGroups(ctx context.Context) ([]GroupInfo, error)
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

const groupSentinelPrefix = "grp"

func (s *Service) Browse(ctx context.Context, req BrowseRequest) ([]FileItem, error) {
	if s == nil {
		return nil, errors.New("browse service is nil")
	}
	if s.store == nil {
		return nil, errors.New("browse store is nil")
	}

	if gid, ok := groupSentinelID(req.ShareCode); ok {
		return s.listGroupMembers(ctx, gid)
	}
	if req.ShareCode == "" {
		return s.listRoot(ctx)
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

// listRoot renders the homepage: one virtual dir per group (share_group order),
// followed by loose (ungrouped) shares. Grouped shares are reachable only via
// their group sentinel.
func (s *Service) listRoot(ctx context.Context) ([]FileItem, error) {
	groups, err := s.store.ListGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	shares, err := s.store.ListShares(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	items := make([]FileItem, 0, len(groups)+len(shares))
	for _, g := range groups {
		items = append(items, FileItem{
			ShareCode: groupSentinel(g.ID),
			Name:      g.Name,
			Path:      "/" + g.Name,
			IsDir:     true,
		})
	}
	items = append(items, looseShareItems(shares)...)
	return items, nil
}

func (s *Service) listGroupMembers(ctx context.Context, gid int64) ([]FileItem, error) {
	shares, err := s.store.ListShares(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	items := make([]FileItem, 0)
	for _, share := range shares {
		if share.GroupID != gid {
			continue
		}
		items = append(items, newShareDirItem(share))
	}
	return items, nil
}

func looseShareItems(shares []ShareSummary) []FileItem {
	var items []FileItem
	for _, share := range shares {
		if share.GroupID != 0 {
			continue
		}
		items = append(items, newShareDirItem(share))
	}
	return items
}

func newShareDirItem(share ShareSummary) FileItem {
	name := share.ShareTitle
	if name == "" {
		name = share.ShareCode
	}
	return FileItem{
		ShareCode:   share.ShareCode,
		ReceiveCode: share.ReceiveCode,
		ShareTitle:  share.ShareTitle,
		Name:        name,
		Path:        "/" + name,
		IsDir:       true,
		UpdatedAt:   share.UpdatedAt,
	}
}

func groupSentinel(id int64) string {
	return groupSentinelPrefix + strconv.FormatInt(id, 10)
}

// groupSentinelID decodes a "grp<N>" sentinel share_code. Real share codes start
// with "sw", so there is no collision.
func groupSentinelID(code string) (int64, bool) {
	if !strings.HasPrefix(code, groupSentinelPrefix) {
		return 0, false
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(code, groupSentinelPrefix), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
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
