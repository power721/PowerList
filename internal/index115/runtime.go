package index115

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"

	"github.com/blevesearch/bleve/v2"

	_ "github.com/glebarez/go-sqlite"
)

var ErrManifestNotReady = errors.New("index115 manifest not found or not ready")

func NewRuntime(ctx context.Context, dbPath, bleveBaseDir string) (*Store, *Searcher, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, nil, err
	}

	store := OpenStore(db)
	if err := store.RefreshShares(ctx); err != nil {
		_ = db.Close()
		return nil, nil, err
	}

	indexPath, err := loadReadyIndexPath(ctx, db, bleveBaseDir)
	if err != nil {
		_ = db.Close()
		return nil, nil, err
	}

	index, err := bleve.Open(indexPath)
	if err != nil {
		_ = db.Close()
		return nil, nil, err
	}

	return store, &Searcher{
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
	return filepath.Join(bleveBaseDir, relPath), nil
}
