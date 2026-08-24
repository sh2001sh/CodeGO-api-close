package app

import (
	"errors"
	"fmt"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	"github.com/sh2001sh/new-api/i18n"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/internal/platform/logger"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"net/url"
	"strings"
)

var (
	ErrPaymentComplianceRequired = errors.New(i18n.MsgPaymentComplianceRequired)
	ErrInvalidSettingType        = errors.New(i18n.MsgSettingInvalidType)
	ErrQuotaThresholdGtZero      = errors.New(i18n.MsgQuotaThresholdGtZero)
	ErrWebhookEmpty              = errors.New(i18n.MsgSettingWebhookEmpty)
	ErrWebhookInvalid            = errors.New(i18n.MsgSettingWebhookInvalid)
	ErrEmailInvalid              = errors.New(i18n.MsgSettingEmailInvalid)
	ErrBarkURLEmpty              = errors.New(i18n.MsgSettingBarkUrlEmpty)
	ErrBarkURLInvalid            = errors.New(i18n.MsgSettingBarkUrlInvalid)
	ErrURLMustHTTP               = errors.New(i18n.MsgSettingUrlMustHttp)
	ErrGotifyURLEmpty            = errors.New(i18n.MsgSettingGotifyUrlEmpty)
	ErrGotifyTokenEmpty          = errors.New(i18n.MsgSettingGotifyTokenEmpty)
	ErrGotifyURLInvalid          = errors.New(i18n.MsgSettingGotifyUrlInvalid)
	ErrTransferFailed            = errors.New(i18n.MsgUserTransferFailed)
	ErrUpdateFailed              = errors.New(i18n.MsgUpdateFailed)
)

// TransferAffiliateQuotaRequest captures the authenticated user's affiliate quota transfer request.
type TransferAffiliateQuotaRequest struct {
	Quota int `json:"quota" binding:"required"`
}

// TransferAffiliateQuotaError keeps the public failure sentinel while preserving the model-layer cause.
type TransferAffiliateQuotaError struct {
	Cause error
}

func (e *TransferAffiliateQuotaError) Error() string {
	return ErrTransferFailed.Error()
}

func (e *TransferAffiliateQuotaError) Unwrap() error {
	return ErrTransferFailed
}

// UpdateUserSettingsRequest captures the writable user alerting and preference settings.
type UpdateUserSettingsRequest struct {
	QuotaWarningType                 string  `json:"notify_type"`
	QuotaWarningThreshold            float64 `json:"quota_warning_threshold"`
	WebhookURL                       string  `json:"webhook_url,omitempty"`
	WebhookSecret                    string  `json:"webhook_secret,omitempty"`
	NotificationEmail                string  `json:"notification_email,omitempty"`
	BarkURL                          string  `json:"bark_url,omitempty"`
	GotifyURL                        string  `json:"gotify_url,omitempty"`
	GotifyToken                      string  `json:"gotify_token,omitempty"`
	GotifyPriority                   int     `json:"gotify_priority,omitempty"`
	UpstreamModelUpdateNotifyEnabled *bool   `json:"upstream_model_update_notify_enabled,omitempty"`
	AcceptUnsetModelRatioModel       bool    `json:"accept_unset_model_ratio_model"`
	RecordIPLog                      bool    `json:"record_ip_log"`
}

// TransferAffiliateQuotaToBalance converts affiliate quota into spendable quota for the authenticated user.
func TransferAffiliateQuotaToBalance(userID int, quota int) error {
	if !commerceapp.IsPaymentComplianceConfirmed() {
		return ErrPaymentComplianceRequired
	}

	if float64(quota) < platformruntime.QuotaPerUnit {
		return &TransferAffiliateQuotaError{Cause: fmt.Errorf("转移额度最小为%s", logger.LogQuota(int(platformruntime.QuotaPerUnit)))}
	}
	operationID := platformruntime.GetUUID()
	var updatedUser *identityschema.User
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		user := &identityschema.User{}
		if err := tx.Set("gorm:query_option", "FOR UPDATE").First(user, userID).Error; err != nil {
			return err
		}
		if user.AffQuota < quota {
			return errors.New("邀请额度不足")
		}
		if err := billingapp.CreditClaudeWalletQuotaTx(tx, user.Id, quota, "affiliate-transfer:"+operationID, "affiliate_quota_transfer"); err != nil {
			return err
		}
		if err := tx.Model(user).Update("aff_quota", gorm.Expr("aff_quota - ?", quota)).Error; err != nil {
			return err
		}
		updatedUser = user
		return nil
	}); err != nil {
		return &TransferAffiliateQuotaError{Cause: err}
	}
	if updatedUser != nil {
		_ = platformcache.DeleteUserCache(updatedUser.Id)
	}
	return nil
}

