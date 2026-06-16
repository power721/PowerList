package handles

import (
	"fmt"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/search/alist115"
	"github.com/OpenListTeam/OpenList/v4/server/common"
	"github.com/gin-gonic/gin"
)

var alist115Service *alist115.Service

// InitAlist115 initializes the alist115 indexing service
func InitAlist115(dataDir string) error {
	var err error
	alist115Service, err = alist115.NewService(dataDir)
	if err != nil {
		return fmt.Errorf("failed to initialize alist115 service: %w", err)
	}
	return nil
}

// Alist115ImportBatch handles batch import of 115 cloud storage nodes
func Alist115ImportBatch(c *gin.Context) {
	user := c.Request.Context().Value(conf.UserKey).(*model.User)
	if !user.IsAdmin() {
		common.ErrorResp(c, errs.PermissionDenied, 403)
		return
	}

	var req alist115.ImportBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	if len(req.Nodes) == 0 {
		common.ErrorStrResp(c, "no nodes to import", 400)
		return
	}

	if alist115Service == nil {
		common.ErrorStrResp(c, "alist115 service not initialized", 500)
		return
	}

	importedCount, err := alist115Service.BatchIndex(req.Nodes)
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	resp := alist115.ImportBatchResponse{
		Success:       true,
		ImportedCount: importedCount,
		FailedCount:   len(req.Nodes) - importedCount,
		Message:       fmt.Sprintf("Successfully imported %d nodes", importedCount),
	}

	common.SuccessResp(c, resp)
}

// Alist115Search handles search requests for 115 cloud storage
func Alist115Search(c *gin.Context) {
	user := c.Request.Context().Value(conf.UserKey).(*model.User)
	if user == nil {
		common.ErrorResp(c, errs.PermissionDenied, 403)
		return
	}

	var req alist115.SearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	if req.Query == "" {
		common.ErrorStrResp(c, "query cannot be empty", 400)
		return
	}

	if alist115Service == nil {
		common.ErrorStrResp(c, "alist115 service not initialized", 500)
		return
	}

	results, err := alist115Service.Search(req)
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	common.SuccessResp(c, results)
}

// Alist115Clear handles clearing the 115 index
func Alist115Clear(c *gin.Context) {
	user := c.Request.Context().Value(conf.UserKey).(*model.User)
	if !user.IsAdmin() {
		common.ErrorResp(c, errs.PermissionDenied, 403)
		return
	}

	if alist115Service == nil {
		common.ErrorStrResp(c, "alist115 service not initialized", 500)
		return
	}

	// Get data directory from config
	dataDir := conf.Conf.Database.DBFile
	if dataDir == "" {
		dataDir = "data"
	}

	if err := alist115Service.Clear(dataDir); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	common.SuccessWithMsgResp(c, "Successfully cleared 115 index")
}
