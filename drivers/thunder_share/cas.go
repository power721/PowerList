package thunder_share

import (
	"context"
	"errors"
	"fmt"

	_189pc "github.com/OpenListTeam/OpenList/v4/drivers/189pc"
	"github.com/OpenListTeam/OpenList/v4/internal/casfile"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/internal/stream"
)

var resolveThunderShareCASSourceLink = func(ctx context.Context, d *ThunderShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	return resolveThunderShareLink(ctx, d, file, args)
}

var openThunderShareCASStream = func(ctx context.Context, file model.Obj, link *model.Link) (model.FileStreamer, error) {
	return stream.NewSeekableStream(&stream.FileStream{Ctx: ctx, Obj: file}, link)
}

var get189CloudPCCandidates = list189CloudPCCandidates

var restore189CloudPCCAS = func(ctx context.Context, cloud *_189pc.Cloud189PC, casFileName string, info *casfile.Info) (*model.Link, error) {
	return cloud.RestoreCASForPlayback(ctx, casFileName, info)
}

var readThunderShareCASInfo = func(ctx context.Context, d *ThunderShare, file model.Obj, args model.LinkArgs) (*casfile.Info, error) {
	return d.readSharedCASInfo(ctx, file, args)
}

var restoreThunderShareCASInfo = restoreSharedCASInfo

var resolveThunderShareCASLink = func(ctx context.Context, d *ThunderShare, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	return d.resolveCASPlayback(ctx, file, args)
}

func list189CloudPCCandidates() []*_189pc.Cloud189PC {
	all := make([]*_189pc.Cloud189PC, 0)
	for _, storage := range op.GetStorages("189CloudPC") {
		if cloud, ok := storage.(*_189pc.Cloud189PC); ok {
			all = append(all, cloud)
		}
	}
	preferred, _ := op.GetFirstDriver("189CloudPC", 0).(*_189pc.Cloud189PC)
	return distinct189CloudPCCandidates(preferred, all)
}

func distinct189CloudPCCandidates(preferred *_189pc.Cloud189PC, all []*_189pc.Cloud189PC) []*_189pc.Cloud189PC {
	candidates := make([]*_189pc.Cloud189PC, 0, len(all)+1)
	seen := make(map[uint]struct{})
	appendCandidate := func(cloud *_189pc.Cloud189PC) {
		if cloud == nil {
			return
		}
		if _, ok := seen[cloud.ID]; ok {
			return
		}
		seen[cloud.ID] = struct{}{}
		candidates = append(candidates, cloud)
	}
	appendCandidate(preferred)
	for _, cloud := range all {
		appendCandidate(cloud)
	}
	return candidates
}

func restoreSharedCASInfo(ctx context.Context, casFileName string, info *casfile.Info) (*model.Link, error) {
	candidates := get189CloudPCCandidates()
	if len(candidates) == 0 {
		return nil, errors.New("找不到天翼云盘帐号")
	}
	failures := make([]error, 0, len(candidates))
	for _, cloud := range candidates {
		link, err := restore189CloudPCCAS(ctx, cloud, casFileName, info)
		if err == nil {
			return link, nil
		}
		failures = append(failures, fmt.Errorf("189CloudPC[%d]: %w", cloud.ID, err))
	}
	return nil, fmt.Errorf("所有天翼云盘帐号均无法还原 CAS: %w", errors.Join(failures...))
}

func (d *ThunderShare) resolveCASPlayback(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	info, err := readThunderShareCASInfo(ctx, d, file, args)
	if err != nil {
		return nil, err
	}
	return restoreThunderShareCASInfo(ctx, file.GetName(), info)
}

func (d *ThunderShare) readSharedCASInfo(ctx context.Context, file model.Obj, args model.LinkArgs) (*casfile.Info, error) {
	link, err := resolveThunderShareCASSourceLink(ctx, d, file, args)
	if err != nil {
		return nil, err
	}
	if link == nil {
		return nil, errors.New("找不到迅雷云盘帐号")
	}
	casStream, err := openThunderShareCASStream(ctx, file, link)
	if err != nil {
		return nil, err
	}
	defer casStream.Close()
	return casfile.ParseReader(casStream)
}
