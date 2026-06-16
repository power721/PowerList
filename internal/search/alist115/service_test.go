package alist115

import (
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
	defer os.RemoveAll(tempDir)

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
	defer os.RemoveAll(tempDir)

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
