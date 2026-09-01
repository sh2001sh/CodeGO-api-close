package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
)

type favoriteModelRequest struct {
	ModelID  int  `json:"model_id" binding:"required"`
	Favorite bool `json:"favorite"`
}

func GetFavoriteModels(c *gin.Context) {
	ids, err := identityapp.GetFavoriteModelIDs(c.GetInt("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, gin.H{"model_ids": ids})
}

func SetFavoriteModel(c *gin.Context) {
	var req favoriteModelRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.ModelID <= 0 {
		httpapi.ApiErrorMsg(c, "模型 ID 无效")
		return
	}
	ids, err := identityapp.SetFavoriteModel(c.GetInt("id"), req.ModelID, req.Favorite)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"model_ids": ids}})
}
