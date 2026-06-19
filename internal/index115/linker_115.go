package index115

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

var ErrLinkClientNotConfigured = errors.New("index115 link client not configured")

type ResolvedLink struct {
	URL       string
	ExpiredIn int64
}

type ShareDownloadClient interface {
	ResolveShareLink(ctx context.Context, cookie string, shareCode string, receiveCode string, file FileItem) (ResolvedLink, string, error)
	DeleteReceivedByFileID(ctx context.Context, cookie string, fileID string) error
}

type LinkResolver struct {
	client ShareDownloadClient
	leases *leaseRegistry
	delay  time.Duration
}

func NewLinkResolver(client ShareDownloadClient, delay time.Duration) *LinkResolver {
	return &LinkResolver{
		client: client,
		leases: newLeaseRegistry(delay),
		delay:  delay,
	}
}

func (r *LinkResolver) Resolve(ctx context.Context, req LinkRequest, file FileItem) (ResolvedLink, error) {
	if r.client == nil {
		return ResolvedLink{}, ErrLinkClientNotConfigured
	}
	receiveCode := r.resolveReceiveCode(req.ReceiveCode, file.ReceiveCode)
	link, receivedFileID, err := r.client.ResolveShareLink(ctx, req.Cookie, req.ShareCode, receiveCode, file)
	if err != nil {
		return ResolvedLink{}, err
	}
	if receivedFileID != "" {
		r.scheduleCleanup(req.Cookie, file.FileID, receivedFileID)
	}
	return link, nil
}

func (r *LinkResolver) resolveReceiveCode(requestCode, shareCode string) string {
	if requestCode != "" {
		return requestCode
	}
	return shareCode
}

func (r *LinkResolver) leaseKey(cookie, fileID string) string {
	sum := sha1.Sum([]byte(cookie + ":" + fileID))
	return hex.EncodeToString(sum[:])
}

func (r *LinkResolver) scheduleCleanup(cookie, fileID, receivedFileID string) {
	if r.client == nil || r.leases == nil || r.delay <= 0 {
		return
	}
	key := r.leaseKey(cookie, fileID)
	expiresAt := r.leases.Touch(key)
	go func() {
		time.Sleep(r.delay)
		if !r.leases.Expired(key, expiresAt) {
			return
		}
		_ = r.client.DeleteReceivedByFileID(context.Background(), cookie, receivedFileID)
	}()
}

type leaseRegistry struct {
	mu    sync.Mutex
	ttl   time.Duration
	items map[string]time.Time
}

func newLeaseRegistry(ttl time.Duration) *leaseRegistry {
	return &leaseRegistry{
		ttl:   ttl,
		items: map[string]time.Time{},
	}
}

func (r *leaseRegistry) Touch(key string) time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	expiresAt := time.Now().Add(r.ttl)
	r.items[key] = expiresAt
	return expiresAt
}

func (r *leaseRegistry) Expired(key string, at time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.items[key]
	if !ok {
		return true
	}
	return !current.After(at)
}
