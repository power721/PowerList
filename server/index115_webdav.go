package server

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/index115"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	xwebdav "golang.org/x/net/webdav"

	"github.com/gin-gonic/gin"
)

type index115BrowseProvider interface {
	Browse(ctx context.Context, req index115.BrowseRequest) ([]index115.FileItem, error)
}

var index115BrowseService index115BrowseProvider
var index115DAVHandler *xwebdav.Handler

func SetIndex115BrowseService(service index115BrowseProvider) {
	index115BrowseService = service
}

func WebDavIndex115(dav *gin.RouterGroup) {
	handler := getIndex115DAVHandler()
	dav.Use(index115WebDAVAuth, index115WebDAVReadOnly)
	dav.Any("/*path", func(c *gin.Context) {
		handler.ServeHTTP(c.Writer, c.Request)
	})
	dav.Any("", func(c *gin.Context) {
		handler.ServeHTTP(c.Writer, c.Request)
	})
	dav.Handle("PROPFIND", "/*path", func(c *gin.Context) {
		handler.ServeHTTP(c.Writer, c.Request)
	})
	dav.Handle("PROPFIND", "", func(c *gin.Context) {
		handler.ServeHTTP(c.Writer, c.Request)
	})
}

func getIndex115DAVHandler() *xwebdav.Handler {
	if index115DAVHandler != nil {
		return index115DAVHandler
	}
	index115DAVHandler = &xwebdav.Handler{
		Prefix:     index115WebDAVPrefix(),
		FileSystem: &index115WebDAVFS{},
		LockSystem: xwebdav.NewMemLS(),
	}
	return index115DAVHandler
}

func index115WebDAVPrefix() string {
	if conf.URL != nil {
		return path.Join(conf.URL.Path, "/dav/index115")
	}
	return "/dav/index115"
}

func isIndex115WebDAVPath(p string) bool {
	prefix := index115WebDAVPrefix()
	return p == prefix || strings.HasPrefix(p, prefix+"/")
}

func index115WebDAVAuth(c *gin.Context) {
	if c.Request.Method == http.MethodOptions {
		c.Next()
		return
	}
	token := setting.GetStr(conf.Token)
	if token == "" {
		c.Next()
		return
	}
	auth := c.GetHeader("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		auth = strings.TrimPrefix(auth, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(auth), []byte(token)) == 1 {
			c.Next()
			return
		}
	}
	c.Writer.Header()["WWW-Authenticate"] = []string{`Bearer realm="openlist-index115"`}
	c.AbortWithStatus(http.StatusUnauthorized)
}

func index115WebDAVReadOnly(c *gin.Context) {
	switch c.Request.Method {
	case http.MethodOptions, http.MethodGet, http.MethodHead, "PROPFIND":
		c.Next()
	default:
		c.AbortWithStatus(http.StatusForbidden)
	}
}

type index115WebDAVFS struct{}

func (fs *index115WebDAVFS) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	return os.ErrPermission
}

func (fs *index115WebDAVFS) RemoveAll(ctx context.Context, name string) error {
	return os.ErrPermission
}

func (fs *index115WebDAVFS) Rename(ctx context.Context, oldName, newName string) error {
	return os.ErrPermission
}

func (fs *index115WebDAVFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	entry, err := fs.resolve(ctx, name)
	if err != nil {
		return nil, err
	}
	return entry.info, nil
}

func (fs *index115WebDAVFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (xwebdav.File, error) {
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC) != 0 {
		return nil, os.ErrPermission
	}
	entry, err := fs.resolve(ctx, name)
	if err != nil {
		return nil, err
	}
	if entry.info.IsDir() {
		return &index115WebDAVFile{
			info:     entry.info,
			children: entry.children,
			dirPos:   0,
			reader:   bytes.NewReader(nil),
		}, nil
	}
	return &index115WebDAVFile{
		info:   entry.info,
		reader: bytes.NewReader(nil),
	}, nil
}

