package http

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/i18n"
	gatewayroutingapp "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformpagination "github.com/sh2001sh/new-api/internal/platform/pagination"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
	stdhttp "net/http"
	"strconv"
	"strings"
)

type TokenBatch struct {
	Ids []int `json:"ids"`
}

func GetAllTokens(c *gin.Context) {
	userID := c.GetInt("id")
	pageInfo := platformpagination.GetPageQuery(c)
	tokens, total, err := identityapp.ListUserTokens(userID, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(identityapp.BuildMaskedTokenResponses(tokens))
	httpapi.ApiSuccess(c, pageInfo)
}

func SearchTokens(c *gin.Context) {
	userID := c.GetInt("id")
	keyword := c.Query("keyword")
	token := c.Query("token")

	pageInfo := platformpagination.GetPageQuery(c)

	tokens, total, err := identityapp.SearchUserTokens(userID, keyword, token, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(identityapp.BuildMaskedTokenResponses(tokens))
	httpapi.ApiSuccess(c, pageInfo)
}

func GetToken(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	userID := c.GetInt("id")
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	token, err := identityapp.GetUserToken(userID, id)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, identityapp.BuildMaskedTokenResponse(token))
}

func GetTokenKey(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	userID := c.GetInt("id")
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	token, err := identityapp.GetUserToken(userID, id)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, gin.H{
		"key": token.GetFullKey(),
	})
}

func GetTokenStatus(c *gin.Context) {
	tokenID := c.GetInt("token_id")
	userID := c.GetInt("id")
	token, err := identityapp.GetUserToken(userID, tokenID)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	expiredAt := token.ExpiredTime
	if expiredAt == -1 {
		expiredAt = 0
	}
	c.JSON(stdhttp.StatusOK, gin.H{
		"object":          "credit_summary",
		"total_granted":   token.RemainQuota,
		"total_used":      0,
		"total_available": token.RemainQuota,
		"expires_at":      expiredAt * 1000,
	})
}

func GetTokenUsage(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(stdhttp.StatusUnauthorized, gin.H{
			"success": false,
			"message": "No Authorization header",
		})
		return
	}

	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		c.JSON(stdhttp.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Invalid Bearer token",
		})
		return
	}
	tokenKey := parts[1]

	token, err := identityapp.GetTokenByBearerKey(tokenKey)
	if err != nil {
		platformobservability.SysError("failed to get token by key: " + err.Error())
		httpapi.ApiErrorI18n(c, i18n.MsgTokenGetInfoFailed)
		return
	}

	expiredAt := token.ExpiredTime
	if expiredAt == -1 {
		expiredAt = 0
	}

	c.JSON(stdhttp.StatusOK, gin.H{
		"code":    true,
		"message": "ok",
		"data": gin.H{
			"object":               "token_usage",
			"name":                 token.Name,
			"total_granted":        token.RemainQuota + token.UsedQuota,
			"total_used":           token.UsedQuota,
			"total_available":      token.RemainQuota,
			"unlimited_quota":      token.UnlimitedQuota,
			"model_limits":         token.GetModelLimitsMap(),
			"model_limits_enabled": token.ModelLimitsEnabled,
			"expires_at":           expiredAt,
		},
	})
}

