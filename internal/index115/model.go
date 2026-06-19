package index115

type ShareSummary struct {
	ShareCode   string
	ReceiveCode string
	ShareTitle  string
	Path        string
	IsDir       bool
	FileCount   int64
	DirCount    int64
	UpdatedAt   int64
}

type FileItem struct {
	FileID      string
	ShareCode   string
	ReceiveCode string
	ShareTitle  string
	ParentID    string
	Name        string
	Path        string
	Size        int64
	IsDir       bool
	Ext         string
	SHA1        string
	UpdatedAt   int64
}

type SearchRequest struct {
	Query     string
	Page      int
	PerPage   int
	Scope     string
	ShareCode string
}

type BrowseRequest struct {
	ShareCode   string
	ReceiveCode string
	ParentID    string
}

type LinkRequest struct {
	Cookie      string `json:"cookie"`
	ShareCode   string `json:"share_code"`
	ReceiveCode string `json:"receive_code"`
	FileID      string `json:"file_id"`
}
