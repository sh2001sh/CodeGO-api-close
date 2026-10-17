package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/constant"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func ListAdminChannels(input AdminChannelQuery) ([]ChannelView, error) {
	if input.StartTimestamp > 0 && input.EndTimestamp > 0 && input.StartTimestamp > input.EndTimestamp {
		input.StartTimestamp, input.EndTimestamp = input.EndTimestamp, input.StartTimestamp
	}
	cacheKey := fmt.Sprintf("%p:%+v", platformdb.DB, input)
	value, err, _ := adminChannelsLoads.Do(cacheKey, func() (any, error) {
		return listAdminChannelsCached(input, cacheKey)
	})
	if err != nil {
		return nil, err
	}
	return cloneChannelViews(value.([]ChannelView)), nil
}

// ListAdminChannelsPage keeps filtering and pagination in the database so the
// governance table only enriches the rows visible on the current page.
func ListAdminChannelsPage(input AdminChannelQuery) (*AdminChannelListResult, error) {
	if input.Page < 1 {
		input.Page = 1
	}
	if input.PageSize < 1 {
		input.PageSize = 50
	} else if input.PageSize > 200 {
		input.PageSize = 200
	}
	if input.StartTimestamp > 0 && input.EndTimestamp > 0 && input.StartTimestamp > input.EndTimestamp {
		input.StartTimestamp, input.EndTimestamp = input.EndTimestamp, input.StartTimestamp
	}

	channelTable := marketplaceschema.Channel{}.TableName()
	groupTable := marketplaceschema.Group{}.TableName()
	query := platformdb.DB.Table(channelTable + " AS c").
		Joins("JOIN " + groupTable + " AS g ON g.channel_id = c.id AND g.deleted_at IS NULL").
		Where("c.deleted_at IS NULL")
	if source := strings.TrimSpace(input.Source); source != "" {
		query = query.Where("c.submitted_source_label = ? OR c.approved_source_label = ?", source, source)
	}
	if provider := strings.TrimSpace(input.Provider); provider != "" {
		query = query.Where("c.provider_type = ?", provider)
	}
	if normalizedSearch := normalizeExternalIDSearch(input.OwnerSearch); normalizedSearch != "" {
		ownerUserIDs, err := ownerUserIDsByExternalID(normalizedSearch)
		if err != nil {
			return nil, err
		}
		if len(ownerUserIDs) == 0 {
			return &AdminChannelListResult{Items: []ChannelView{}, Page: input.Page, PageSize: input.PageSize}, nil
		}
		query = query.Where("c.owner_user_id IN ?", ownerUserIDs)
	}
	if status := strings.TrimSpace(input.Status); status != "" {
		query = query.Where("c.status = ?", status)
	}
	if verification := strings.TrimSpace(input.Verification); verification != "" {
		query = query.Where("g.verification_status = ?", verification)
	}
	if search := strings.TrimSpace(input.Search); search != "" {
		pattern := ownerLogLike(search)
		query = query.Where(
			"(LOWER(c.id) LIKE ? ESCAPE '!' OR LOWER(g.system_display_name) LIKE ? ESCAPE '!' OR LOWER(c.provider_type) LIKE ? ESCAPE '!' OR LOWER(c.submitted_source_label) LIKE ? ESCAPE '!' OR LOWER(c.approved_source_label) LIKE ? ESCAPE '!' OR LOWER(c.declared_models) LIKE ? ESCAPE '!' OR LOWER(g.custom_name) LIKE ? ESCAPE '!' OR LOWER(g.description) LIKE ? ESCAPE '!' OR LOWER(g.tags) LIKE ? ESCAPE '!')",
			pattern, pattern, pattern, pattern, pattern, pattern, pattern, pattern, pattern,
		)
	}

	var total int64
	if err := query.Session(&gorm.Session{}).Distinct("c.id").Count(&total).Error; err != nil {
		return nil, err
	}
	var channels []marketplaceschema.Channel
	if err := query.Select("c.*").Order("c.updated_at DESC, c.id ASC").
		Offset((input.Page - 1) * input.PageSize).Limit(input.PageSize).
		Scan(&channels).Error; err != nil {
		return nil, err
	}
	items, err := enrichAdminChannelPage(channels, input)
	if err != nil {
		return nil, err
	}
	return &AdminChannelListResult{Items: items, Total: total, Page: input.Page, PageSize: input.PageSize}, nil
}

