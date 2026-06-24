package index115

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

type shareMeta struct {
	ShareCode     string
	ReceiveCode   string
	ShareTitle    string
	RootFolderID  string
	GroupID       int64
	Status        string
	LastCrawledAt int64
	ID            int64
}

type Store struct {
	db     *sql.DB
	shares map[string]shareMeta
	groups []GroupInfo
}

func OpenStore(db *sql.DB) *Store {
	return &Store{
		db:     db,
		shares: map[string]shareMeta{},
	}
}

func (s *Store) RefreshShares(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, share_code, COALESCE(receive_code, ''), COALESCE(share_title, ''), status, COALESCE(last_crawled_at, 0), COALESCE(group_id, 0)
		FROM share`)
	if err != nil {
		return err
	}
	defer rows.Close()

	shares := map[string]shareMeta{}
	for rows.Next() {
		var meta shareMeta
		if err := rows.Scan(&meta.ID, &meta.ShareCode, &meta.ReceiveCode, &meta.ShareTitle, &meta.Status, &meta.LastCrawledAt, &meta.GroupID); err != nil {
			return err
		}
		current, ok := shares[meta.ShareCode]
		if !ok || preferShareMeta(meta, current) {
			shares[meta.ShareCode] = meta
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Derive each share's effective root folder: when a share has exactly one
	// root-level row (parent_id='0') and it is a directory, that folder is a
	// redundant wrapper whose name duplicates the share title — collapse it by
	// recording its file_id here so ListChildren/resolveFullPath can skip it.
	// Index-served via idx_file_share_parent; only root-level rows are read,
	// never the full tree.
	rootRows, err := s.db.QueryContext(ctx, `
		SELECT share_code,
		       COUNT(*) AS n,
		       SUM(is_dir) AS dirs,
		       MAX(CASE WHEN is_dir = 1 THEN file_id END) AS dir_id
		FROM file
		WHERE parent_id = '0'
		GROUP BY share_code`)
	if err != nil {
		return err
	}
	defer rootRows.Close()
	for rootRows.Next() {
		var (
			shareCode string
			n, dirs   int
			dirID     sql.NullString
		)
		if err := rootRows.Scan(&shareCode, &n, &dirs, &dirID); err != nil {
			return err
		}
		meta, ok := shares[shareCode]
		if !ok {
			continue
		}
		if n == 1 && dirs == 1 && dirID.Valid {
			meta.RootFolderID = dirID.String
			shares[shareCode] = meta
		}
	}
	if err := rootRows.Err(); err != nil {
		return err
	}

	groupRows, err := s.db.QueryContext(ctx, `SELECT group_id, name FROM share_group ORDER BY sort_order ASC`)
	if err != nil {
		return err
	}
	defer groupRows.Close()
	var gs []GroupInfo
	for groupRows.Next() {
		var g GroupInfo
		if err := groupRows.Scan(&g.ID, &g.Name); err != nil {
			return err
		}
		gs = append(gs, g)
	}
	if err := groupRows.Err(); err != nil {
		return err
	}

	s.shares = shares
	s.groups = gs
	return nil
}

func preferShareMeta(next, current shareMeta) bool {
	if next.Status == "ACTIVE" && current.Status != "ACTIVE" {
		return true
	}
	if next.Status != "ACTIVE" && current.Status == "ACTIVE" {
		return false
	}
	if next.LastCrawledAt != current.LastCrawledAt {
		return next.LastCrawledAt > current.LastCrawledAt
	}
	return next.ID > current.ID
}

func (s *Store) ListShares(ctx context.Context) ([]ShareSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT share_code,
		       MAX(COALESCE(updated_at, 0)) AS updated_at,
		       SUM(CASE WHEN is_dir = 0 THEN 1 ELSE 0 END) AS file_count,
		       SUM(CASE WHEN is_dir = 1 THEN 1 ELSE 0 END) AS dir_count
		FROM file
		GROUP BY share_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ShareSummary
	for rows.Next() {
		var item ShareSummary
		if err := rows.Scan(&item.ShareCode, &item.UpdatedAt, &item.FileCount, &item.DirCount); err != nil {
			return nil, err
		}
		meta := s.shares[item.ShareCode]
		item.GroupID = meta.GroupID
		item.ReceiveCode = meta.ReceiveCode
		item.ShareTitle = meta.ShareTitle
		if item.ShareTitle == "" {
			item.ShareTitle = item.ShareCode
		}
		item.Path = "/"
		item.IsDir = true
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ShareCode < items[j].ShareCode
	})
	return items, nil
}

func (s *Store) ListGroups(ctx context.Context) ([]GroupInfo, error) {
	return s.groups, nil
}

func (s *Store) ListChildren(ctx context.Context, shareCode, parentID string) ([]FileItem, error) {
	// Single-root shares collapse: the lone root folder duplicates the share
	// title, so browsing the share root (parent_id="0") jumps straight into
	// that folder's children.
	if parentID == "0" {
		if root := s.shares[shareCode].RootFolderID; root != "" {
			parentID = root
		}
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT file_id, share_code, parent_id, name, size, is_dir, ext, sha1, COALESCE(updated_at, 0)
		FROM file
		WHERE share_code = ? AND parent_id = ?
		ORDER BY is_dir DESC, name ASC`, shareCode, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	meta := s.shares[shareCode]
	var items []FileItem
	for rows.Next() {
		item, err := scanFileItem(rows)
		if err != nil {
			return nil, err
		}
		applyShareMeta(&item, meta)
		items = append(items, item)
	}
	return items, rows.Err()
}

