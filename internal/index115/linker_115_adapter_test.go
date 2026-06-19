package index115

import (
	"context"
	"errors"
	"testing"
)

func TestDriver115ShareClientResolveShareLinkUsesCookieClient(t *testing.T) {
	factory := &fakeDriver115Factory{
		client: &fakeDriver115Client{
			downloadURL: "https://example.com/video.m3u8",
		},
	}
	adapter := &driver115ShareClient{factory: factory}

	link, sha1, err := adapter.ResolveShareLink(context.Background(), "UID=1;CID=2;SEID=3", "sw1", "rc1", "file1")
	if err != nil {
		t.Fatalf("ResolveShareLink() error = %v", err)
	}
	if link.URL != "https://example.com/video.m3u8" {
		t.Fatalf("unexpected link: %+v", link)
	}
	if sha1 != "" {
		t.Fatalf("expected empty sha1 from resolve, got %q", sha1)
	}
	if factory.lastCookie != "UID=1;CID=2;SEID=3" {
		t.Fatalf("expected cookie forwarded to factory, got %q", factory.lastCookie)
	}
	if factory.client.lastShareCode != "sw1" || factory.client.lastReceiveCode != "rc1" || factory.client.lastFileID != "file1" {
		t.Fatalf("unexpected download args: %+v", factory.client)
	}
}

func TestDriver115ShareClientDeleteReceivedBySHA1DeletesMatchingFiles(t *testing.T) {
	factory := &fakeDriver115Factory{
		client: &fakeDriver115Client{
			rootFiles: []driver115File{
				{FileID: "recv", Name: "最近接收", IsDir: true},
			},
			dirFiles: map[string][]driver115File{
				"recv": {
					{FileID: "a", Sha1: "keep"},
					{FileID: "b", Sha1: "target"},
				},
			},
		},
	}
	adapter := &driver115ShareClient{factory: factory}

	if err := adapter.DeleteReceivedBySHA1(context.Background(), "UID=1;CID=2;SEID=3", "target"); err != nil {
		t.Fatalf("DeleteReceivedBySHA1() error = %v", err)
	}
	if len(factory.client.deletedIDs) != 1 || factory.client.deletedIDs[0] != "b" {
		t.Fatalf("expected file b to be deleted, got %+v", factory.client.deletedIDs)
	}
}

func TestDriver115ShareClientDeleteReceivedBySHA1IgnoresMissingReceiveDir(t *testing.T) {
	factory := &fakeDriver115Factory{
		client: &fakeDriver115Client{},
	}
	adapter := &driver115ShareClient{factory: factory}

	if err := adapter.DeleteReceivedBySHA1(context.Background(), "UID=1;CID=2;SEID=3", "target"); err != nil {
		t.Fatalf("DeleteReceivedBySHA1() error = %v", err)
	}
	if len(factory.client.deletedIDs) != 0 {
		t.Fatalf("expected no deletes, got %+v", factory.client.deletedIDs)
	}
}

func TestDriver115ShareClientResolveShareLinkPropagatesFactoryError(t *testing.T) {
	adapter := &driver115ShareClient{
		factory: &fakeDriver115Factory{err: errors.New("bad cookie")},
	}
	_, _, err := adapter.ResolveShareLink(context.Background(), "bad", "sw1", "rc1", "file1")
	if err == nil {
		t.Fatal("expected error")
	}
}

type fakeDriver115Factory struct {
	client     *fakeDriver115Client
	err        error
	lastCookie string
}

func (f *fakeDriver115Factory) NewClient(ctx context.Context, cookie string) (driver115Client, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.lastCookie = cookie
	return f.client, nil
}

type fakeDriver115Client struct {
	downloadURL     string
	lastShareCode   string
	lastReceiveCode string
	lastFileID      string
	rootFiles       []driver115File
	dirFiles        map[string][]driver115File
	deletedIDs      []string
}

func (f *fakeDriver115Client) DownloadByShareCode(ctx context.Context, shareCode, receiveCode, fileID string) (ResolvedLink, error) {
	f.lastShareCode = shareCode
	f.lastReceiveCode = receiveCode
	f.lastFileID = fileID
	return ResolvedLink{URL: f.downloadURL, ExpiredIn: 14400}, nil
}

func (f *fakeDriver115Client) ListDir(ctx context.Context, dirID string) ([]driver115File, error) {
	if dirID == "0" {
		return f.rootFiles, nil
	}
	return f.dirFiles[dirID], nil
}

func (f *fakeDriver115Client) Delete(ctx context.Context, fileID string) error {
	f.deletedIDs = append(f.deletedIDs, fileID)
	return nil
}
