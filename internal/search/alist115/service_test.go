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

// TestServiceSearch verifies that search returns matching files
func TestServiceSearch(t *testing.T) {
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

	// Index 3 files: 2 with "侏罗纪", 1 with "other"
	nodes := []IndexNode{
		{
			Path:     "/movies/侏罗纪公园.mp4",
			Name:     "侏罗纪公园.mp4",
			Size:     1024000,
			IsDir:    false,
			Modified: time.Now(),
		},
		{
			Path:     "/movies/侏罗纪世界.mp4",
			Name:     "侏罗纪世界.mp4",
			Size:     2048000,
			IsDir:    false,
			Modified: time.Now(),
		},
		{
			Path:     "/movies/other_movie.mp4",
			Name:     "other_movie.mp4",
			Size:     512000,
			IsDir:    false,
			Modified: time.Now(),
		},
	}

	indexed, err := service.BatchIndex(nodes)
	if err != nil {
		t.Fatalf("BatchIndex failed: %v", err)
	}
	if indexed != 3 {
		t.Fatalf("Expected 3 indexed nodes, got %d", indexed)
	}

	// Search for "侏罗纪"
	req := SearchRequest{
		Query:   "侏罗纪",
		Page:    1,
		PerPage: 20,
	}

	resp, err := service.Search(req)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// Verify Total >= 2
	if resp.Total < 2 {
		t.Errorf("Expected Total >= 2, got %d", resp.Total)
	}

	// Verify query is echoed back
	if resp.Query != "侏罗纪" {
		t.Errorf("Expected Query='侏罗纪', got '%s'", resp.Query)
	}

	// Verify results contain data
	if len(resp.Results) < 2 {
		t.Errorf("Expected at least 2 results, got %d", len(resp.Results))
	}

	// Verify result nodes have expected fields
	for _, node := range resp.Results {
		if node.Path == "" {
			t.Error("Result node has empty Path")
		}
		if node.Name == "" {
			t.Error("Result node has empty Name")
		}
	}
}

// TestServiceSearchPagination verifies that pagination works correctly
func TestServiceSearchPagination(t *testing.T) {
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

	// Index 5 files with "test"
	nodes := make([]IndexNode, 5)
	for i := 0; i < 5; i++ {
		nodes[i] = IndexNode{
			Path:     fmt.Sprintf("/test/file%d.txt", i),
			Name:     fmt.Sprintf("file%d.txt", i),
			Size:     int64(i * 100),
			IsDir:    false,
			Modified: time.Now(),
		}
	}

	indexed, err := service.BatchIndex(nodes)
	if err != nil {
		t.Fatalf("BatchIndex failed: %v", err)
	}
	if indexed != 5 {
		t.Fatalf("Expected 5 indexed nodes, got %d", indexed)
	}

	// Search with PerPage=2
	req := SearchRequest{
		Query:   "test",
		Page:    1,
		PerPage: 2,
	}

	resp, err := service.Search(req)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// Verify Total is 5 but Results only has 2
	if resp.Total != 5 {
		t.Errorf("Expected Total=5, got %d", resp.Total)
	}
	if len(resp.Results) != 2 {
		t.Errorf("Expected 2 results (pagination), got %d", len(resp.Results))
	}

	// Search with Page=2
	req.Page = 2
	resp, err = service.Search(req)
	if err != nil {
		t.Fatalf("Search with page 2 failed: %v", err)
	}

	if resp.Total != 5 {
		t.Errorf("Expected Total=5, got %d", resp.Total)
	}
	if len(resp.Results) != 2 {
		t.Errorf("Expected 2 results (pagination with page 2), got %d", len(resp.Results))
	}
}