// GetTokenAccountBalance returns wallet and subscription balance for one valid API key.
func GetTokenAccountBalance(c *gin.Context) {
	payload, err := identityapp.BuildTokenBalanceSummary(c.GetInt("id"), c.GetInt("token_id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, payload)
}

func AddToken(c *gin.Context) {
	token := identityschema.Token{}
	err := c.ShouldBindJSON(&token)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	if len(token.Name) > 50 {
		httpapi.ApiErrorI18n(c, i18n.MsgTokenNameTooLong)
		return
	}
	if !token.UnlimitedQuota {
		if token.RemainQuota < 0 {
			httpapi.ApiErrorI18n(c, i18n.MsgTokenQuotaNegative)
			return
		}
		maxQuotaValue := int((1000000000 * platformruntime.QuotaPerUnit))
		if token.RemainQuota > maxQuotaValue {
			httpapi.ApiErrorI18n(c, i18n.MsgTokenQuotaExceedMax, map[string]any{"Max": maxQuotaValue})
			return
		}
	}
	if err := marketplaceapp.ValidateMultiplierLimitValue(token.MarketplaceMultiplierLimit); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	maxTokens := identitystore.GetMaxUserTokens()
	count, err := identityapp.CountTokensForUser(c.GetInt("id"))
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	if int(count) >= maxTokens {
		c.JSON(stdhttp.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("已达到最大令牌数量限制 (%d)", maxTokens),
		})
		return
	}
	token.Group, token.CrossGroupRetry, err = normalizeTokenGroupSelection(
		c.GetInt("id"), token.Group, token.CrossGroupRetry,
	)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	key, err := platformruntime.GenerateKey()
	if err != nil {
		httpapi.ApiErrorI18n(c, i18n.MsgTokenGenerateFailed)
		platformobservability.SysLog("failed to generate token key: " + err.Error())
		return
	}
	cleanToken := identityschema.Token{
		UserId:                     c.GetInt("id"),
		Name:                       token.Name,
		Key:                        key,
		CreatedTime:                platformruntime.GetTimestamp(),
		AccessedTime:               0,
		ExpiredTime:                token.ExpiredTime,
		RemainQuota:                token.RemainQuota,
		UnlimitedQuota:             token.UnlimitedQuota,
		ModelLimitsEnabled:         token.ModelLimitsEnabled,
		ModelLimits:                token.ModelLimits,
		AllowIps:                   token.AllowIps,
		Group:                      gatewayroutingapp.NormalizeTokenGroup(token.Group),
		CrossGroupRetry:            token.CrossGroupRetry,
		MarketplaceMultiplierLimit: token.MarketplaceMultiplierLimit,
	}
	err = identityapp.InsertUserToken(&cleanToken)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func DeleteToken(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	userID := c.GetInt("id")
	err := identityapp.DeleteUserToken(userID, id)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func UpdateToken(c *gin.Context) {
	userID := c.GetInt("id")
	statusOnly := c.Query("status_only")
	token := identityschema.Token{}
	err := c.ShouldBindJSON(&token)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	if len(token.Name) > 50 {
		httpapi.ApiErrorI18n(c, i18n.MsgTokenNameTooLong)
		return
	}
	if !token.UnlimitedQuota {
		if token.RemainQuota < 0 {
			httpapi.ApiErrorI18n(c, i18n.MsgTokenQuotaNegative)
			return
		}
		maxQuotaValue := int((1000000000 * platformruntime.QuotaPerUnit))
		if token.RemainQuota > maxQuotaValue {
			httpapi.ApiErrorI18n(c, i18n.MsgTokenQuotaExceedMax, map[string]any{"Max": maxQuotaValue})
			return
		}
	}
	if err := marketplaceapp.ValidateMultiplierLimitValue(token.MarketplaceMultiplierLimit); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	cleanToken, err := identityapp.GetUserToken(userID, token.Id)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	if token.Status == constant.TokenStatusEnabled {
		if cleanToken.Status == constant.TokenStatusExpired && cleanToken.ExpiredTime <= platformruntime.GetTimestamp() && cleanToken.ExpiredTime != -1 {
			httpapi.ApiErrorI18n(c, i18n.MsgTokenExpiredCannotEnable)
			return
		}
		if cleanToken.Status == constant.TokenStatusExhausted && cleanToken.RemainQuota <= 0 && !cleanToken.UnlimitedQuota {
			httpapi.ApiErrorI18n(c, i18n.MsgTokenExhaustedCannotEable)
			return
		}
	}
	if statusOnly != "" {
		cleanToken.Status = token.Status
	} else {
		token.Group, token.CrossGroupRetry, err = normalizeTokenGroupSelection(
			userID, token.Group, token.CrossGroupRetry,
		)
		if err != nil {
			httpapi.ApiError(c, err)
			return
		}
		cleanToken.Name = token.Name
		cleanToken.ExpiredTime = token.ExpiredTime
		cleanToken.RemainQuota = token.RemainQuota
		cleanToken.UnlimitedQuota = token.UnlimitedQuota
		cleanToken.ModelLimitsEnabled = token.ModelLimitsEnabled
		cleanToken.ModelLimits = token.ModelLimits
		cleanToken.AllowIps = token.AllowIps
		cleanToken.Group = gatewayroutingapp.NormalizeTokenGroup(token.Group)
		cleanToken.CrossGroupRetry = token.CrossGroupRetry
		cleanToken.MarketplaceMultiplierLimit = token.MarketplaceMultiplierLimit
	}
	err = identityapp.UpdateUserToken(cleanToken)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    identityapp.BuildMaskedTokenResponse(cleanToken),
	})
}

func normalizeTokenGroupSelection(userID int, group string, crossGroupRetry bool) (string, bool, error) {
	group = gatewayroutingapp.NormalizeTokenGroup(group)
	if group == gatewayroutingapp.AutoGroupName {
		// Auto is inherently a cross-group route pool. Keep the legacy field
		// enabled for compatibility, but do not expose it as a user choice.
		return group, true, nil
	}
	if marketplaceapp.IsMarketplaceAutoTokenGroup(group) {
		return group, false, nil
	}
	if !marketplaceapp.IsMarketplaceTokenGroup(group) {
		return group, crossGroupRetry, nil
	}
	if _, err := marketplaceapp.ResolveTokenGroupBinding(group, userID); err != nil {
		return "", false, err
	}
	return group, false, nil
}

func DeleteTokenBatch(c *gin.Context) {
	tokenBatch := TokenBatch{}
	if err := c.ShouldBindJSON(&tokenBatch); err != nil || len(tokenBatch.Ids) == 0 {
		httpapi.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	userID := c.GetInt("id")
	count, err := identityapp.BatchDeleteUserTokens(userID, tokenBatch.Ids)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    count,
	})
}

func GetTokenKeysBatch(c *gin.Context) {
	tokenBatch := TokenBatch{}
	if err := c.ShouldBindJSON(&tokenBatch); err != nil || len(tokenBatch.Ids) == 0 {
		httpapi.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if len(tokenBatch.Ids) > 100 {
		httpapi.ApiErrorI18n(c, i18n.MsgBatchTooMany, map[string]any{"Max": 100})
		return
	}
	userID := c.GetInt("id")
	keysMap, err := identityapp.GetUserTokenKeys(userID, tokenBatch.Ids)
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	httpapi.ApiSuccess(c, gin.H{"keys": keysMap})
}