func enrichAdminChannelPage(channels []marketplaceschema.Channel, input AdminChannelQuery) ([]ChannelView, error) {
	if len(channels) == 0 {
		return []ChannelView{}, nil
	}
	groups, err := groupsByChannelIDs(channelIDs(channels))
	if err != nil {
		return nil, err
	}
	ownerUserIDs := make([]int, 0, len(channels))
	for index := range channels {
		ownerUserIDs = append(ownerUserIDs, channels[index].OwnerUserID)
	}
	var earnings map[string]ownerChannelEarnings
	var externalIDs map[int]string
	var latestRuns map[string]*marketplaceschema.VerificationRun
	loadEarnings := func() error {
		var err error
		earnings, err = earningsByGroupIDsInRange(groupIDs(groups), input.StartTimestamp, input.EndTimestamp)
		return err
	}
	loadOwners := func() error {
		var err error
		externalIDs, err = ownerExternalIDs(ownerUserIDs)
		return err
	}
	loadVerifications := func() error {
		var err error
		latestRuns, err = latestVerifications(channelIDs(channels))
		return err
	}
	if platformdb.DB.Dialector.Name() == "sqlite" {
		if err := loadEarnings(); err != nil {
			return nil, err
		}
		if err := loadOwners(); err != nil {
			return nil, err
		}
		if err := loadVerifications(); err != nil {
			return nil, err
		}
	} else {
		var loaders errgroup.Group
		loaders.Go(loadEarnings)
		loaders.Go(loadOwners)
		loaders.Go(loadVerifications)
		if err := loaders.Wait(); err != nil {
			return nil, err
		}
	}
	result := make([]ChannelView, 0, len(channels))
	for index := range channels {
		group := groups[channels[index].ID]
		if group == nil {
			continue
		}
		view := channelViewWithLatestVerification(&channels[index], group, latestRuns[channels[index].ID])
		view.OwnerExternalID = externalIDs[channels[index].OwnerUserID]
		view.RequestCount = earnings[group.ID].RequestCount
		view.TotalIncome = earnings[group.ID].TotalIncome
		view.PendingIncome = earnings[group.ID].PendingIncome
		view.ReleasedIncome = earnings[group.ID].ReleasedIncome
		view.ReclaimedIncome = earnings[group.ID].ReclaimedIncome
		view.ForfeitedIncome = earnings[group.ID].ForfeitedIncome
		result = append(result, *view)
	}
	return result, nil
}