// TestServiceClear verifies that Clear empties the index
func TestServiceClear(t *testing.T) {
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

	// Index some nodes
	nodes := []IndexNode{
		{Path: "/test/file1.txt", Name: "file1.txt", Size: 100, IsDir: false, Modified: time.Now()},
		{Path: "/test/file2.txt", Name: "file2.txt", Size: 200, IsDir: false, Modified: time.Now()},
	}

	indexed, err := service.BatchIndex(nodes)
	if err != nil {
		t.Fatalf("BatchIndex failed: %v", err)
	}
	if indexed != 2 {
		t.Fatalf("Expected 2 indexed nodes, got %d", indexed)
	}

	// Verify documents are in index
	docCount, err := service.index.DocCount()
	if err != nil {
		t.Fatalf("Failed to get document count: %v", err)
	}
	if docCount != 2 {
		t.Errorf("Expected 2 documents before clear, got %d", docCount)
	}

	// Clear the index
	err = service.Clear(tempDir)
	if err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	// Verify index is empty
	docCount, err = service.index.DocCount()
	if err != nil {
		t.Fatalf("Failed to get document count after clear: %v", err)
	}
	if docCount != 0 {
		t.Errorf("Expected 0 documents after clear, got %d", docCount)
	}

	// Verify we can still index after clear
	indexed, err = service.BatchIndex(nodes[:1])
	if err != nil {
		t.Fatalf("BatchIndex after clear failed: %v", err)
	}
	if indexed != 1 {
		t.Fatalf("Expected 1 indexed node after clear, got %d", indexed)
	}
}

// TestServiceSearchScope verifies that scope filtering works correctly
func TestServiceSearchScope(t *testing.T) {
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

	// Index mixed nodes: 2 folders and 3 files, all with "test" in path
	nodes := []IndexNode{
		{Path: "/test/folder1", Name: "folder1", Size: 0, IsDir: true, Modified: time.Now()},
		{Path: "/test/folder2", Name: "folder2", Size: 0, IsDir: true, Modified: time.Now()},
		{Path: "/test/file1.txt", Name: "file1.txt", Size: 100, IsDir: false, Modified: time.Now()},
		{Path: "/test/file2.txt", Name: "file2.txt", Size: 200, IsDir: false, Modified: time.Now()},
		{Path: "/test/file3.txt", Name: "file3.txt", Size: 300, IsDir: false, Modified: time.Now()},
	}

	indexed, err := service.BatchIndex(nodes)
	if err != nil {
		t.Fatalf("BatchIndex failed: %v", err)
	}
	if indexed != 5 {
		t.Fatalf("Expected 5 indexed nodes, got %d", indexed)
	}

	// Test Scope 0: all results (default)
	req := SearchRequest{
		Query:   "test",
		Page:    1,
		PerPage: 20,
		Scope:   0,
	}

	resp, err := service.Search(req)
	if err != nil {
		t.Fatalf("Search with Scope=0 failed: %v", err)
	}

	if resp.Total != 5 {
		t.Errorf("Scope=0: Expected Total=5 (all), got %d", resp.Total)
	}

	// Test Scope 1: folders only
	req.Scope = 1
	resp, err = service.Search(req)
	if err != nil {
		t.Fatalf("Search with Scope=1 failed: %v", err)
	}

	if resp.Total != 2 {
		t.Errorf("Scope=1: Expected Total=2 (folders only), got %d", resp.Total)
	}

	// Verify all results are folders
	for _, node := range resp.Results {
		if !node.IsDir {
			t.Errorf("Scope=1: Expected only folders, but got file: %s", node.Path)
		}
	}

	// Test Scope 2: files only
	req.Scope = 2
	resp, err = service.Search(req)
	if err != nil {
		t.Fatalf("Search with Scope=2 failed: %v", err)
	}

	if resp.Total != 3 {
		t.Errorf("Scope=2: Expected Total=3 (files only), got %d", resp.Total)
	}

	// Verify all results are files
	for _, node := range resp.Results {
		if node.IsDir {
			t.Errorf("Scope=2: Expected only files, but got folder: %s", node.Path)
		}
	}
}
