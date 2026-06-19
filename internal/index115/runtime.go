package index115

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"

	"github.com/blevesearch/bleve/v2"

	_ "github.com/glebarez/go-sqlite"
)

var ErrManifestNotReady = errors.New("index115 manifest not found or not ready")

func NewRuntime(ctx context.Context, dbPath, bleveBaseDir string) (*Store, *Searcher, error) {
	store, err := OpenStoreRuntime(ctx, dbPath)
	if err != nil {
		return nil, nil, err
	}
	searcher, err := NewSearcher(ctx, store, bleveBaseDir)
	if err != nil {
		_ = store.db.Close()
		return nil, nil, err
	}
	return store, searcher, nil
}

func OpenStoreRuntime(ctx context.Context, dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	store := OpenStore(db)
	if err := store.RefreshShares(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func NewSearcher(ctx context.Context, store *Store, bleveBaseDir string) (*Searcher, error) {
	indexPath, err := loadReadyIndexPath(ctx, store.db, bleveBaseDir)
	if err != nil {
		return nil, err
	}

	index, err := bleve.Open(indexPath)
	if err != nil {
		return nil, err
	}
	return &Searcher{
		store: store,
		index: index,
	}, nil
}

func loadReadyIndexPath(ctx context.Context, db *sql.DB, bleveBaseDir string) (string, error) {
	row := db.QueryRowContext(ctx, `
		SELECT index_path
		FROM index_manifest
		WHERE id = 1 AND status = 'READY'`)
	var relPath string
	if err := row.Scan(&relPath); err != nil {
		if err == sql.ErrNoRows {
			return "", ErrManifestNotReady
		}
		return "", err
	}
	return resolveIndexPath(bleveBaseDir, relPath), nil
}

func resolveIndexPath(bleveBaseDir, manifestPath string) string {
	clean := filepath.Clean(manifestPath)
	if filepath.IsAbs(clean) {
		return filepath.Join(bleveBaseDir, filepath.Base(clean))
	}
	baseName := filepath.Base(filepath.Clean(bleveBaseDir))
	prefix := baseName + string(filepath.Separator)
	if clean == baseName {
		return bleveBaseDir
	}
	if strings.HasPrefix(clean, prefix) {
		clean = strings.TrimPrefix(clean, prefix)
	}
	return filepath.Join(bleveBaseDir, clean)
}
