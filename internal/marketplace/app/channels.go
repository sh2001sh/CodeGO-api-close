package app

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/constant"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	invalidateAdminMarketplaceStatsCache()
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
		SubmittedSourceLabel: sourceLabel,
		SourceLabelStatus:    marketplacedomain.SourceLabelPending,
		BaseURLCiphertext:    baseURL, CredentialCiphertext: credential,
		CredentialTail: credentialTail(req.APIKey), CredentialVersion: 1,
		DeclaredModels: string(models),
		ModelPrices:    modelPrices,
		MaxConcurrency: req.MaxConcurrency, UserMaxConcurrency: req.UserMaxConcurrency, QPS: req.QPS,
		MaintenanceWindow: strings.TrimSpace(req.MaintenanceWindow), Status: marketplacedomain.LifecycleDraft,
		SensitiveWordInterceptionEnabled: &sensitiveWordInterceptionEnabled,
		MultiplierCardSupported:          req.MultiplierCardSupported,
		CreditPackSupported:              req.CreditPackSupported,
		MultiplierCardUserEnabled:        req.MultiplierCardUserEnabled,
		AutoProbeEnabled:                 req.AutoProbeEnabled, AutoProbeIntervalMinutes: req.AutoProbeIntervalMinutes,
		AutoProbeModel: strings.TrimSpace(req.AutoProbeModel),
	}
	if !channel.MultiplierCardSupported {
		channel.MultiplierCardUserEnabled = false
	}
	channel.MultiplierCardSupported = channel.MultiplierCardUserEnabled
	if channel.AutoProbeIntervalMinutes == 0 {
		channel.AutoProbeIntervalMinutes = 10
	}
	markMarketplaceCapabilitiesPending(channel)
	group := newMarketplaceGroup(channelID, ownerUserID, ownerName, sourceLabel, req.Multiplier, req.Visibility)
	if err := applyPublicPresentation(group, &req.CustomName, &req.Description, &req.Tags); err != nil {
		return nil, nil, err
	}
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
	return listOwnerChannelViews(ownerUserID, nil)
}

// ListOwnerChannelsByIDs hydrates only the visible inventory page. The ownership
// predicate is applied before any group, income, or verification data is loaded.
func ListOwnerChannelsByIDs(ownerUserID int, ids []string) ([]ChannelView, error) {
	if ownerUserID <= 0 {
		return nil, errors.New("用户未登录")
	}
	if len(ids) > 50 {
		return nil, errors.New("每次最多查询 50 个渠道")
	}
	if len(ids) == 0 {
		return []ChannelView{}, nil
	}
	return listOwnerChannelViews(ownerUserID, ids)
}