func (fs *index115WebDAVFS) resolve(ctx context.Context, name string) (*index115ResolvedEntry, error) {
	if index115BrowseService == nil {
		return nil, os.ErrNotExist
	}
	clean := cleanWebDAVPath(name)
	if clean == "/" {
		items, err := index115BrowseService.Browse(ctx, index115.BrowseRequest{})
		if err != nil {
			return nil, err
		}
		children := make([]os.FileInfo, 0, len(items))
		for _, item := range items {
			children = append(children, newIndex115FileInfo(item, true))
		}
		sort.Slice(children, func(i, j int) bool {
			return children[i].Name() < children[j].Name()
		})
		return &index115ResolvedEntry{
			info:     newVirtualDirInfo("", "/", time.Now()),
			children: children,
		}, nil
	}

	parts := splitWebDAVPath(clean)
	rootItems, err := index115BrowseService.Browse(ctx, index115.BrowseRequest{})
	if err != nil {
		return nil, err
	}
	var current index115.FileItem
	found := false
	for _, item := range rootItems {
		if item.Name == parts[0] {
			current = item
			found = true
			break
		}
	}
	if !found {
		return nil, os.ErrNotExist
	}
	if len(parts) == 1 {
		children, err := fs.childInfos(ctx, current.ShareCode, "0")
		if err != nil {
			return nil, err
		}
		return &index115ResolvedEntry{
			info:     newIndex115FileInfo(current, true),
			children: children,
		}, nil
	}

	parentID := "0"
	var currentInfo os.FileInfo = newIndex115FileInfo(current, true)
	for idx := 1; idx < len(parts); idx++ {
		items, err := index115BrowseService.Browse(ctx, index115.BrowseRequest{
			ShareCode: current.ShareCode,
			ParentID:  parentID,
		})
		if err != nil {
			return nil, err
		}
		match := index115.FileItem{}
		found = false
		for _, item := range items {
			if item.Name == parts[idx] {
				match = item
				found = true
				break
			}
		}
		if !found {
			return nil, os.ErrNotExist
		}
		currentInfo = newIndex115FileInfo(match, idx == 1)
		// Crossing from a group node (sentinel share_code like "grp1") into one
		// of its member shares: switch the active share for deeper drilling.
		// Normal intra-share paths are unaffected (match.ShareCode == current.ShareCode).
		if match.ShareCode != "" && match.ShareCode != current.ShareCode {
			current = match
		}
		if idx == len(parts)-1 {
			children := []os.FileInfo(nil)
			if match.IsDir {
				children, err = fs.childInfos(ctx, current.ShareCode, match.FileID)
				if err != nil {
					return nil, err
				}
			}
			return &index115ResolvedEntry{
				info:     currentInfo,
				children: children,
			}, nil
		}
		parentID = match.FileID
	}
	return nil, os.ErrNotExist
}

func (fs *index115WebDAVFS) childInfos(ctx context.Context, shareCode, parentID string) ([]os.FileInfo, error) {
	items, err := index115BrowseService.Browse(ctx, index115.BrowseRequest{
		ShareCode: shareCode,
		ParentID:  parentID,
	})
	if err != nil {
		return nil, err
	}
	children := make([]os.FileInfo, 0, len(items))
	for _, item := range items {
		children = append(children, newIndex115FileInfo(item, false))
	}
	return children, nil
}

type index115ResolvedEntry struct {
	info     os.FileInfo
	children []os.FileInfo
}

type index115WebDAVFile struct {
	info     os.FileInfo
	children []os.FileInfo
	dirPos   int
	reader   *bytes.Reader
}

func (f *index115WebDAVFile) Close() error { return nil }
func (f *index115WebDAVFile) Read(p []byte) (int, error) {
	return f.reader.Read(p)
}
func (f *index115WebDAVFile) Seek(offset int64, whence int) (int64, error) {
	return f.reader.Seek(offset, whence)
}
func (f *index115WebDAVFile) Readdir(count int) ([]os.FileInfo, error) {
	if !f.info.IsDir() {
		return nil, errors.New("not a directory")
	}
	if f.dirPos >= len(f.children) && count > 0 {
		return nil, io.EOF
	}
	if count <= 0 {
		result := make([]os.FileInfo, len(f.children)-f.dirPos)
		copy(result, f.children[f.dirPos:])
		f.dirPos = len(f.children)
		return result, nil
	}
	end := f.dirPos + count
	if end > len(f.children) {
		end = len(f.children)
	}
	result := make([]os.FileInfo, end-f.dirPos)
	copy(result, f.children[f.dirPos:end])
	f.dirPos = end
	return result, nil
}
func (f *index115WebDAVFile) Stat() (os.FileInfo, error) { return f.info, nil }
func (f *index115WebDAVFile) Write(p []byte) (int, error) {
	return 0, os.ErrPermission
}

type index115FileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	dir     bool
}

func newIndex115FileInfo(item index115.FileItem, isRoot bool) os.FileInfo {
	mod := unixTimeOrNow(item.UpdatedAt)
	name := item.Name
	if isRoot && item.ShareTitle != "" {
		name = item.ShareTitle
	}
	mode := os.FileMode(0o444)
	if item.IsDir {
		mode = os.ModeDir | 0o555
	}
	return index115FileInfo{
		name:    name,
		size:    item.Size,
		mode:    mode,
		modTime: mod,
		dir:     item.IsDir,
	}
}

func newVirtualDirInfo(name, _ string, mod time.Time) os.FileInfo {
	return index115FileInfo{
		name:    name,
		size:    0,
		mode:    os.ModeDir | 0o555,
		modTime: mod,
		dir:     true,
	}
}

func (i index115FileInfo) Name() string       { return i.name }
func (i index115FileInfo) Size() int64        { return i.size }
func (i index115FileInfo) Mode() os.FileMode  { return i.mode }
func (i index115FileInfo) ModTime() time.Time { return i.modTime }
func (i index115FileInfo) IsDir() bool        { return i.dir }
func (i index115FileInfo) Sys() any           { return nil }

func cleanWebDAVPath(name string) string {
	if name == "" {
		return "/"
	}
	clean := path.Clean("/" + strings.TrimPrefix(name, "/"))
	if clean == "." {
		return "/"
	}
	return clean
}

func splitWebDAVPath(clean string) []string {
	if clean == "/" {
		return nil
	}
	return strings.Split(strings.TrimPrefix(clean, "/"), "/")
}

func unixTimeOrNow(ts int64) time.Time {
	if ts <= 0 {
		return time.Now()
	}
	return time.Unix(ts, 0)
}
