package http

import (
	"github.com/gin-gonic/gin"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
	workflowapp "github.com/sh2001sh/new-api/internal/workflow/app"
)

// GetGeneMapShare returns a public gene-map share snapshot by token.
func GetGeneMapShare(c *gin.Context) {
	payload, err := workflowapp.BuildPublicGeneMapShare(c.Param("token"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}
