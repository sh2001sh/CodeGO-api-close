package app

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/constant"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

func ListAdminChannels(status string) ([]ChannelView, error) {
	query := platformdb.DB.Model(&marketplaceschema.Channel{})
	if strings.TrimSpace(status) != "" {
		query = query.Where("status = ?", status)
	}
	var channels []marketplaceschema.Channel
	if err := query.Order("updated_at desc").Find(&channels).Error; err != nil {
		return nil, err
	}
	groups, err := groupsByChannelIDs(channelIDs(channels))
	if err != nil {
		return nil, err
	}
	result := make([]ChannelView, 0, len(channels))
	for index := range channels {
		if group := groups[channels[index].ID]; group != nil {
			result = append(result, *channelView(&channels[index], group))
		}
	}
	return result, nil
}

func ReviewChannel(channelID string, req AdminReviewRequest) (*ChannelView, error) {
	if strings.TrimSpace(req.Reason) == "" {
		return nil, errors.New("管理员审核必须填写原因")
	}
	channel, group, err := loadChannelGroup(channelID)
	if err != nil {
		return nil, err
	}
	if !req.Approved {
		return rejectChannel(channel, group, req.Reason)
	}
	if group.VerificationStatus != marketplacedomain.VerificationPassed {
		return nil, errors.New("渠道检测未通过，不能发布")
	}
	if channel.InternalChannelID == nil {
		if err := createInternalChannel(channel, group); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	err = platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(channel).Updates(map[string]any{
			"status": marketplacedomain.LifecycleActive, "last_review_reason": req.Reason,
			"approved_source_label":      channel.SubmittedSourceLabel,
			"source_label_status":        marketplacedomain.SourceLabelApproved,
			"source_label_review_reason": req.Reason,
		}).Error; err != nil {
			return err
		}
		return tx.Model(group).Updates(map[string]any{"lifecycle_status": marketplacedomain.LifecycleActive, "published_at": now}).Error
	})
	if err == nil {
		channel.Status = marketplacedomain.LifecycleActive
		channel.LastReviewReason = req.Reason
		channel.ApprovedSourceLabel = channel.SubmittedSourceLabel
		channel.SourceLabelStatus = marketplacedomain.SourceLabelApproved
		channel.SourceLabelReviewReason = req.Reason
		group.LifecycleStatus = marketplacedomain.LifecycleActive
		group.PublishedAt = &now
	}
	return channelView(channel, group), err
}

func rejectChannel(channel *marketplaceschema.Channel, group *marketplaceschema.Group, reason string) (*ChannelView, error) {
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(channel).Updates(map[string]any{
			"status": marketplacedomain.LifecycleDraft, "last_review_reason": reason,
			"approved_source_label": "", "source_label_status": marketplacedomain.SourceLabelRejected,
			"source_label_review_reason": reason,
		}).Error; err != nil {
			return err
		}
		return tx.Model(group).Update("lifecycle_status", marketplacedomain.LifecycleDraft).Error
	})
	if err == nil {
		channel.Status = marketplacedomain.LifecycleDraft
		channel.LastReviewReason = reason
		channel.ApprovedSourceLabel = ""
		channel.SourceLabelStatus = marketplacedomain.SourceLabelRejected
		channel.SourceLabelReviewReason = reason
		group.LifecycleStatus = marketplacedomain.LifecycleDraft
	}
	return channelView(channel, group), err
}

func createInternalChannel(channel *marketplaceschema.Channel, group *marketplaceschema.Group) error {
	metadata, _ := json.Marshal(map[string]any{
		"marketplace_channel_id": channel.ID, "marketplace_group_id": group.ID,
		"source_type": group.SourceType, "credit_pool_policy": group.CreditPoolPolicy,
	})
	baseURL := channel.BaseURLCiphertext
	internal := &gatewayschema.Channel{
		Type: providerChannelType(channel.ProviderType), Key: channel.CredentialCiphertext,
		Status: constant.ChannelStatusEnabled, Name: group.SystemDisplayName,
		CreatedTime: platformruntime.GetTimestamp(), BaseURL: &baseURL,
		Models: strings.Join(decodeModels(channel.DeclaredModels), ","), Group: group.InternalGroupName,
		OtherInfo: string(metadata),
	}
	if err := gatewaystore.CreateChannel(internal); err != nil {
		return err
	}
	if err := platformdb.DB.Model(channel).Update("internal_channel_id", internal.Id).Error; err != nil {
		_ = gatewaystore.DeleteChannelByID(internal.Id)
		return err
	}
	channel.InternalChannelID = &internal.Id
	return nil
}

func loadChannelGroup(channelID string) (*marketplaceschema.Channel, *marketplaceschema.Group, error) {
	var channel marketplaceschema.Channel
	if err := platformdb.DB.First(&channel, "id = ?", channelID).Error; err != nil {
		return nil, nil, err
	}
	var group marketplaceschema.Group
	if err := platformdb.DB.First(&group, "channel_id = ?", channelID).Error; err != nil {
		return nil, nil, err
	}
	return &channel, &group, nil
}

func providerChannelType(provider string) int {
	switch provider {
	case "codex":
		// Marketplace Codex entries authenticate to the public Responses API
		// with a bearer API key. The internal Codex channel type is reserved for
		// OAuth JSON credentials and would reject the submitted marketplace key.
		return constant.ChannelTypeOpenAI
	case "azure_openai":
		return constant.ChannelTypeAzure
	case "anthropic":
		return constant.ChannelTypeAnthropic
	case "gemini":
		return constant.ChannelTypeGemini
	default:
		return constant.ChannelTypeOpenAI
	}
}

var _ = gorm.ErrRecordNotFound
