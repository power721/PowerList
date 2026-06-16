package alist115

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/google/uuid"
)

// Service provides bleve-based indexing and search for 115 cloud storage
type Service struct {
	index   bleve.Index
	dataDir string
}

// NewService creates or opens a bleve index at dataDir/indexes/115
func NewService(dataDir string) (*Service, error) {
	indexPath := filepath.Join(dataDir, "indexes", "115")

	var index bleve.Index
	var err error

	// Try to open existing index
	index, err = bleve.Open(indexPath)
	if err == bleve.ErrorIndexPathDoesNotExist {
		// Create new index with default mapping
		mapping := bleve.NewIndexMapping()
		index, err = bleve.New(indexPath, mapping)
		if err != nil {
			return nil, fmt.Errorf("failed to create index: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("failed to open index: %w", err)
	}

	return &Service{
		index:   index,
		dataDir: dataDir,
	}, nil
}

// BatchIndex indexes multiple nodes in batch, applying path mapping
func (s *Service) BatchIndex(nodes []IndexNode) (int, error) {
	batch := s.index.NewBatch()
	indexed := 0

	for _, node := range nodes {
		// Apply path mapping to remove emoji prefix
		mappedPath := MapPath(node.Path)

		// Create document for indexing
		doc := map[string]interface{}{
			"path":       mappedPath,
			"name":       node.Name,
			"size":       node.Size,
			"is_dir":     node.IsDir,
			"indexed_at": time.Now(),
		}

		// Use UUID as document ID
		docID := uuid.New().String()

		err := batch.Index(docID, doc)
		if err != nil {
			return indexed, fmt.Errorf("failed to add document to batch: %w", err)
		}
		indexed++
	}

	// Execute batch
	if err := s.index.Batch(batch); err != nil {
		return indexed, fmt.Errorf("failed to execute batch: %w", err)
	}

	return indexed, nil
}

// Close closes the bleve index
func (s *Service) Close() error {
	if s.index != nil {
		return s.index.Close()
	}
	return nil
}
