package http

import (
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
	"time"

	"github.com/gin-gonic/gin"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
)

func GetUserCheckinStatus(c *gin.Context) {
	month := c.DefaultQuery("month", time.Now().Format("2006-01"))
	payload, err := identityapp.LoadCheckinStatus(c.GetInt("id"), month)
	if err != nil {
		if err == identityapp.ErrCheckinDisabled {
			httpapi.ApiErrorMsg(c, err.Error())
			return
		}
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func DoUserCheckin(c *gin.Context) {
	payload, err := identityapp.PerformCheckin(c.GetInt("id"))
	if err != nil {
		if err == identityapp.ErrCheckinDisabled {
			httpapi.ApiErrorMsg(c, err.Error())
			return
		}
		httpapi.ApiError(c, err)
		return
	}

	c.JSON(200, gin.H{
		"success": true,
		"message": "签到成功",
		"data":    payload,
	})
}
