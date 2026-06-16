// Package alist115 provides data models and indexing support for 115 cloud storage.
// It enables full-text search across 115 cloud files and directories through
// bleve indexing and provides API structures for import and search operations.
package alist115

import "time"

// IndexNode represents a file or directory node in the 115 index
type IndexNode struct {
	Path       string    `json:"path"`        // Full path
	Name       string    `json:"name"`        // File/directory name
	Size       int64     `json:"size"`        // File size in bytes
	IsDir      bool      `json:"is_dir"`      // Whether this is a directory
	Modified   time.Time `json:"modified"`    // Last modified time
	ParentPath string    `json:"parent_path"` // Parent directory path
	Depth      int       `json:"depth"`       // Directory depth (0 for root)
	ChildCount int       `json:"child_count"` // Number of children (for directories)
}

// ImportBatchRequest represents a batch import request
type ImportBatchRequest struct {
	Nodes []IndexNode `json:"nodes"` // Nodes to import
}

// ImportBatchResponse represents a batch import response
type ImportBatchResponse struct {
	Success       bool   `json:"success"`        // Whether import succeeded
	ImportedCount int    `json:"imported_count"` // Number of nodes imported
	FailedCount   int    `json:"failed_count"`   // Number of nodes that failed
	Message       string `json:"message"`        // Status message
}

// SearchRequest represents a search request
type SearchRequest struct {
	Query   string `json:"query"`    // Search query
	Page    int    `json:"page"`     // Page number (1-based)
	PerPage int    `json:"per_page"` // Results per page
	Scope   int    `json:"scope"`    // Search scope: 0=all, 1=folder only, 2=file only
}

// SearchResponse represents a search response
type SearchResponse struct {
	Query   string       `json:"query"`   // Original query
	Total   int          `json:"total"`   // Total number of results
	Results []SearchNode `json:"results"` // Search results
}

// SearchNode represents a search result node
type SearchNode struct {
	Path  string `json:"path"`   // Full path
	Name  string `json:"name"`   // File/directory name
	Size  int64  `json:"size"`   // File size in bytes
	IsDir bool   `json:"is_dir"` // Whether this is a directory
}
