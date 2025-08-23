package handles

import (
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/server/common"
	"github.com/gin-gonic/gin"
)

func UpdateToken(c *gin.Context) {
	var req model.Token
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	if err := op.SaveToken(&req); err != nil {
		common.ErrorResp(c, err, 500, true)
	} else {
		common.SuccessResp(c)
	}
}

func DeleteToken(c *gin.Context) {
	key := c.Query("key")
	if err := op.DeleteTokenByKey(key); err != nil {
		common.ErrorResp(c, err, 500, true)
		return
	}
	common.SuccessResp(c)
}

func GetToken(c *gin.Context) {
	key := c.Query("key")
	token, err := op.GetTokenByKey(key)
	if err != nil {
		common.ErrorResp(c, err, 500, true)
		return
	}
	common.SuccessResp(c, token)
}

func GetTokens(c *gin.Context) {
	tokens, err := op.GetTokens()
	if err != nil {
		common.ErrorResp(c, err, 500, true)
		return
	}
	common.SuccessResp(c, tokens)
}