// parseCompositeFileID splits a consumer-facing file id of the form
// "shareCode-fileId" back into its share code and raw 115 cid. The cid is NOT
// globally unique — the same folder linked by several shares reuses one cid — so
// every lookup must be scoped by share_code. Share codes ("sw" + alphanumerics)
// never contain '-', so the first '-' is the separator. Returns ok=false for ids
// that are not in this form.
func parseCompositeFileID(id string) (shareCode, fileID string, ok bool) {
	idx := strings.IndexByte(id, '-')
	if idx <= 0 || idx == len(id)-1 {
		return "", "", false
	}
	return id[:idx], id[idx+1:], true
}

// fileByShareAndID loads a single file row scoped by (share_code, file_id). This
// is the correct primitive now that file_id is only unique within a share.
func (s *Store) fileByShareAndID(ctx context.Context, shareCode, fileID string) (FileItem, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT file_id, share_code, parent_id, name, size, is_dir, ext, sha1, COALESCE(updated_at, 0)
		FROM file
		WHERE share_code = ? AND file_id = ?`, shareCode, fileID)

	var item FileItem
	var isDir int
	err := row.Scan(&item.FileID, &item.ShareCode, &item.ParentID, &item.Name, &item.Size, &isDir, &item.Ext, &item.SHA1, &item.UpdatedAt)
	if err == sql.ErrNoRows {
		return FileItem{}, false, nil
	}
	if err != nil {
		return FileItem{}, false, err
	}
	item.IsDir = isDir == 1
	applyShareMeta(&item, s.shares[item.ShareCode])
	return item, true, nil
}

// FileByID resolves a consumer-facing composite file id ("shareCode-fileId",
// assembled by clients such as alist-tvbox) to its row, scoped by share_code so
// a cid shared across several shares resolves within the right share.
func (s *Store) FileByID(ctx context.Context, fileID string) (FileItem, bool, error) {
	shareCode, fid, ok := parseCompositeFileID(fileID)
	if !ok {
		return FileItem{}, false, nil
	}
	return s.fileByShareAndID(ctx, shareCode, fid)
}

// FileWithFullPath returns the file row with Path rebuilt as the full path
// relative to the share root by walking the parent_id chain. The file table's
// path column only stores the immediate "/name" segment, so callers that need
// the real location (e.g. assembling a mounted-storage play path) must walk
// parent_id up to the share root (parent_id = "0").
func (s *Store) FileWithFullPath(ctx context.Context, fileID string) (FileItem, bool, error) {
	item, ok, err := s.FileByID(ctx, fileID)
	if err != nil || !ok {
		return FileItem{}, false, err
	}
	item.Path = s.resolveFullPath(ctx, item)
	return item, true, nil
}

func (s *Store) resolveFullPath(ctx context.Context, item FileItem) string {
	rootFolderID := s.shares[item.ShareCode].RootFolderID
	segments := []string{item.Name}
	parentID := item.ParentID
	for i := 0; i < 64 && parentID != "" && parentID != "0" && parentID != rootFolderID; i++ {
		parent, ok, err := s.fileByShareAndID(ctx, item.ShareCode, parentID)
		if err != nil || !ok {
			break
		}
		segments = append([]string{parent.Name}, segments...)
		parentID = parent.ParentID
	}
	return "/" + strings.Join(segments, "/")
}

func (s *Store) FilesByIDs(ctx context.Context, ids []string) (map[string]FileItem, error) {
	if len(ids) == 0 {
		return map[string]FileItem{}, nil
	}

	placeholders := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		SELECT file_id, share_code, parent_id, name, size, is_dir, ext, sha1, COALESCE(updated_at, 0)
		FROM file
		WHERE file_id IN (%s)`, strings.Join(placeholders, ","))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make(map[string]FileItem, len(ids))
	for rows.Next() {
		item, err := scanFileItem(rows)
		if err != nil {
			return nil, err
		}
		applyShareMeta(&item, s.shares[item.ShareCode])
		items[item.FileID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// FilesBySearchIDs resolves bleve search-hit ids into file rows. The index can
// emit two id formats: a bare 115 cid (legacy doc ids) or a composite
// "shareCode-fileId" (current doc ids). Composite ids resolve via the
// (share_code, file_id) primary key, so a cid shared across several shares
// resolves within its own share rather than a sibling's. Bare ids fall back to
// file_id-only — ambiguous for a shared cid (legacy, lossy), retained only for
// back-compat with indexes built before the composite doc id. Results are keyed
// by the original input id so callers can reassemble hits in bleve relevance
// order regardless of format.
func (s *Store) FilesBySearchIDs(ctx context.Context, ids []string) (map[string]FileItem, error) {
	out := make(map[string]FileItem, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	type kv struct{ shareCode, fileID string }
	var composites []kv
	var bares []string
	for _, id := range ids {
		if shareCode, fileID, ok := parseCompositeFileID(id); ok {
			composites = append(composites, kv{shareCode, fileID})
		} else {
			bares = append(bares, id)
		}
	}

	const fileCols = `file_id, share_code, parent_id, name, size, is_dir, ext, sha1, COALESCE(updated_at, 0)`

	// Composites: resolve through the (share_code, file_id) key via a row-value
	// IN, disambiguating cids shared across shares. Keyed by "shareCode-fileId".
	if len(composites) > 0 {
		var q strings.Builder
		q.WriteString(`SELECT ` + fileCols + ` FROM file WHERE (share_code, file_id) IN (`)
		args := make([]any, 0, len(composites)*2)
		for i, p := range composites {
			if i > 0 {
				q.WriteByte(',')
			}
			q.WriteString("(?,?)")
			args = append(args, p.shareCode, p.fileID)
		}
		q.WriteByte(')')
		if err := s.scanKeyedRows(ctx, q.String(), args, func(item FileItem) string {
			return item.ShareCode + "-" + item.FileID
		}, out); err != nil {
			return nil, err
		}
	}

	// Bares: legacy file_id-only lookup, keyed by the bare cid.
	if len(bares) > 0 {
		placeholders := make([]string, len(bares))
		args := make([]any, len(bares))
		for i, id := range bares {
			placeholders[i] = "?"
			args[i] = id
		}
		query := fmt.Sprintf(`SELECT %s FROM file WHERE file_id IN (%s)`, fileCols, strings.Join(placeholders, ","))
		if err := s.scanKeyedRows(ctx, query, args, func(item FileItem) string {
			return item.FileID
		}, out); err != nil {
			return nil, err
		}
	}

	return out, nil
}

// scanKeyedRows runs a file SELECT and inserts each row into out under the key
// returned by key, applying share metadata. Shared so the composite and bare
// lookup branches in FilesBySearchIDs don't duplicate the scan/close loop.
func (s *Store) scanKeyedRows(ctx context.Context, query string, args []any, key func(FileItem) string, out map[string]FileItem) error {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanFileItem(rows)
		if err != nil {
			return err
		}
		applyShareMeta(&item, s.shares[item.ShareCode])
		out[key(item)] = item
	}
	return rows.Err()
}

func scanFileItem(scanner interface {
	Scan(dest ...any) error
}) (FileItem, error) {
	var item FileItem
	var isDir int
	err := scanner.Scan(&item.FileID, &item.ShareCode, &item.ParentID, &item.Name, &item.Size, &isDir, &item.Ext, &item.SHA1, &item.UpdatedAt)
	if err != nil {
		return FileItem{}, err
	}
	item.IsDir = isDir == 1
	return item, nil
}

func applyShareMeta(item *FileItem, meta shareMeta) {
	item.ReceiveCode = meta.ReceiveCode
	item.ShareTitle = meta.ShareTitle
	if item.ShareTitle == "" {
		item.ShareTitle = item.ShareCode
	}
}