// UpdateUserSettings validates and persists the authenticated user's alerting preferences.
func UpdateUserSettings(userID int, userRole int, req UpdateUserSettingsRequest) error {
	if err := validateUserSettingsRequest(req); err != nil {
		return err
	}

	user, err := LoadUserByID(userID, true)
	if err != nil {
		return err
	}

	existingSettings := identitydomain.GetSetting(user)
	upstreamNotifyEnabled := existingSettings.UpstreamModelUpdateNotifyEnabled
	if userRole >= constant.RoleAdminUser && req.UpstreamModelUpdateNotifyEnabled != nil {
		upstreamNotifyEnabled = *req.UpstreamModelUpdateNotifyEnabled
	}

	settings := existingSettings
	settings.NotifyType = req.QuotaWarningType
	settings.QuotaWarningThreshold = req.QuotaWarningThreshold
	settings.UpstreamModelUpdateNotifyEnabled = upstreamNotifyEnabled
	settings.AcceptUnsetRatioModel = req.AcceptUnsetModelRatioModel
	settings.RecordIpLog = req.RecordIPLog
	settings.WebhookUrl = ""
	settings.WebhookSecret = ""
	settings.NotificationEmail = ""
	settings.BarkUrl = ""
	settings.GotifyUrl = ""
	settings.GotifyToken = ""
	settings.GotifyPriority = 0

	switch req.QuotaWarningType {
	case dto.NotifyTypeWebhook:
		settings.WebhookUrl = req.WebhookURL
		if req.WebhookSecret != "" {
			settings.WebhookSecret = req.WebhookSecret
		}
	case dto.NotifyTypeEmail:
		if req.NotificationEmail != "" {
			settings.NotificationEmail = req.NotificationEmail
		}
	case dto.NotifyTypeBark:
		settings.BarkUrl = req.BarkURL
	case dto.NotifyTypeGotify:
		settings.GotifyUrl = req.GotifyURL
		settings.GotifyToken = req.GotifyToken
		if req.GotifyPriority < 0 || req.GotifyPriority > 10 {
			settings.GotifyPriority = 5
		} else {
			settings.GotifyPriority = req.GotifyPriority
		}
	}

	identitydomain.SetSetting(user, settings)
	if err := identitystore.UpdateUser(user, false); err != nil {
		return ErrUpdateFailed
	}
	return nil
}

func validateUserSettingsRequest(req UpdateUserSettingsRequest) error {
	switch req.QuotaWarningType {
	case dto.NotifyTypeEmail, dto.NotifyTypeWebhook, dto.NotifyTypeBark, dto.NotifyTypeGotify:
	default:
		return ErrInvalidSettingType
	}

	if req.QuotaWarningThreshold <= 0 {
		return ErrQuotaThresholdGtZero
	}

	if req.QuotaWarningType == dto.NotifyTypeWebhook {
		if req.WebhookURL == "" {
			return ErrWebhookEmpty
		}
		if _, err := url.ParseRequestURI(req.WebhookURL); err != nil {
			return ErrWebhookInvalid
		}
	}

	if req.QuotaWarningType == dto.NotifyTypeEmail && req.NotificationEmail != "" {
		if !strings.Contains(req.NotificationEmail, "@") {
			return ErrEmailInvalid
		}
	}

	if req.QuotaWarningType == dto.NotifyTypeBark {
		if req.BarkURL == "" {
			return ErrBarkURLEmpty
		}
		if _, err := url.ParseRequestURI(req.BarkURL); err != nil {
			return ErrBarkURLInvalid
		}
		if !strings.HasPrefix(req.BarkURL, "https://") && !strings.HasPrefix(req.BarkURL, "http://") {
			return ErrURLMustHTTP
		}
	}

	if req.QuotaWarningType == dto.NotifyTypeGotify {
		if req.GotifyURL == "" {
			return ErrGotifyURLEmpty
		}
		if req.GotifyToken == "" {
			return ErrGotifyTokenEmpty
		}
		if _, err := url.ParseRequestURI(req.GotifyURL); err != nil {
			return ErrGotifyURLInvalid
		}
		if !strings.HasPrefix(req.GotifyURL, "https://") && !strings.HasPrefix(req.GotifyURL, "http://") {
			return ErrURLMustHTTP
		}
	}

	return nil
}
