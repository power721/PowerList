package alist115

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"
)

func TestServiceBatchIndex(t *testing.T) {
	// Create temporary directory for test index
	tempDir, err := os.MkdirTemp("", "alist115-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	// Create service
	service, err := NewService(tempDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}
	defer service.Close()

	// Verify index path exists
	indexPath := filepath.Join(tempDir, "indexes", "115")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		t.Errorf("Index directory was not created at: %s", indexPath)
	}

	// Create test nodes with emoji paths
	nodes := []IndexNode{
		{
			Path:     "/🏷️我的115分享/测试文件夹/file1.txt",
			Name:     "file1.txt",
			Size:     1024,
			IsDir:    false,
			Modified: time.Now(),
		},
		{
			Path:     "/🏷️我的115分享/测试文件夹",
			Name:     "测试文件夹",
			Size:     0,
			IsDir:    true,
			Modified: time.Now(),
		},
	}

	// Index nodes
	indexed, err := service.BatchIndex(nodes)
	if err != nil {
		t.Fatalf("BatchIndex failed: %v", err)
	}

	// Verify indexed count
	if indexed != 2 {
		t.Errorf("Expected indexed=2, got %d", indexed)
	}

	// Verify documents are in index by checking document count
	docCount, err := service.index.DocCount()
	if err != nil {
		t.Fatalf("Failed to get document count: %v", err)
	}

	if docCount != 2 {
		t.Errorf("Expected 2 documents in index, got %d", docCount)
	}

	// Verify path mapping was applied by searching for the mapped path
	// (without emoji prefix)
	query := bleve.NewMatchQuery("我的115分享")
	search := bleve.NewSearchRequest(query)
	searchResults, err := service.index.Search(search)
	if err != nil {
		t.Fatalf("Failed to search index: %v", err)
	}

	if searchResults.Total != 2 {
		t.Errorf("Expected to find 2 results with mapped path, got %d", searchResults.Total)
	}
}

func TestServiceClose(t *testing.T) {
	// Create temporary directory for test index
	tempDir, err := os.MkdirTemp("", "alist115-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	// Create service
	service, err := NewService(tempDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Close service
	err = service.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Verify we can reopen the index
	service2, err := NewService(tempDir)
	if err != nil {
		t.Fatalf("Failed to reopen service: %v", err)
	}
	defer service2.Close()
}

// TestBatchIndexAfterClose verifies that BatchIndex returns an error when called after Close
func TestBatchIndexAfterClose(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "alist115-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	service, err := NewService(tempDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Close the service
	if err := service.Close(); err != nil {
		t.Fatalf("Failed to close service: %v", err)
	}

	// Try to index after close - should fail
	nodes := []IndexNode{
		{Path: "/test/file.txt", Name: "file.txt", Size: 100, IsDir: false},
	}

	indexed, err := service.BatchIndex(nodes)
	if err == nil {
		t.Error("Expected error when indexing after close, got nil")
	}
	if indexed != 0 {
		t.Errorf("Expected indexed=0 after close, got %d", indexed)
	}
}

// TestIdempotentIndexing verifies that re-indexing the same path updates instead of duplicating
func TestIdempotentIndexing(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "alist115-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	service, err := NewService(tempDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}
	defer service.Close()

	// Index a node
	nodes := []IndexNode{
		{Path: "/test/file.txt", Name: "file.txt", Size: 100, IsDir: false, Modified: time.Now()},
	}

	indexed, err := service.BatchIndex(nodes)
	if err != nil {
		t.Fatalf("First BatchIndex failed: %v", err)
	}
	if indexed != 1 {
		t.Errorf("Expected indexed=1, got %d", indexed)
	}

	// Verify document count
	docCount, err := service.index.DocCount()
	if err != nil {
		t.Fatalf("Failed to get document count: %v", err)
	}
	if docCount != 1 {
		t.Errorf("Expected 1 document after first index, got %d", docCount)
	}

	// Re-index the same path with different size (simulating an update)
	nodes[0].Size = 200
	indexed, err = service.BatchIndex(nodes)
	if err != nil {
		t.Fatalf("Second BatchIndex failed: %v", err)
	}
	if indexed != 1 {
		t.Errorf("Expected indexed=1, got %d", indexed)
	}

	// Verify document count is still 1 (updated, not duplicated)
	docCount, err = service.index.DocCount()
	if err != nil {
		t.Fatalf("Failed to get document count: %v", err)
	}
	if docCount != 1 {
		t.Errorf("Expected 1 document after re-index (no duplicate), got %d", docCount)
	}
}

// TestLargeBatchChunking verifies that large batches are processed in chunks
func TestLargeBatchChunking(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "alist115-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	service, err := NewService(tempDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}
	defer service.Close()

	// Create 25,000 nodes (should be processed in 3 chunks of 10,000)
	const nodeCount = 25000
	nodes := make([]IndexNode, nodeCount)
	for i := 0; i < nodeCount; i++ {
		nodes[i] = IndexNode{
			Path:     fmt.Sprintf("/test/file%d.txt", i),
			Name:     fmt.Sprintf("file%d.txt", i),
			Size:     int64(i * 100),
			IsDir:    false,
			Modified: time.Now(),
		}
	}

	// Index all nodes
	indexed, err := service.BatchIndex(nodes)
	if err != nil {
		t.Fatalf("BatchIndex failed: %v", err)
	}

	if indexed != nodeCount {
		t.Errorf("Expected indexed=%d, got %d", nodeCount, indexed)
	}

	// Verify document count
	docCount, err := service.index.DocCount()
	if err != nil {
		t.Fatalf("Failed to get document count: %v", err)
	}

	if docCount != nodeCount {
		t.Errorf("Expected %d documents in index, got %d", nodeCount, docCount)
	}
}