func listAdminChannelsCached(input AdminChannelQuery, cacheKey string) ([]ChannelView, error) {
	startedAt := time.Now()
	adminMarketplaceStatsCache.Lock()
	if adminMarketplaceStatsCache.adminChannelsResult != nil &&
		adminMarketplaceStatsCache.adminChannelsKey == cacheKey &&
		time.Since(adminMarketplaceStatsCache.adminChannelsAt) < adminMarketplaceStatsCacheTTL {
		result := cloneChannelViews(adminMarketplaceStatsCache.adminChannelsResult)
		adminMarketplaceStatsCache.Unlock()
		return result, nil
	}
	adminMarketplaceStatsCache.Unlock()
	channelsStartedAt := time.Now()
	query := platformdb.DB.Model(&marketplaceschema.Channel{})
	if source := strings.TrimSpace(input.Source); source != "" {
		query = query.Where("submitted_source_label = ? OR approved_source_label = ?", source, source)
	}
	if provider := strings.TrimSpace(input.Provider); provider != "" {
		query = query.Where("provider_type = ?", provider)
	}
	if normalizedSearch := normalizeExternalIDSearch(input.OwnerSearch); normalizedSearch != "" {
		ownerUserIDs, err := ownerUserIDsByExternalID(normalizedSearch)
		if err != nil {
			return nil, err
		}
		if len(ownerUserIDs) == 0 {
			return []ChannelView{}, nil
		}
		query = query.Where("owner_user_id IN ?", ownerUserIDs)
	}
	if strings.TrimSpace(input.Status) != "" {
		query = query.Where("status = ?", input.Status)
	}
	var channels []marketplaceschema.Channel
	if err := query.Order("updated_at desc").Find(&channels).Error; err != nil {
		return nil, err
	}
	channelsElapsed := time.Since(channelsStartedAt)
	groupsStartedAt := time.Now()
	groups, err := groupsByChannelIDs(channelIDs(channels))
	if err != nil {
		return nil, err
	}
	groupsElapsed := time.Since(groupsStartedAt)
	ownerUserIDs := make([]int, 0, len(channels))
	for index := range channels {
		ownerUserIDs = append(ownerUserIDs, channels[index].OwnerUserID)
	}
	var earnings map[string]ownerChannelEarnings
	var externalIDs map[int]string
	var latestRuns map[string]*marketplaceschema.VerificationRun
	var earningsElapsed, ownersElapsed, verificationsElapsed time.Duration
	loadEarnings := func() error {
		at := time.Now()
		var err error
		earnings, err = earningsByGroupIDsInRange(groupIDs(groups), input.StartTimestamp, input.EndTimestamp)
		earningsElapsed = time.Since(at)
		return err
	}
	loadOwners := func() error {
		at := time.Now()
		var err error
		externalIDs, err = ownerExternalIDs(ownerUserIDs)
		ownersElapsed = time.Since(at)
		return err
	}
	loadVerifications := func() error {
		at := time.Now()
		var err error
		latestRuns, err = latestVerifications(channelIDs(channels))
		verificationsElapsed = time.Since(at)
		return err
	}
	if platformdb.DB.Dialector.Name() == "sqlite" {
		if err := loadEarnings(); err != nil {
			return nil, err
		}
		if err := loadOwners(); err != nil {
			return nil, err
		}
		if err := loadVerifications(); err != nil {
			return nil, err
		}
	} else {
		var loaders errgroup.Group
		loaders.Go(loadEarnings)
		loaders.Go(loadOwners)
		loaders.Go(loadVerifications)
		if err := loaders.Wait(); err != nil {
			return nil, err
		}
	}
	result := make([]ChannelView, 0, len(channels))
	for index := range channels {
		if group := groups[channels[index].ID]; group != nil {
			if search := strings.ToLower(strings.TrimSpace(input.Search)); search != "" {
				searchable := strings.ToLower(strings.Join([]string{
					channels[index].ID, group.SystemDisplayName, channels[index].ProviderType,
					channels[index].SubmittedSourceLabel, channels[index].ApprovedSourceLabel,
					channels[index].DeclaredModels,
				}, " "))
				if !strings.Contains(searchable, search) {
					continue
				}
			}
			if verification := strings.TrimSpace(input.Verification); verification != "" && group.VerificationStatus != verification {
				continue
			}
			view := channelViewWithLatestVerification(&channels[index], group, latestRuns[channels[index].ID])
			view.OwnerExternalID = externalIDs[channels[index].OwnerUserID]
			view.RequestCount = earnings[group.ID].RequestCount
			view.TotalIncome = earnings[group.ID].TotalIncome
			view.PendingIncome = earnings[group.ID].PendingIncome
			view.ReleasedIncome = earnings[group.ID].ReleasedIncome
			view.ReclaimedIncome = earnings[group.ID].ReclaimedIncome
			view.ForfeitedIncome = earnings[group.ID].ForfeitedIncome
			result = append(result, *view)
		}
	}
	adminMarketplaceStatsCache.Lock()
	adminMarketplaceStatsCache.adminChannelsAt = time.Now()
	adminMarketplaceStatsCache.adminChannelsKey = cacheKey
	adminMarketplaceStatsCache.adminChannelsResult = cloneChannelViews(result)
	adminMarketplaceStatsCache.Unlock()
	totalElapsed := time.Since(startedAt)
	if totalElapsed >= 500*time.Millisecond {
		platformobservability.SysLog(fmt.Sprintf("slow admin marketplace query channels_ms=%d groups_ms=%d earnings_ms=%d owners_ms=%d verifications_ms=%d rows=%d total_ms=%d", channelsElapsed.Milliseconds(), groupsElapsed.Milliseconds(), earningsElapsed.Milliseconds(), ownersElapsed.Milliseconds(), verificationsElapsed.Milliseconds(), len(result), totalElapsed.Milliseconds()))
	}
	return result, nil
}

