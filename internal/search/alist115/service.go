package alist115

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"time"

	"github.com/blevesearch/bleve/v2"
)

const (
	// maxBatchSize limits the number of documents indexed in a single batch
	// to prevent memory exhaustion with very large datasets
	maxBatchSize = 10000
)

// Service provides bleve-based indexing and search for 115 cloud storage
type Service struct {
	index bleve.Index
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
		index: index,
	}, nil
}

// BatchIndex indexes multiple nodes in batch, applying path mapping.
// It processes nodes in chunks to prevent memory exhaustion with large datasets.
// Returns the total number of successfully indexed nodes.
func (s *Service) BatchIndex(nodes []IndexNode) (int, error) {
	// Check if index is still open
	if s.index == nil {
		return 0, fmt.Errorf("index is closed")
	}

	totalIndexed := 0

	// Process in chunks to prevent memory exhaustion
	for i := 0; i < len(nodes); i += maxBatchSize {
		end := i + maxBatchSize
		if end > len(nodes) {
			end = len(nodes)
		}

		chunk := nodes[i:end]
		indexed, err := s.indexChunk(chunk)
		totalIndexed += indexed

		if err != nil {
			return totalIndexed, fmt.Errorf("failed to index chunk at offset %d: %w", i, err)
		}
	}

	return totalIndexed, nil
}

// indexChunk indexes a single chunk of nodes
func (s *Service) indexChunk(nodes []IndexNode) (int, error) {
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

		// Use deterministic ID based on path for idempotent indexing
		docID := generateDocID(mappedPath)

		err := batch.Index(docID, doc)
		if err != nil {
			return indexed, fmt.Errorf("failed to add document to batch: %w", err)
		}
		indexed++
	}

	// Execute batch
	if err := s.index.Batch(batch); err != nil {
		// Don't count as indexed if batch execution fails
		return 0, fmt.Errorf("failed to execute batch: %w", err)
	}

	return indexed, nil
}

// generateDocID creates a deterministic document ID from a path using SHA-256 hash.
// This ensures that re-importing the same file updates the existing document
// instead of creating duplicates.
func generateDocID(path string) string {
	hash := sha256.Sum256([]byte(path))
	return hex.EncodeToString(hash[:])
}

// Close closes the bleve index
func (s *Service) Close() error {
	if s.index != nil {
		return s.index.Close()
	}
	return nil
}
