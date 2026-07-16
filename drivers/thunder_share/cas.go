package thunder_share

import (
	"context"

	"github.com/OpenListTeam/OpenList/v4/internal/casfile"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/stream"
)

var resolveThunderShareCASSourceLink = func(ctx context.Context, d *ThunderShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	return resolveThunderShareLink(ctx, d, file, args)
}

var openThunderShareCASStream = func(ctx context.Context, file model.Obj, link *model.Link) (model.FileStreamer, error) {
	return stream.NewSeekableStream(&stream.FileStream{Ctx: ctx, Obj: file}, link)
}

func (d *ThunderShare) readSharedCASInfo(ctx context.Context, file model.Obj, args model.LinkArgs) (*casfile.Info, error) {
	link, err := resolveThunderShareCASSourceLink(ctx, d, file, args)
	if err != nil {
		return nil, err
	}
	casStream, err := openThunderShareCASStream(ctx, file, link)
	if err != nil {
		return nil, err
	}
	defer casStream.Close()
	return casfile.ParseReader(casStream)
}
