package http

import (
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
	stdhttp "net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	gatewayexecutionapp "github.com/sh2001sh/new-api/internal/gateway/execution/app"
)

func TestChannel(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}

	testModel := c.Query("model")
	endpointType := c.Query("endpoint_type")
	isStream, _ := strconv.ParseBool(c.Query("stream"))
	consumedTime, newAPIError, localErr := gatewayexecutionapp.TestChannelByID(channelID, testModel, endpointType, isStream)
	if localErr != nil {
		resp := gin.H{
			"success": false,
			"message": localErr.Error(),
			"time":    0.0,
		}
		if newAPIError != nil {
			resp["error_code"] = newAPIError.GetErrorCode()
		}
		c.JSON(stdhttp.StatusOK, resp)
		return
	}
	if newAPIError != nil {
		c.JSON(stdhttp.StatusOK, gin.H{
			"success":    false,
			"message":    newAPIError.Error(),
			"time":       consumedTime,
			"error_code": newAPIError.GetErrorCode(),
		})
		return
	}

	c.JSON(stdhttp.StatusOK, gin.H{
		"success": true,
		"message": "",
		"time":    consumedTime,
	})
}

func TestAllChannels(c *gin.Context) {
	if err := gatewayexecutionapp.TestAllChannels(true); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}