func listOwnerChannelViews(ownerUserID int, ids []string) ([]ChannelView, error) {
	if ownerUserID <= 0 {
		return nil, errors.New("用户未登录")
	}
	var channels []marketplaceschema.Channel
	query := platformdb.DB.Unscoped().Omit("base_url_ciphertext", "credential_ciphertext", "transport_capabilities", "gpt56_mapping_results").Where("owner_user_id = ?", ownerUserID)
	if ids != nil {
		query = query.Where("id IN ?", ids)
	}
	if err := query.Order("deleted_at asc, created_at desc, id asc").Find(&channels).Error; err != nil {
		return nil, err
	}
	groups, err := groupsByChannelIDsWithDeleted(channelIDs(channels))
	if err != nil {
		return nil, err
	}
	earnings, err := earningsByOwnerGroupIDs(ownerUserID, groupIDs(groups))
	if err != nil {
		return nil, err
	}
	latestRuns, err := latestVerifications(channelIDs(channels))
	if err != nil {
		return nil, err
	}
	result := make([]ChannelView, 0, len(channels))
	for index := range channels {
		group := groups[channels[index].ID]
		if group != nil {
			view := channelViewWithLatestVerification(&channels[index], group, latestRuns[channels[index].ID])
			view.RequestCount = earnings[group.ID].RequestCount
			view.TotalIncome = earnings[group.ID].TotalIncome
			view.PendingIncome = earnings[group.ID].PendingIncome
			view.ReleasedIncome = earnings[group.ID].ReleasedIncome
			view.ReclaimedIncome = earnings[group.ID].ReclaimedIncome
			view.ForfeitedIncome = earnings[group.ID].ForfeitedIncome
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
	var reverify, capabilitiesChanged, presentationChanged bool
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		// Apply the patch to locked current records. A stale full-row save must
		// not overwrite a newer review decision, shop edit, or admin pause.
		var currentChannel marketplaceschema.Channel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND owner_user_id = ?", channel.ID, channel.OwnerUserID).First(&currentChannel).Error; err != nil {
			return err
		}
		var currentGroup marketplaceschema.Group
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND channel_id = ? AND owner_user_id = ?", group.ID, currentChannel.ID, currentChannel.OwnerUserID).First(&currentGroup).Error; err != nil {
			return err
		}
		*channel, *group = currentChannel, currentGroup
		previousModels := decodeModels(channel.DeclaredModels)
		previousResults := decodeModelVerificationResults(channel.ModelVerificationResults)
		previousProvider := channel.ProviderType
		previousBaseURL := channel.BaseURLCiphertext
		previousCredential := channel.CredentialCiphertext
		var err error
		reverify, err = applyChannelUpdate(channel, group, req)
		if err != nil {
			return err
		}
		presentationChanged = currentGroup.CustomName != group.CustomName || currentGroup.Description != group.Description || currentGroup.Tags != group.Tags || currentChannel.MaintenanceWindow != channel.MaintenanceWindow
		if presentationChanged {
			var shop marketplaceschema.Shop
			if err := tx.Where("owner_user_id = ?", channel.OwnerUserID).First(&shop).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			applyMarketplacePresentationRules(group, &shop, channel.MaintenanceWindow)
		}
		if consistencyStatus != nil {
			if err := applyModelConsistencyStatus(channel, *consistencyStatus); err != nil {
				return err
			}
		}
		modelsChanged := strings.Join(previousModels, "\x00") != strings.Join(decodeModels(channel.DeclaredModels), "\x00")
		credentialTransportChanged := previousProvider != channel.ProviderType ||
			previousBaseURL != channel.BaseURLCiphertext ||
			previousCredential != channel.CredentialCiphertext
		capabilitiesChanged = credentialTransportChanged || modelsChanged
		if capabilitiesChanged {
			markMarketplaceCapabilitiesPending(channel)
		}
		if modelsChanged && !credentialTransportChanged {
			channel.ModelVerificationResults = encodeModelVerificationResults(
				mergeModelVerificationResults(decodeModels(channel.DeclaredModels), previousResults, nil),
			)
		}
		// Keep a distinct content revision even for rapid edit-and-undo saves.
		// Database timestamp precision and host clocks can otherwise coincide.
		revision := time.Now().UTC().Truncate(time.Microsecond)
		if !revision.After(group.UpdatedAt) {
			revision = group.UpdatedAt.Add(time.Microsecond)
		}
		if !revision.After(channel.UpdatedAt) {
			revision = channel.UpdatedAt.Add(time.Microsecond)
		}
		tx = tx.Session(&gorm.Session{NowFunc: func() time.Time { return revision }})
		if reverify {
			if err := pauseActiveVerificationRunsWithDB(tx, channel.ID, time.Now().UTC()); err != nil {
				return err
			}
		}
		if err := tx.Save(channel).Error; err != nil {
			return err
		}
		if marketplaceChannelAdminHeld(channel) {
			if err := syncPausedMarketplaceChannelWithDB(tx, channel, true); err != nil {
				return err
			}
		}
		return tx.Save(group).Error
	}); err != nil {
		return nil, err
	}
	if reverify {
		marketplaceVerificationTasks.cancelChannel(channel.ID)
	}
	if capabilitiesChanged || !channel.AutoProbeEnabled {
		marketplaceAutoProbeTasks.cancelChannel(channel.ID)
	}
	invalidateAdminMarketplaceStatsCache()
	if channel.InternalChannelID != nil {
		if err := syncInternalChannel(channel, group); err != nil {
			return nil, err
		}
	}
	if capabilitiesChanged {
		queueMarketplaceCapabilityProbe(channel.ID)
	}
	if presentationChanged {
		if _, err := preparePresentationProbe(context.Background(), channel); err != nil {
			return nil, err
		}
		if err := platformdb.DB.First(group, "id = ?", group.ID).Error; err != nil {
			return nil, err
		}
	}
	return channelView(channel, group), nil
}

