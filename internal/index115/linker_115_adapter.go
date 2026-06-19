package index115

import (
	"context"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	driver115 "github.com/power721/115driver/pkg/driver"
)

const receiveDirName = "最近接收"

type driver115Factory interface {
	NewClient(ctx context.Context, cookie string) (driver115Client, error)
}

type driver115Client interface {
	DownloadByShareCode(ctx context.Context, shareCode, receiveCode, fileID string) (ResolvedLink, error)
	ListDir(ctx context.Context, dirID string) ([]driver115File, error)
	Delete(ctx context.Context, fileID string) error
}

type driver115File struct {
	FileID string
	Name   string
	Sha1   string
	IsDir  bool
}

type driver115ShareClient struct {
	factory driver115Factory
}

func NewDriver115ShareClient() ShareDownloadClient {
	return &driver115ShareClient{
		factory: defaultDriver115Factory{},
	}
}

func (c *driver115ShareClient) ResolveShareLink(ctx context.Context, cookie string, shareCode string, receiveCode string, fileID string) (ResolvedLink, string, error) {
	client, err := c.factory.NewClient(ctx, cookie)
	if err != nil {
		return ResolvedLink{}, "", err
	}
	link, err := client.DownloadByShareCode(ctx, shareCode, receiveCode, fileID)
	if err != nil {
		return ResolvedLink{}, "", err
	}
	return link, "", nil
}

func (c *driver115ShareClient) DeleteReceivedBySHA1(ctx context.Context, cookie string, sha1 string) error {
	if sha1 == "" {
		return nil
	}
	client, err := c.factory.NewClient(ctx, cookie)
	if err != nil {
		return err
	}
	rootFiles, err := client.ListDir(ctx, "0")
	if err != nil {
		return err
	}
	receiveDirID := ""
	for _, file := range rootFiles {
		if file.IsDir && file.Name == receiveDirName {
			receiveDirID = file.FileID
			break
		}
	}
	if receiveDirID == "" {
		return nil
	}
	files, err := client.ListDir(ctx, receiveDirID)
	if err != nil {
		return err
	}
	for _, file := range files {
		if file.Sha1 == sha1 {
			return client.Delete(ctx, file.FileID)
		}
	}
	return nil
}

type defaultDriver115Factory struct{}

func (defaultDriver115Factory) NewClient(ctx context.Context, cookie string) (driver115Client, error) {
	_ = ctx
	cr := &driver115.Credential{}
	if err := cr.FromCookie(cookie); err != nil {
		return nil, err
	}
	client := driver115.New(
		driver115.UA(conf.UA115Browser),
		driver115.InsecureSkipVerify(conf.Conf.TlsInsecureSkipVerify),
	).ImportCredential(cr)
	if err := client.CookieCheck(); err != nil {
		return nil, err
	}
	return &defaultDriver115Client{client: client}, nil
}

type defaultDriver115Client struct {
	client *driver115.Pan115Client
}

func (c *defaultDriver115Client) DownloadByShareCode(ctx context.Context, shareCode, receiveCode, fileID string) (ResolvedLink, error) {
	_ = ctx
	info, err := c.client.DownloadByShareCode(shareCode, receiveCode, fileID)
	if err != nil {
		return ResolvedLink{}, err
	}
	return ResolvedLink{
		URL:       info.URL.URL,
		ExpiredIn: 4 * 60 * 60,
	}, nil
}

func (c *defaultDriver115Client) ListDir(ctx context.Context, dirID string) ([]driver115File, error) {
	_ = ctx
	files, err := c.client.ListWithLimit(dirID, driver115.FileListLimit, driver115.WithMultiUrls())
	if err != nil {
		return nil, err
	}
	items := make([]driver115File, 0, len(*files))
	for _, file := range *files {
		items = append(items, driver115File{
			FileID: file.FileID,
			Name:   file.Name,
			Sha1:   file.Sha1,
			IsDir:  file.IsDir(),
		})
	}
	return items, nil
}

func (c *defaultDriver115Client) Delete(ctx context.Context, fileID string) error {
	_ = ctx
	return c.client.Delete(fileID)
}
