# 115 Cloud Storage Indexing API

This document describes the HTTP API endpoints for the 115 cloud storage indexing feature.

## Endpoints

### 1. Import Batch - POST /api/fs/115/import-batch

Import multiple 115 cloud storage nodes into the search index.

**Authentication:** Admin only

**Request Body:**
```json
{
  "nodes": [
    {
      "path": "/movies/action/movie1.mp4",
      "name": "movie1.mp4",
      "size": 1073741824,
      "is_dir": false,
      "modified": "2024-01-01T00:00:00Z",
      "parent_path": "/movies/action",
      "depth": 2,
      "child_count": 0
    }
  ]
}
```

**Response:**
```json
{
  "success": true,
  "imported_count": 1,
  "failed_count": 0,
  "message": "Successfully imported 1 nodes"
}
```

### 2. Search - GET/POST /api/fs/115/search

Search indexed 115 cloud storage nodes.

**Authentication:** Authenticated users

**Request Body:**
```json
{
  "query": "movie",
  "page": 1,
  "per_page": 20,
  "scope": 0
}
```

**Parameters:**
- `query` (string, required): Search query
- `page` (int, optional): Page number (default: 1)
- `per_page` (int, optional): Results per page (default: 20, max: 100)
- `scope` (int, optional): Search scope
  - `0`: All (files and folders) - default
  - `1`: Folders only
  - `2`: Files only

**Response:**
```json
{
  "query": "movie",
  "total": 100,
  "results": [
    {
      "path": "/movies/action/movie1.mp4",
      "name": "movie1.mp4",
      "size": 1073741824,
      "is_dir": false
    }
  ]
}
```

### 3. Clear Index - DELETE /api/fs/115/clear

Clear the entire 115 index.

**Authentication:** Admin only

**Response:**
```json
{
  "message": "Successfully cleared 115 index"
}
```

## Implementation Details

- Index location: `{dataDir}/indexes/115`
- Batch processing: Maximum 10,000 nodes per batch
- Path mapping: Emoji prefixes are automatically removed
- Document ID: Generated from SHA-256 hash of path (idempotent updates)
- Search sorting: Results sorted by indexed_at descending (most recent first)

## Usage Example

```bash
# Import nodes
curl -X POST http://localhost:5244/api/fs/115/import-batch \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"nodes":[{"path":"/test.txt","name":"test.txt","size":100,"is_dir":false}]}'

# Search
curl -X POST http://localhost:5244/api/fs/115/search \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"query":"test","page":1,"per_page":20,"scope":0}'

# Clear index
curl -X DELETE http://localhost:5244/api/fs/115/clear \
  -H "Authorization: Bearer YOUR_TOKEN"
```
