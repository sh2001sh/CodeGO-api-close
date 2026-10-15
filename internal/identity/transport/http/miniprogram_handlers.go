package http

import (
	"github.com/gin-gonic/gin"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
)

type miniProgramSessionRequest struct {
	Code string `json:"code"`
}

type miniProgramBindRequest struct {
	BindCode string `json:"bind_code"`
}

type miniProgramShareCheckRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

func CreateMiniProgramBindCode(c *gin.Context) {
	payload, err := identityapp.BuildMiniProgramBindCodePayload(c.GetInt("id"), c.ClientIP())
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func GetMiniProgramBinding(c *gin.Context) {
	payload, err := identityapp.BuildMiniProgramBindingPayload(c.GetInt("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func DeleteMiniProgramBinding(c *gin.Context) {
	if err := identityapp.DeleteMiniProgramBinding(c.GetInt("id")); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, map[string]any{
		"bound": false,
	})
}

func MiniProgramSession(c *gin.Context) {
	var req miniProgramSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	payload, err := identityapp.BuildMiniProgramSessionResponse(req.Code)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func GetMiniProgramMe(c *gin.Context) {
	payload, err := identityapp.BuildMiniProgramMeResponse(c.GetString("mini_openid"), c.GetInt64("mini_token_expires_at"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func BindMiniProgram(c *gin.Context) {
	var req miniProgramBindRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	payload, err := identityapp.BindMiniProgramSession(req.BindCode, c.GetString("mini_openid"), c.GetString("mini_unionid"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func UnbindMiniProgram(c *gin.Context) {
	if err := identityapp.UnbindMiniProgramSession(c.GetInt("id"), c.GetString("mini_openid")); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, map[string]any{
		"bound": false,
	})
}

func CheckMiniProgramShareContent(c *gin.Context) {
	var req miniProgramShareCheckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiErrorMsg(c, "invalid request")
		return
	}
	payload, err := identityapp.BuildMiniProgramShareCheck(req.Title, req.Content)
	if err != nil {
		if err.Error() == "content is required" {
			httpapi.ApiErrorMsg(c, err.Error())
			return
		}
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}
