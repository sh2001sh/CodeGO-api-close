package http

import (
	"github.com/gin-gonic/gin"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
)

func getUnifiedCreditMigrationDetail(c *gin.Context) {
	payload, err := commerceapp.GetUnifiedCreditMigrationDetail(c.GetInt("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}
