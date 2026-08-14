package http

import (
	"errors"
	stdhttp "net/http"

	"github.com/gin-gonic/gin"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	"github.com/sh2001sh/new-api/internal/identity/sessionstate"
	platformpagination "github.com/sh2001sh/new-api/internal/platform/pagination"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
)

type configureWalletTransferPasswordRequest struct {
	CurrentPassword    string `json:"current_password"`
	OldPaymentPassword string `json:"old_payment_password"`
	NewPaymentPassword string `json:"new_payment_password"`
	ConfirmPassword    string `json:"confirm_password"`
}

func getWalletTransferOverview(c *gin.Context) {
	user, err := identityapp.LoadActiveUser(c.GetInt("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	page := platformpagination.GetPageQuery(c)
	overview, err := commerceapp.BuildWalletTransferOverview(user, page.GetPage(), page.GetPageSize())
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, overview)
}

func getWalletTransferRecipient(c *gin.Context) {
	recipient, err := commerceapp.LookupWalletTransferRecipient(c.GetInt("id"), c.Param("external_id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, recipient)
}

func configureWalletTransferPassword(c *gin.Context) {
	var req configureWalletTransferPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, commerceschema.ErrWalletTransferInvalid)
		return
	}
	if req.NewPaymentPassword != req.ConfirmPassword {
		httpapi.ApiError(c, commerceschema.ErrWalletTransferPasswordConfirmation)
		return
	}
	user, err := identityapp.LoadActiveUser(c.GetInt("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	security, err := commerceapp.GetWalletTransferSecurityOverview(user.Id, user.Password != "")
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	if !security.PasswordSet && !authorizeFirstWalletTransferPassword(c, user.Password, req.CurrentPassword) {
		return
	}
	if err := commerceapp.ConfigureWalletTransferPassword(user.Id, req.OldPaymentPassword, req.NewPaymentPassword); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, gin.H{"password_set": true})
}

func createWalletTransfer(c *gin.Context) {
	var req commerceapp.CreateWalletTransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, commerceschema.ErrWalletTransferInvalid)
		return
	}
	transfer, err := commerceapp.TransferWalletQuota(c.GetInt("id"), req)
	if err != nil {
		if errors.Is(err, commerceschema.ErrWalletTransferPasswordLocked) {
			c.JSON(stdhttp.StatusTooManyRequests, gin.H{"success": false, "message": err.Error(), "code": "PAYMENT_PASSWORD_LOCKED"})
			return
		}
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, transfer)
}

func authorizeFirstWalletTransferPassword(c *gin.Context, accountPasswordHash, currentPassword string) bool {
	if accountPasswordHash != "" {
		if platformsecurity.ValidatePasswordAndHash(currentPassword, accountPasswordHash) {
			return true
		}
		httpapi.ApiError(c, commerceschema.ErrWalletTransferAccountPassword)
		return false
	}
	if err := sessionstate.RequireSecureVerification(c); err != nil {
		c.JSON(stdhttp.StatusForbidden, gin.H{
			"success": false,
			"message": "请先完成 2FA 或 Passkey 安全验证",
			"code":    "VERIFICATION_REQUIRED",
		})
		return false
	}
	return true
}
