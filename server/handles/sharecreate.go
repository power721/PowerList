package handles

import (
	_115 "github.com/OpenListTeam/OpenList/v4/drivers/115"
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/server/common"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// ShareCreateReq 在 115 云盘账号(cookie 版)存储上为指定目录创建永久分享。
type ShareCreateReq struct {
	// Path 目标目录(网盘账号挂载下的绝对路径)
	Path string `json:"path" binding:"required"`
}

// FsShareCreate 创建 115 永久分享:share/send + updateshare(-1) 两步在驱动内串好,
// 返回 share_code / receive_code / share_url。分享是快照语义——调用方建完即可
// 删除盘内源文件;追加内容须再建新分享(新 share_code)。
func FsShareCreate(c *gin.Context) {
	user := c.Request.Context().Value(conf.UserKey).(*model.User)

	var req ShareCreateReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	path, err := user.JoinPath(req.Path)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}

	// 权限:创建分享属管理动作,要求该路径可写
	meta, err := op.GetNearestMeta(path)
	if err != nil && !errors.Is(errors.Cause(err), errs.MetaNotFound) {
		common.ErrorResp(c, err, 500, true)
		return
	}
	if !common.CanWrite(user, meta, path) {
		common.ErrorResp(c, errs.PermissionDenied, 403)
		return
	}

	storage, actualPath, err := op.GetStorageAndActualPath(path)
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	pan115, ok := storage.(*_115.Pan115)
	if !ok {
		common.ErrorResp(c, errors.New("目标存储不是115云盘账号(cookie 版)驱动,不支持创建分享"), 400)
		return
	}

	dir, err := op.Get(c.Request.Context(), storage, actualPath)
	if err != nil {
		common.ErrorResp(c, errors.Wrap(err, "获取目标目录失败"), 500)
		return
	}
	if !dir.IsDir() {
		common.ErrorResp(c, errs.NotFolder, 400)
		return
	}

	result, err := pan115.CreatePermanentShare(c.Request.Context(), dir)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	common.SuccessResp(c, result)
}