func PauseOwnerChannel(ownerUserID int, channelID string, paused bool) error {
	channel, group, err := loadOwnedChannelGroup(ownerUserID, channelID)
	if err != nil {
		return err
	}
	if marketplaceChannelAdminHeld(channel) {
		return errors.New("渠道已停用，请联系管理员")
	}
	status := marketplacedomain.LifecycleActive
	if paused {
		status = marketplacedomain.LifecycleSuspended
	} else if group.VerificationStatus != marketplacedomain.VerificationPassed {
		return errors.New("检测未通过，不能恢复服务")
	}
	return setMarketplaceChannelLifecycle(channel, group, status, paused, true)
}

func PauseAdminChannel(channelID string, paused bool) error {
	channel, group, err := loadChannelGroup(channelID)
	if err != nil {
		return err
	}
	status := marketplacedomain.LifecycleActive
	if paused {
		status = marketplacedomain.LifecycleDisabled
	}
	return setMarketplaceChannelLifecycle(channel, group, status, paused, false)
}

func setMarketplaceChannelLifecycle(channel *marketplaceschema.Channel, group *marketplaceschema.Group, status string, paused, ownerAction bool) error {
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var currentChannel marketplaceschema.Channel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND owner_user_id = ?", channel.ID, channel.OwnerUserID).First(&currentChannel).Error; err != nil {
			return err
		}
		var currentGroup marketplaceschema.Group
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND channel_id = ? AND owner_user_id = ?", group.ID, channel.ID, channel.OwnerUserID).First(&currentGroup).Error; err != nil {
			return err
		}
		if ownerAction {
			if marketplaceChannelAdminHeld(&currentChannel) {
				return errors.New("渠道已停用，请联系管理员")
			}
			if !paused && currentGroup.VerificationStatus != marketplacedomain.VerificationPassed {
				return errors.New("检测未通过，不能恢复服务")
			}
		}
		if !paused && !ownerAction && (currentGroup.VerificationStatus != marketplacedomain.VerificationPassed || currentChannel.SourceLabelStatus == marketplacedomain.SourceLabelRejected) {
			return errors.New("渠道需要通过检测及管理员审核后才能恢复")
		}
		*channel, *group = currentChannel, currentGroup
		if err := tx.Model(channel).Update("status", status).Error; err != nil {
			return err
		}
		if err := tx.Model(group).Update("lifecycle_status", status).Error; err != nil {
			return err
		}
		return syncPausedMarketplaceChannelWithDB(tx, channel, paused)
	})
	if err != nil {
		return err
	}
	if paused {
		marketplaceAutoProbeTasks.cancelChannel(channel.ID)
		marketplaceVerificationTasks.cancelChannel(channel.ID)
	}
	gatewaystore.InitChannelCache()
	gatewaystore.NotifyChannelCacheChanged()
	invalidateAdminMarketplaceStatsCache()
	return nil
}

func marketplaceChannelAdminHeld(channel *marketplaceschema.Channel) bool {
	return channel != nil && (channel.Status == marketplacedomain.LifecycleDisabled || channel.SourceLabelStatus == marketplacedomain.SourceLabelRejected)
}

// syncPausedMarketplaceChannel prevents a paused marketplace channel from
// being selected by gateway traffic or the periodic full-channel test task.
// Marketplace and gateway channels are stored separately, so the lifecycle
// update must be reflected in the linked internal channel explicitly.
func syncPausedMarketplaceChannel(channel *marketplaceschema.Channel, paused bool) error {
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		return syncPausedMarketplaceChannelWithDB(tx, channel, paused)
	}); err != nil {
		return err
	}
	gatewaystore.InitChannelCache()
	gatewaystore.NotifyChannelCacheChanged()
	return nil
}

func syncPausedMarketplaceChannelWithDB(tx *gorm.DB, channel *marketplaceschema.Channel, paused bool) error {
	if channel == nil || channel.InternalChannelID == nil || *channel.InternalChannelID <= 0 {
		return nil
	}
	targetStatus := constant.ChannelStatusEnabled
	if paused {
		targetStatus = constant.ChannelStatusManuallyDisabled
	}
	var internal gatewayschema.Channel
	if err := tx.First(&internal, *channel.InternalChannelID).Error; err != nil {
		return err
	}
	if err := tx.Model(&internal).Update("status", targetStatus).Error; err != nil {
		return err
	}
	internal.Status = targetStatus
	// Rebuild missing abilities too: an enabled channel alone does not make
	// its original group/model selectable after a pause or interrupted sync.
	return gatewaystore.UpdateChannelAbilities(&internal, tx)
}

