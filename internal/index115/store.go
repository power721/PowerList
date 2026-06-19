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
	Status        string
	LastCrawledAt int64
	ID            int64
}

type Store struct {
	db     *sql.DB
	shares map[string]shareMeta
}

func OpenStore(db *sql.DB) *Store {
	return &Store{
		db:     db,
		shares: map[string]shareMeta{},
	}
}

func (s *Store) RefreshShares(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, share_code, COALESCE(receive_code, ''), COALESCE(share_title, ''), status, COALESCE(last_crawled_at, 0)
		FROM share`)
	if err != nil {
		return err
	}
	defer rows.Close()

	shares := map[string]shareMeta{}
	for rows.Next() {
		var meta shareMeta
		if err := rows.Scan(&meta.ID, &meta.ShareCode, &meta.ReceiveCode, &meta.ShareTitle, &meta.Status, &meta.LastCrawledAt); err != nil {
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
	s.shares = shares
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

func (s *Store) ListChildren(ctx context.Context, shareCode, parentID string) ([]FileItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT file_id, share_code, parent_id, name, path, size, is_dir, ext, sha1, COALESCE(updated_at, 0)
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

func (s *Store) FileByID(ctx context.Context, fileID string) (FileItem, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT file_id, share_code, parent_id, name, path, size, is_dir, ext, sha1, COALESCE(updated_at, 0)
		FROM file
		WHERE file_id = ?`, fileID)

	var item FileItem
	var isDir int
	err := row.Scan(&item.FileID, &item.ShareCode, &item.ParentID, &item.Name, &item.Path, &item.Size, &isDir, &item.Ext, &item.SHA1, &item.UpdatedAt)
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
		SELECT file_id, share_code, parent_id, name, path, size, is_dir, ext, sha1, COALESCE(updated_at, 0)
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

func scanFileItem(scanner interface {
	Scan(dest ...any) error
}) (FileItem, error) {
	var item FileItem
	var isDir int
	err := scanner.Scan(&item.FileID, &item.ShareCode, &item.ParentID, &item.Name, &item.Path, &item.Size, &isDir, &item.Ext, &item.SHA1, &item.UpdatedAt)
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