func ReviewChannel(channelID string, req AdminReviewRequest) (*ChannelView, error) {
	if strings.TrimSpace(req.Reason) == "" {
		return nil, errors.New("管理员审核必须填写原因")
	}
	channel, group := &marketplaceschema.Channel{}, &marketplaceschema.Group{}
	now := time.Now().UTC()
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(channel, "id = ?", channelID).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(group, "channel_id = ?", channelID).Error; err != nil {
			return err
		}
		channel.LastReviewReason, channel.SourceLabelReviewReason = req.Reason, req.Reason
		if req.Approved {
			if group.VerificationStatus != marketplacedomain.VerificationPassed {
				return errors.New("渠道检测未通过，不能发布")
			}
			channel.Status, group.LifecycleStatus = marketplacedomain.LifecycleActive, marketplacedomain.LifecycleActive
			channel.ApprovedSourceLabel, channel.SourceLabelStatus = channel.SubmittedSourceLabel, marketplacedomain.SourceLabelApproved
			group.ModerationStatus, group.ModerationReason, group.ModerationCheckedAt = "passed", req.Reason, &now
			group.PublishedAt = &now
			if channel.InternalChannelID == nil {
				if err := createInternalChannelWithDB(tx, channel, group); err != nil {
					return err
				}
			}
		} else {
			channel.Status, group.LifecycleStatus = marketplacedomain.LifecycleDisabled, marketplacedomain.LifecycleDisabled
			channel.ApprovedSourceLabel, channel.SourceLabelStatus = "", marketplacedomain.SourceLabelRejected
			if err := pauseActiveVerificationRunsWithDB(tx, channelID, now); err != nil {
				return err
			}
		}
		if err := tx.Save(channel).Error; err != nil {
			return err
		}
		if err := tx.Save(group).Error; err != nil {
			return err
		}
		return syncPausedMarketplaceChannelWithDB(tx, channel, !req.Approved)
	})
	if err != nil {
		return nil, err
	}
	if !req.Approved {
		marketplaceVerificationTasks.cancelChannel(channelID)
		marketplaceAutoProbeTasks.cancelChannel(channelID)
	}
	gatewaystore.InitChannelCache()
	gatewaystore.NotifyChannelCacheChanged()
	invalidateAdminMarketplaceStatsCache()
	return channelView(channel, group), nil
}

func createInternalChannel(channel *marketplaceschema.Channel, group *marketplaceschema.Group) error {
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var current marketplaceschema.Channel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", channel.ID).Error; err != nil {
			return err
		}
		if current.Status == marketplacedomain.LifecycleSuspended || marketplaceChannelAdminHeld(&current) {
			return nil
		}
		if current.InternalChannelID != nil {
			channel.InternalChannelID = current.InternalChannelID
			return nil
		}
		return createInternalChannelWithDB(tx, channel, group)
	})
}

func createInternalChannelWithDB(tx *gorm.DB, channel *marketplaceschema.Channel, group *marketplaceschema.Group) error {
	metadata, _ := json.Marshal(map[string]any{
		"marketplace_channel_id": channel.ID, "marketplace_group_id": group.ID,
		"source_type": group.SourceType, "credit_pool_policy": group.CreditPoolPolicy,
	})
	baseURL := channel.BaseURLCiphertext
	internal := &gatewayschema.Channel{
		Type: providerChannelType(channel.ProviderType), Key: channel.CredentialCiphertext,
		Status: constant.ChannelStatusEnabled, Name: group.SystemDisplayName,
		ChannelScope:                     gatewayschema.ChannelScopeExternal,
		MarketplaceMaxConcurrency:        channel.MaxConcurrency,
		MarketplaceUserMaxConcurrency:    channel.UserMaxConcurrency,
		SensitiveWordInterceptionEnabled: channel.SensitiveWordInterceptionEnabled,
		MultiplierCardSupported:          channel.MultiplierCardSupported,
		CreditPackSupported:              channel.CreditPackSupported,
		MultiplierCardUserEnabled:        channel.MultiplierCardUserEnabled,
		CreatedTime:                      platformruntime.GetTimestamp(), BaseURL: &baseURL,
		Models: strings.Join(decodeModels(channel.DeclaredModels), ","), Group: group.InternalGroupName,
		OtherInfo: string(metadata),
	}
	internal.ChannelInfo.ResponsesCapabilities = decodeMarketplaceCapabilities(channel.TransportCapabilities)
	if err := tx.Create(internal).Error; err != nil {
		return err
	}
	if err := gatewaystore.AddChannelAbilities(internal, tx); err != nil {
		return err
	}
	result := tx.Model(channel).Update("internal_channel_id", internal.Id)
	if result.Error != nil || result.RowsAffected != 1 {
		if result.Error != nil {
			return result.Error
		}
		return errors.New("渠道已删除")
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
