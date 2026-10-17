package http

import (
	"context"
	"errors"
	stdhttp "net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformpagination "github.com/sh2001sh/new-api/internal/platform/pagination"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
)

type configureWalletTransferPasswordRequest struct {
	VerificationMethod string `json:"verification_method"`
	CurrentPassword    string `json:"current_password"`
	OldPaymentPassword string `json:"old_payment_password"`
	NewPaymentPassword string `json:"new_payment_password"`
	ConfirmPassword    string `json:"confirm_password"`
	EmailCode          string `json:"email_code"`
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
	recipient, err := commerceapp.LookupWalletTransferRecipient(c.Request.Context(), c.GetInt("id"), c.Param("external_id"))
	if err != nil {
		if !errors.Is(err, commerceschema.ErrWalletTransferInvalid) && !errors.Is(err, commerceschema.ErrWalletTransferRecipientNotFound) && !errors.Is(err, commerceschema.ErrWalletTransferSelf) {
			c.JSON(stdhttp.StatusServiceUnavailable, gin.H{"success": false, "message": "收款账户查询暂不可用，请稍后重试", "code": "RECIPIENT_LOOKUP_UNAVAILABLE"})
			return
		}
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
	security, err := commerceapp.GetWalletTransferSecurityOverview(user.Id, user.Password != "", user.Email)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	verifyEmail := func() bool {
		owner, ownerErr := identitystore.UniqueUserByEmail(user.Email)
		return ownerErr == nil && owner.Id == user.Id && identityapp.VerifyCodeWithKey(user.Email, req.EmailCode, identityapp.WalletTransferPasswordPurpose)
	}
	if security.PasswordSet {
		switch strings.TrimSpace(req.VerificationMethod) {
		case "payment_password":
			err = commerceapp.ConfigureWalletTransferPassword(user.Id, req.OldPaymentPassword, req.NewPaymentPassword)
		case "email":
			if !verifyEmail() {
				httpapi.ApiError(c, commerceschema.ErrWalletTransferEmailCodeInvalid)
				return
			}
			err = commerceapp.ResetWalletTransferPassword(user.Id, req.NewPaymentPassword)
		default:
			httpapi.ApiError(c, commerceschema.ErrWalletTransferInvalid)
			return
		}
	} else {
		method := strings.TrimSpace(req.VerificationMethod)
		if method == "email" {
			if !verifyEmail() {
				httpapi.ApiError(c, commerceschema.ErrWalletTransferEmailCodeInvalid)
				return
			}
		} else if (method != "" && method != "account_password") || !authorizeFirstWalletTransferPassword(user.Password, req.CurrentPassword) {
			httpapi.ApiError(c, commerceschema.ErrWalletTransferAccountPassword)
			return
		}
		err = commerceapp.ConfigureWalletTransferPassword(user.Id, "", req.NewPaymentPassword)
	}
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, gin.H{"password_set": true})
}

func sendWalletTransferPasswordEmailCode(c *gin.Context) {
	email, err := identityapp.SendWalletTransferPasswordVerification(c.GetInt("id"))
	if err != nil {
		if errors.Is(err, identityapp.ErrBoundEmailRequired) {
			httpapi.ApiError(c, commerceschema.ErrWalletTransferEmailRequired)
			return
		}
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, gin.H{"email_masked": maskEmailForResponse(email)})
}

func maskEmailForResponse(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" {
		return ""
	}
	return string([]rune(parts[0])[0]) + "***@" + parts[1]
}

func createWalletTransfer(c *gin.Context) {
	var req commerceapp.CreateWalletTransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, commerceschema.ErrWalletTransferInvalid)
		return
	}
	req.RequestId = strings.TrimSpace(req.RequestId)
	req.RecipientExternalId = strings.ToUpper(strings.TrimSpace(req.RecipientExternalId))
	if c.GetInt("id") <= 0 || req.RequestId == "" || len(req.RequestId) > 128 || req.PaymentPassword == "" {
		httpapi.ApiError(c, commerceschema.ErrWalletTransferInvalid)
		return
	}
	operation, start := walletTransferRequests.admit(c.GetInt("id"), req)
	if operation == nil {
		c.JSON(stdhttp.StatusTooManyRequests, gin.H{"success": false, "code": "TRANSFER_BUSY", "message": "已有转账处理中，请查询原请求"})
		return
	}
	if start {
		// Admission uses the live request. Detached execution receives only the
		// validated snapshot, never the request, Gin context or payment password.
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		authorized, err := commerceapp.AuthorizeWalletTransfer(ctx, c.GetInt("id"), req)
		cancel()
		req.PaymentPassword = ""
		if err != nil {
			walletTransferRequests.finish(operation, nil, err)
		} else {
			executor := walletTransferRequests
			go func() {
				var transfer *commerceapp.WalletTransferHistoryItem
				err := commerceschema.ErrWalletTransferUnavailable
				defer func() {
					if recover() != nil {
						platformobservability.SysError("wallet transfer execution panicked; query the original native receipt")
					}
					executor.finish(operation, transfer, err)
				}()
				ctx, cancel := context.WithTimeout(context.Background(), commerceapp.WalletTransferTimeout)
				defer cancel()
				transfer, err = commerceapp.TransferAuthorizedWalletQuota(ctx, authorized)
			}()
		}
	}
	select {
	case <-operation.done:
		writeWalletTransferOutcome(c, operation)
		return
	default:
	}
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-operation.done:
		writeWalletTransferOutcome(c, operation)
	case <-timer.C:
		writeWalletTransferPending(c, operation.requestID, true)
	case <-c.Request.Context().Done():
		// An admitted operation keeps its independent bounded execution context.
		writeWalletTransferPending(c, operation.requestID, true)
	}
}

func authorizeFirstWalletTransferPassword(accountPasswordHash, currentPassword string) bool {
	return accountPasswordHash != "" && currentPassword != "" && platformsecurity.ValidatePasswordAndHash(currentPassword, accountPasswordHash)
}
