package alist115

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
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

// Search performs a search query on the indexed nodes.
// It searches the path field using a match query and returns paginated results.
func (s *Service) Search(req SearchRequest) (*SearchResponse, error) {
	// Check if index is still open
	if s.index == nil {
		return nil, fmt.Errorf("index is closed")
	}

	// Apply defaults and validate pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PerPage <= 0 {
		req.PerPage = 20
	}
	if req.PerPage > 100 {
		req.PerPage = 100
	}

	// Calculate offset from page number (Page=1 is first page)
	offset := (req.Page - 1) * req.PerPage

	// Build match query on path field
	matchQuery := bleve.NewMatchQuery(req.Query)
	matchQuery.SetField("path")

	// Apply scope filtering if specified
	var q query.Query = matchQuery
	if req.Scope == 1 {
		// Scope 1: folders only (is_dir=true)
		boolQuery := bleve.NewBooleanQuery()
		boolQuery.AddMust(matchQuery)
		isDirQuery := bleve.NewBoolFieldQuery(true)
		isDirQuery.SetField("is_dir")
		boolQuery.AddMust(isDirQuery)
		q = boolQuery
	} else if req.Scope == 2 {
		// Scope 2: files only (is_dir=false)
		boolQuery := bleve.NewBooleanQuery()
		boolQuery.AddMust(matchQuery)
		isDirQuery := bleve.NewBoolFieldQuery(false)
		isDirQuery.SetField("is_dir")
		boolQuery.AddMust(isDirQuery)
		q = boolQuery
	}
	// Scope 0 or invalid: no filter (all results)

	searchRequest := bleve.NewSearchRequest(q)
	searchRequest.From = offset
	searchRequest.Size = req.PerPage

	// Sort by indexed_at descending (most recent first)
	searchRequest.SortBy([]string{"-indexed_at"})

	// Request specific fields to extract
	searchRequest.Fields = []string{"path", "name", "size", "is_dir"}

	// Execute search
	searchResults, err := s.index.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// Build result nodes
	results := make([]SearchNode, 0, len(searchResults.Hits))
	for _, hit := range searchResults.Hits {
		node := SearchNode{}

		// Extract fields from hit
		if path, ok := hit.Fields["path"].(string); ok {
			node.Path = path
		}
		if name, ok := hit.Fields["name"].(string); ok {
			node.Name = name
		}
		if size, ok := hit.Fields["size"].(float64); ok {
			node.Size = int64(size)
		}
		if isDir, ok := hit.Fields["is_dir"].(bool); ok {
			node.IsDir = isDir
		}

		results = append(results, node)
	}

	return &SearchResponse{
		Query:   req.Query,
		Total:   int(searchResults.Total),
		Results: results,
	}, nil
}

// Clear closes the current index, deletes the index directory, and creates a new empty index.
func (s *Service) Clear(dataDir string) error {
	// Close current index
	if s.index != nil {
		if err := s.index.Close(); err != nil {
			return fmt.Errorf("failed to close index: %w", err)
		}
		s.index = nil
	}

	// Delete index directory
	indexPath := filepath.Join(dataDir, "indexes", "115")
	if err := os.RemoveAll(indexPath); err != nil {
		return fmt.Errorf("failed to delete index directory: %w", err)
	}

	// Create new empty index
	mapping := bleve.NewIndexMapping()
	index, err := bleve.New(indexPath, mapping)
	if err != nil {
		return fmt.Errorf("failed to create new index: %w", err)
	}

	s.index = index
	return nil
}

// Close closes the bleve index
func (s *Service) Close() error {
	if s.index != nil {
		return s.index.Close()
	}
	return nil
}