func SetChannelUserBlock(ownerUserID int, channelID string, targetUserID int, blocked bool) error {
	if ownerUserID <= 0 || targetUserID <= 0 || channelID == "" {
		return errors.New("参数无效")
	}
	if ownerUserID == targetUserID {
		return errors.New("不能拉黑自己")
	}
	var channel marketplaceschema.Channel
	if err := platformdb.DB.Where("id = ? AND owner_user_id = ?", channelID, ownerUserID).First(&channel).Error; err != nil {
		return err
	}
	if blocked {
		return platformdb.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&marketplaceschema.ChannelUserBlock{ChannelID: channelID, UserID: targetUserID}).Error
	}
	return platformdb.DB.Where("channel_id = ? AND user_id = ?", channelID, targetUserID).Delete(&marketplaceschema.ChannelUserBlock{}).Error
}

func SetChannelUserBlockByExternalID(ownerUserID int, channelID string, targetExternalID string, blocked bool) error {
	targetExternalID = strings.ToUpper(strings.TrimSpace(targetExternalID))
	if targetExternalID == "" {
		return errors.New("用户编号不能为空")
	}
	var user identityschema.User
	if err := platformdb.DB.Select("id").Where("external_id = ?", targetExternalID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("用户编号不存在")
		}
		return err
	}
	return SetChannelUserBlock(ownerUserID, channelID, user.Id, blocked)
}

func ListChannelUserBlocks(ownerUserID int, channelID string, page int, pageSize int) (*ChannelUserBlockList, error) {
	if ownerUserID <= 0 || strings.TrimSpace(channelID) == "" {
		return nil, errors.New("参数无效")
	}
	var channel marketplaceschema.Channel
	if err := platformdb.DB.Select("id").Where("id = ? AND owner_user_id = ?", channelID, ownerUserID).First(&channel).Error; err != nil {
		return nil, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	query := platformdb.DB.Model(&marketplaceschema.ChannelUserBlock{}).Where("channel_id = ?", channelID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	blocks := make([]marketplaceschema.ChannelUserBlock, 0, pageSize)
	if err := query.Order("created_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&blocks).Error; err != nil {
		return nil, err
	}

	userIDs := make([]int, 0, len(blocks))
	for _, block := range blocks {
		userIDs = append(userIDs, block.UserID)
	}
	usersByID := make(map[int]identityschema.User, len(userIDs))
	if len(userIDs) > 0 {
		var users []identityschema.User
		if err := platformdb.DB.Unscoped().Select("id", "external_id", "username", "display_name").Where("id IN ?", userIDs).Find(&users).Error; err != nil {
			return nil, err
		}
		for _, user := range users {
			usersByID[user.Id] = user
		}
	}

	items := make([]ChannelUserBlockView, 0, len(blocks))
	for _, block := range blocks {
		user := usersByID[block.UserID]
		items = append(items, ChannelUserBlockView{
			UserID: block.UserID, UserExternalID: user.ExternalId, Username: user.Username,
			DisplayName: user.DisplayName, BlockedAt: block.CreatedAt,
		})
	}
	return &ChannelUserBlockList{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func loadOwnedChannelGroup(ownerUserID int, channelID string) (*marketplaceschema.Channel, *marketplaceschema.Group, error) {
	var channel marketplaceschema.Channel
	if err := platformdb.DB.Where("id = ? AND owner_user_id = ?", channelID, ownerUserID).First(&channel).Error; err != nil {
		return nil, nil, err
	}
	var group marketplaceschema.Group
	if err := platformdb.DB.Where("channel_id = ? AND owner_user_id = ?", channel.ID, ownerUserID).First(&group).Error; err != nil {
		return nil, nil, err
	}
	return &channel, &group, nil
}

func groupsByChannelIDs(ids []string) (map[string]*marketplaceschema.Group, error) {
	return groupsByChannelIDsQuery(platformdb.DB, ids)
}

func groupsByChannelIDsWithDeleted(ids []string) (map[string]*marketplaceschema.Group, error) {
	return groupsByChannelIDsQuery(platformdb.DB.Unscoped(), ids)
}

func groupsByChannelIDsQuery(query *gorm.DB, ids []string) (map[string]*marketplaceschema.Group, error) {
	result := make(map[string]*marketplaceschema.Group)
	if len(ids) == 0 {
		return result, nil
	}
	var groups []marketplaceschema.Group
	if err := query.Where("channel_id IN ?", ids).Find(&groups).Error; err != nil {
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
