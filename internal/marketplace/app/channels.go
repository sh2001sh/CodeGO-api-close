package app

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	"gorm.io/gorm"
)

var queueMarketplaceVerification = QueueRequiredVerification

func CreateMarketplaceChannel(ownerUserID int, req CreateChannelRequest) (*ChannelView, error) {
	if ownerUserID <= 0 {
		return nil, errors.New("用户未登录")
	}
	if req.Visibility == "" {
		req.Visibility = marketplacedomain.VisibilityPrivate
	}
	if err := validateCreateRequest(req); err != nil {
		return nil, err
	}
	var channel *marketplaceschema.Channel
	var group *marketplaceschema.Group
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		channel, group, err = buildMarketplaceRecords(tx, ownerUserID, req)
		if err != nil {
			return err
		}
		if err := tx.Create(channel).Error; err != nil {
			return err
		}
		return tx.Create(group).Error
	}); err != nil {
		return nil, err
	}
	queueMarketplaceCapabilityProbe(channel.ID)
	if err := queueMarketplaceVerification(channel.ID); err != nil {
		return nil, err
	}
	return channelView(channel, group), nil
}

func buildMarketplaceRecords(tx *gorm.DB, ownerUserID int, req CreateChannelRequest) (*marketplaceschema.Channel, *marketplaceschema.Group, error) {
	channelID, err := newMarketplaceChannelID(tx)
	if err != nil {
		return nil, nil, err
	}
	baseURL, err := platformsecurity.EncryptSecret(strings.TrimRight(strings.TrimSpace(req.BaseURL), "/"))
	if err != nil {
		return nil, nil, err
	}
	credential, err := platformsecurity.EncryptSecret(strings.TrimSpace(req.APIKey))
	if err != nil {
		return nil, nil, err
	}
	models, _ := json.Marshal(normalizeModels(req.DeclaredModels))
	modelPrices, err := encodeChannelModelPrices(req.ModelPrices, req.DeclaredModels)
	if err != nil {
		return nil, nil, err
	}
	ownerName := loadOwnerDisplayName(ownerUserID)
	sourceLabel, _ := canonicalSourceLabel(req.SourceLabel)
	sensitiveWordInterceptionEnabled := true
	if req.SensitiveWordInterceptionEnabled != nil {
		sensitiveWordInterceptionEnabled = *req.SensitiveWordInterceptionEnabled
	}
	channel := &marketplaceschema.Channel{
		ID: channelID, OwnerUserID: ownerUserID, ProviderType: req.ProviderType,
		SubmittedSourceLabel: sourceLabel, ApprovedSourceLabel: sourceLabel,
		SourceLabelStatus: marketplacedomain.SourceLabelApproved,
		BaseURLCiphertext: baseURL, CredentialCiphertext: credential,
		CredentialTail: credentialTail(req.APIKey), CredentialVersion: 1,
		DeclaredModels: string(models),
		ModelPrices:    modelPrices,
		MaxConcurrency: req.MaxConcurrency, UserMaxConcurrency: req.UserMaxConcurrency, QPS: req.QPS,
		MaintenanceWindow: strings.TrimSpace(req.MaintenanceWindow), Status: marketplacedomain.LifecycleDraft,
		SensitiveWordInterceptionEnabled: &sensitiveWordInterceptionEnabled,
		AutoProbeEnabled:                 req.AutoProbeEnabled, AutoProbeIntervalMinutes: req.AutoProbeIntervalMinutes,
		AutoProbeModel: strings.TrimSpace(req.AutoProbeModel),
	}
	if channel.AutoProbeIntervalMinutes == 0 {
		channel.AutoProbeIntervalMinutes = 10
	}
	markMarketplaceCapabilitiesPending(channel)
	group := newMarketplaceGroup(channelID, ownerUserID, ownerName, sourceLabel, req.Multiplier, req.Visibility)
	return channel, group, nil
}

func newMarketplaceChannelID(tx *gorm.DB) (string, error) {
	sequence := marketplaceschema.ChannelIDSequence{}
	if err := tx.Create(&sequence).Error; err != nil {
		return "", err
	}
	return strconv.FormatUint(sequence.ID, 10), nil
}

func newMarketplaceGroup(channelID string, ownerUserID int, ownerName, sourceLabel string, multiplier float64, visibility string) *marketplaceschema.Group {
	multiplier = marketplacedomain.NormalizeMultiplier(multiplier)
	groupID := platformruntime.GetUUID()
	compact := strings.ReplaceAll(groupID, "-", "")
	return &marketplaceschema.Group{
		ID: groupID, ChannelID: channelID, OwnerUserID: ownerUserID,
		PublicSlug:        "mg_" + compact[:12],
		SystemDisplayName: marketplaceDisplayName(sourceLabel, multiplier, channelID),
		InternalGroupName: marketplaceInternalGroupName(sourceLabel, groupID),
		OwnerDisplayName:  ownerName, SourceType: marketplacedomain.SourceTypeMarketplaceUser,
		CreditPoolPolicy: marketplacedomain.CreditPolicySubscriptionAndUniversal, Multiplier: multiplier,
		RoutingVersion: 1, LifecycleStatus: marketplacedomain.LifecycleVerifying,
		VerificationStatus: marketplacedomain.VerificationQueued, Visibility: visibility,
	}
}

func ListOwnerChannels(ownerUserID int) ([]ChannelView, error) {
	var channels []marketplaceschema.Channel
	if err := platformdb.DB.Where("owner_user_id = ?", ownerUserID).Order("created_at desc").Find(&channels).Error; err != nil {
		return nil, err
	}
	groups, err := groupsByChannelIDs(channelIDs(channels))
	if err != nil {
		return nil, err
	}
	earnings, err := earningsByGroupIDs(groupIDs(groups))
	if err != nil {
		return nil, err
	}
	result := make([]ChannelView, 0, len(channels))
	for index := range channels {
		group := groups[channels[index].ID]
		if group != nil {
			view := channelView(&channels[index], group)
			view.RequestCount = earnings[group.ID].RequestCount
			view.TotalIncome = earnings[group.ID].TotalIncome
			view.PendingIncome = earnings[group.ID].PendingIncome
			view.ReleasedIncome = earnings[group.ID].ReleasedIncome
			result = append(result, *view)
		}
	}
	return result, nil
}

func UpdateOwnerChannel(ownerUserID int, channelID string, req UpdateChannelRequest) (*ChannelView, error) {
	channel, group, err := loadOwnedChannelGroup(ownerUserID, channelID)
	if err != nil {
		return nil, err
	}
	return updateMarketplaceChannel(channel, group, req, nil)
}

func UpdateAdminChannel(channelID string, req AdminUpdateChannelRequest) (*ChannelView, error) {
	channel, group, err := loadChannelGroup(channelID)
	if err != nil {
		return nil, err
	}
	return updateMarketplaceChannel(channel, group, req.UpdateChannelRequest, req.ModelConsistencyStatus)
}

func updateMarketplaceChannel(channel *marketplaceschema.Channel, group *marketplaceschema.Group, req UpdateChannelRequest, consistencyStatus *string) (*ChannelView, error) {
	transportFingerprint := marketplaceTransportFingerprint(channel)
	reverify, err := applyChannelUpdate(channel, group, req)
	if err != nil {
		return nil, err
	}
	if consistencyStatus != nil {
		if err := applyModelConsistencyStatus(channel, *consistencyStatus); err != nil {
			return nil, err
		}
	}
	transportChanged := transportFingerprint != marketplaceTransportFingerprint(channel)
	if transportChanged {
		markMarketplaceCapabilitiesPending(channel)
	}
	if reverify {
		marketplaceVerificationTasks.cancelChannel(channel.ID)
	}
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if reverify {
			if err := pauseActiveVerificationRunsWithDB(tx, channel.ID, time.Now().UTC()); err != nil {
				return err
			}
		}
		if err := tx.Save(channel).Error; err != nil {
			return err
		}
		return tx.Save(group).Error
	}); err != nil {
		return nil, err
	}
	if channel.InternalChannelID != nil {
		if err := syncInternalChannel(channel, group); err != nil {
			return nil, err
		}
	}
	if transportChanged {
		queueMarketplaceCapabilityProbe(channel.ID)
	}
	return channelView(channel, group), nil
}

func PauseOwnerChannel(ownerUserID int, channelID string, paused bool) error {
	channel, group, err := loadOwnedChannelGroup(ownerUserID, channelID)
	if err != nil {
		return err
	}
	status := marketplacedomain.LifecycleActive
	if paused {
		status = marketplacedomain.LifecycleSuspended
	} else if group.VerificationStatus != marketplacedomain.VerificationPassed {
		return errors.New("检测未通过，不能恢复服务")
	}
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(channel).Update("status", status).Error; err != nil {
			return err
		}
		return tx.Model(group).Update("lifecycle_status", status).Error
	})
}

func loadOwnedChannelGroup(ownerUserID int, channelID string) (*marketplaceschema.Channel, *marketplaceschema.Group, error) {
	var channel marketplaceschema.Channel
	if err := platformdb.DB.Where("id = ? AND owner_user_id = ?", channelID, ownerUserID).First(&channel).Error; err != nil {
		return nil, nil, err
	}
	var group marketplaceschema.Group
	if err := platformdb.DB.Where("channel_id = ?", channel.ID).First(&group).Error; err != nil {
		return nil, nil, err
	}
	return &channel, &group, nil
}

func groupsByChannelIDs(ids []string) (map[string]*marketplaceschema.Group, error) {
	result := make(map[string]*marketplaceschema.Group)
	if len(ids) == 0 {
		return result, nil
	}
	var groups []marketplaceschema.Group
	if err := platformdb.DB.Where("channel_id IN ?", ids).Find(&groups).Error; err != nil {
		return nil, err
	}
	for index := range groups {
		result[groups[index].ChannelID] = &groups[index]
	}
	return result, nil
}

func channelIDs(channels []marketplaceschema.Channel) []string {
	ids := make([]string, 0, len(channels))
	for _, channel := range channels {
		ids = append(ids, channel.ID)
	}
	return ids
}

func groupIDs(groups map[string]*marketplaceschema.Group) []string {
	ids := make([]string, 0, len(groups))
	for _, group := range groups {
		if group != nil {
			ids = append(ids, group.ID)
		}
	}
	return ids
}

func normalizeModels(models []string) []string {
	seen := make(map[string]struct{}, len(models))
	result := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		result = append(result, model)
	}
	return result
}

func decodeModels(raw string) []string {
	var models []string
	if json.Unmarshal([]byte(raw), &models) != nil {
		return []string{}
	}
	return models
}

func credentialTail(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 4 {
		return key
	}
	return key[len(key)-4:]
}
