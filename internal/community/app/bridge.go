package app

import (
	"crypto/subtle"
	"errors"
	"os"
	"regexp"
	"strings"

	"github.com/sh2001sh/new-api/constant"
	communityschema "github.com/sh2001sh/new-api/internal/community/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const CommunityAPISecretEnvironment = "CODEGO_COMMUNITY_API_SECRET"

var (
	ErrCommunityAPIDisabled       = errors.New("community API is not configured")
	ErrCommunityAPIUnauthorized   = errors.New("community API authentication failed")
	ErrInvalidCommunitySubject    = errors.New("invalid community member subject")
	ErrCommunityMemberNotFound    = errors.New("community member not found")
	ErrCommunityMemberInactive    = errors.New("community member is inactive")
	ErrInvalidCommunityPagination = errors.New("invalid community channel pagination")
	ErrInvalidCommunityQuery      = errors.New("invalid community channel query")
	ErrInvalidCommunityRating     = errors.New("invalid community channel rating")
	ErrCommunitySelfRating        = errors.New("channel owners cannot rate their own channels")
	ErrCommunityChannelNotFound   = errors.New("community channel not found")
)

var communityProviderPattern = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)
var communityPhoneIdentityPattern = regexp.MustCompile(`^\+?[0-9][0-9\s().-]{5,}[0-9]$`)

type CommunityMember struct {
	Subject              string  `json:"sub"`
	Active               bool    `json:"active"`
	Username             string  `json:"username,omitempty"`
	DisplayName          string  `json:"display_name,omitempty"`
	VerifiedChannelOwner bool    `json:"verified_channel_owner"`
	AverageScore         float64 `json:"average_score"`
	RatingCount          int64   `json:"rating_count"`
}

type CommunityChannel struct {
	ID                 string  `json:"id"`
	Slug               string  `json:"slug"`
	Name               string  `json:"name"`
	Provider           string  `json:"provider"`
	LifecycleStatus    string  `json:"lifecycle_status"`
	VerificationStatus string  `json:"verification_status"`
	AverageScore       float64 `json:"average_score"`
	RatingCount        int64   `json:"rating_count"`
	ViewerStars        int     `json:"viewer_stars"`
}

type CommunityChannelList struct {
	Items    []CommunityChannel `json:"items"`
	Total    int64              `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
}

type CommunitySeller struct {
	Subject      string             `json:"sub"`
	Username     string             `json:"username"`
	DisplayName  string             `json:"display_name"`
	ChannelCount int64              `json:"channel_count"`
	Channels     []CommunityChannel `json:"channels"`
	AverageScore float64            `json:"average_score"`
	RatingCount  int64              `json:"rating_count"`
}

type CommunitySellerList struct {
	Items    []CommunitySeller `json:"items"`
	Total    int64             `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
}

type CommunityRatingRequest struct {
	ViewerSubject string `json:"viewer_sub"`
	Stars         int    `json:"stars"`
}

type CommunityRatingSummary struct {
	AverageScore float64 `json:"average_score"`
	RatingCount  int64   `json:"rating_count"`
	ViewerStars  int     `json:"viewer_stars"`
}

type CommunityRatingResult struct {
	Channel CommunityRatingSummary `json:"channel"`
	Seller  CommunityRatingSummary `json:"seller"`
}

// ListCommunitySellers exposes only active owners with eligible public channels.
// Search matches both owner identity and public channel names. Counts, ratings,
// and previews reflect the selected provider when one is supplied.
func ListCommunitySellers(page, pageSize int, keyword, provider, sort string) (*CommunitySellerList, error) {
	keyword = strings.TrimSpace(keyword)
	provider = strings.TrimSpace(provider)
	sort = strings.TrimSpace(sort)
	if sort == "" {
		sort = "rating"
	}
	if page < 1 || page > 10000 || pageSize < 1 || pageSize > 50 ||
		len([]rune(keyword)) > 64 || (provider != "" && !communityProviderPattern.MatchString(provider)) ||
		!validCommunitySellerSort(sort) {
		return nil, ErrInvalidCommunityQuery
	}

	result := &CommunitySellerList{Items: make([]CommunitySeller, 0), Page: page, PageSize: pageSize}
	if err := communitySellerQuery(keyword, provider).Distinct("community_users.id").Count(&result.Total).Error; err != nil {
		return nil, err
	}
	if result.Total == 0 || (page-1)*pageSize >= int(result.Total) {
		return result, nil
	}
	type sellerRow struct {
		OwnerID      int     `gorm:"column:owner_id"`
		Subject      string  `gorm:"column:sub"`
		Username     string  `gorm:"column:username"`
		DisplayName  string  `gorm:"column:display_name"`
		ChannelCount int64   `gorm:"column:channel_count"`
		AverageScore float64 `gorm:"column:average_score"`
		RatingCount  int64   `gorm:"column:rating_count"`
	}
	var rows []sellerRow
	// Build a fresh statement after Count: GORM's Distinct used for the count can
	// otherwise leak into the grouped page query and PostgreSQL rejects its ORDER BY.
	query := communitySellerQuery(keyword, provider).
		Joins("LEFT JOIN " + communityschema.ChannelRating{}.TableName() + " AS community_ratings ON community_ratings.channel_id = community_channels.id")
	if err := query.Select(`community_users.id AS owner_id, community_users.external_id AS sub,
		community_users.username AS username,
		COALESCE(NULLIF(TRIM(community_users.display_name), ''), community_users.username) AS display_name,
		COUNT(DISTINCT community_groups.id) AS channel_count,
		COALESCE(AVG(community_ratings.stars) * 2.0, 0) AS average_score,
		COUNT(community_ratings.id) AS rating_count`).
		Group("community_users.id, community_users.external_id, community_users.username, community_users.display_name").
		Scopes(communitySellerSortScope(sort)).
		Limit(pageSize).Offset((page - 1) * pageSize).Scan(&rows).Error; err != nil {
		return nil, err
	}
	ownerIDs := make([]int, 0, len(rows))
	for _, row := range rows {
		publicUsername, publicDisplayName := publicCommunityIdentity(row.Subject, row.Username, row.DisplayName)
		ownerIDs = append(ownerIDs, row.OwnerID)
		result.Items = append(result.Items, CommunitySeller{
			Subject: row.Subject, Username: publicUsername, DisplayName: publicDisplayName,
			ChannelCount: row.ChannelCount, Channels: make([]CommunityChannel, 0),
			AverageScore: row.AverageScore, RatingCount: row.RatingCount,
		})
	}
	if len(ownerIDs) == 0 {
		return result, nil
	}
	previewQuery := communitySellerQuery(keyword, provider).
		Where("community_groups.owner_user_id IN ?", ownerIDs).
		Joins("LEFT JOIN (?) AS community_rating_stats ON community_rating_stats.channel_id = community_channels.id", communityChannelRatingStatsQuery())
	ranked := previewQuery.Select(`community_groups.owner_user_id AS owner_id,
		community_channels.id AS id, community_groups.public_slug AS slug,
		community_groups.system_display_name AS name, community_channels.provider_type AS provider,
		community_groups.lifecycle_status AS lifecycle_status,
		community_groups.verification_status AS verification_status,
		COALESCE(community_rating_stats.average_score, 0) AS average_score,
		COALESCE(community_rating_stats.rating_count, 0) AS rating_count,
		ROW_NUMBER() OVER (PARTITION BY community_groups.owner_user_id
			ORDER BY CASE WHEN COALESCE(community_rating_stats.rating_count, 0) > 0 THEN 0 ELSE 1 END ASC,
			COALESCE(community_rating_stats.average_score, 0) DESC,
			community_groups.updated_at DESC, community_groups.id ASC) AS row_num`)
	type previewRow struct {
		OwnerID int `gorm:"column:owner_id"`
		CommunityChannel
	}
	var previews []previewRow
	if err := platformdb.DB.Table("(?) AS ranked", ranked).
		Where("ranked.row_num <= ?", 3).
		Order("ranked.owner_id ASC, ranked.row_num ASC").Scan(&previews).Error; err != nil {
		return nil, err
	}
	index := make(map[int]int, len(rows))
	for i, row := range rows {
		index[row.OwnerID] = i
	}
	for _, preview := range previews {
		i := index[preview.OwnerID]
		result.Items[i].Channels = append(result.Items[i].Channels, preview.CommunityChannel)
	}
	return result, nil
}

func communitySellerQuery(keyword, provider string) *gorm.DB {
	query := publicCommunityChannelQuery().
		Joins("JOIN users AS community_users ON community_users.id = community_groups.owner_user_id").
		Where("community_users.status = ? AND community_users.deleted_at IS NULL AND community_users.external_id <> ''", constant.UserStatusEnabled)
	if keyword != "" {
		escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(keyword))
		term := "%" + escaped + "%"
		matchingChannelOwners := publicCommunityChannelQuery().
			Select("community_groups.owner_user_id").
			Where("(LOWER(community_groups.system_display_name) LIKE ? ESCAPE '!' OR LOWER(community_groups.public_slug) LIKE ? ESCAPE '!')", term, term)
		query = query.Where(`(LOWER(community_users.username) LIKE ? ESCAPE '!' OR
			LOWER(community_users.display_name) LIKE ? ESCAPE '!' OR
			community_users.id IN (?))`, term, term, matchingChannelOwners)
	}
	if provider != "" {
		query = query.Where("community_channels.provider_type = ?", provider)
	}
	return query
}

func validCommunitySellerSort(sort string) bool {
	switch sort {
	case "rating", "recent", "channels":
		return true
	default:
		return false
	}
}

func communitySellerSortScope(sort string) func(*gorm.DB) *gorm.DB {
	return func(query *gorm.DB) *gorm.DB {
		switch sort {
		case "recent":
			return query.Order("MAX(community_groups.updated_at) DESC").Order("community_users.external_id ASC")
		case "channels":
			return query.Order("COUNT(DISTINCT community_groups.id) DESC").Order("MAX(community_groups.updated_at) DESC").Order("community_users.external_id ASC")
		default:
			return query.Order("CASE WHEN COUNT(community_ratings.id) > 0 THEN 0 ELSE 1 END ASC").
				Order("COALESCE(AVG(community_ratings.stars), 0) DESC").
				Order("COUNT(community_ratings.id) DESC").
				Order("MAX(community_groups.updated_at) DESC").
				Order("community_users.external_id ASC")
		}
	}
}

func communityChannelRatingStatsQuery() *gorm.DB {
	return platformdb.DB.Model(&communityschema.ChannelRating{}).
		Select("channel_id, AVG(stars) * 2.0 AS average_score, COUNT(id) AS rating_count").
		Group("channel_id")
}

// AuthorizeCommunityService validates the dedicated server-to-server secret.
// It deliberately does not reuse the OIDC client secret.
func AuthorizeCommunityService(candidate string) error {
	expected := strings.TrimSpace(os.Getenv(CommunityAPISecretEnvironment))
	if len(expected) < 32 {
		return ErrCommunityAPIDisabled
	}
	if subtle.ConstantTimeCompare([]byte(candidate), []byte(expected)) != 1 {
		return ErrCommunityAPIUnauthorized
	}
	return nil
}

func GetCommunityMember(subject string) (*CommunityMember, error) {
	subject, err := normalizeCommunitySubject(subject)
	if err != nil {
		return nil, err
	}
	user, err := loadCommunityMember(subject)
	if err != nil {
		return nil, err
	}
	member := &CommunityMember{Subject: subject, Active: user.Status == constant.UserStatusEnabled}
	if !member.Active {
		return member, nil
	}
	member.Username, member.DisplayName = publicCommunityIdentity(subject, user.Username, user.DisplayName)
	member.VerifiedChannelOwner, err = hasVerifiedCommunityChannel(user.Id)
	if err != nil {
		return nil, err
	}
	if member.VerifiedChannelOwner {
		summary, summaryErr := loadCommunityOwnerRatingSummary(user.Id)
		if summaryErr != nil {
			return nil, summaryErr
		}
		member.AverageScore = summary.AverageScore
		member.RatingCount = summary.RatingCount
	}
	return member, nil
}

func publicCommunityIdentity(subject, username, displayName string) (string, string) {
	username = strings.TrimSpace(username)
	displayName = strings.TrimSpace(displayName)
	publicUsername := username
	if sensitiveCommunityIdentity(username) {
		publicUsername = "codego-" + strings.ToLower(subject)
	}
	if displayName == "" {
		displayName = username
	}
	if sensitiveCommunityIdentity(displayName) {
		displayName = "渠道主 " + subject
	}
	return publicUsername, displayName
}

func sensitiveCommunityIdentity(value string) bool {
	value = strings.TrimSpace(value)
	return strings.Contains(value, "@") || communityPhoneIdentityPattern.MatchString(value)
}

func ListCommunityMemberChannels(subject string, page, pageSize int, keyword, sort, viewerSubject string) (*CommunityChannelList, error) {
	keyword = strings.TrimSpace(keyword)
	sort = strings.TrimSpace(sort)
	viewerSubject = strings.TrimSpace(viewerSubject)
	if sort == "" {
		sort = "rating"
	}
	if page < 1 || page > 10000 || pageSize < 1 || pageSize > 50 || len([]rune(keyword)) > 64 || !validCommunityChannelSort(sort) {
		return nil, ErrInvalidCommunityQuery
	}
	subject, err := normalizeCommunitySubject(subject)
	if err != nil {
		return nil, err
	}
	user, err := loadCommunityMember(subject)
	if err != nil {
		return nil, err
	}
	if user.Status != constant.UserStatusEnabled {
		return nil, ErrCommunityMemberInactive
	}

	viewerUserID := 0
	if viewerSubject != "" {
		viewerSubject, err = normalizeCommunitySubject(viewerSubject)
		if err != nil {
			return nil, err
		}
		viewer, viewerErr := loadCommunityMember(viewerSubject)
		if viewerErr != nil {
			return nil, viewerErr
		}
		if viewer.Status != constant.UserStatusEnabled {
			return nil, ErrCommunityMemberInactive
		}
		viewerUserID = viewer.Id
	}

	query := communityMemberChannelQuery(user.Id, keyword)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	items := make([]CommunityChannel, 0)
	if total > 0 {
		query = communityMemberChannelQuery(user.Id, keyword).
			Joins("LEFT JOIN (?) AS community_rating_stats ON community_rating_stats.channel_id = community_channels.id", communityChannelRatingStatsQuery())
		selectFields := strings.Join([]string{
			"community_channels.id AS id",
			"community_groups.public_slug AS slug",
			"community_groups.system_display_name AS name",
			"community_channels.provider_type AS provider",
			"community_groups.lifecycle_status AS lifecycle_status",
			"community_groups.verification_status AS verification_status",
			"COALESCE(community_rating_stats.average_score, 0) AS average_score",
			"COALESCE(community_rating_stats.rating_count, 0) AS rating_count",
		}, ", ")
		if err := query.Select(selectFields).
			Scopes(communityChannelSortScope(sort)).
			Limit(pageSize).Offset((page - 1) * pageSize).
			Scan(&items).Error; err != nil {
			return nil, err
		}
		if viewerUserID > 0 {
			channelIDs := make([]string, 0, len(items))
			for _, item := range items {
				channelIDs = append(channelIDs, item.ID)
			}
			var viewerRatings []communityschema.ChannelRating
			if err := platformdb.DB.Select("channel_id", "stars").
				Where("channel_id IN ? AND user_id = ?", channelIDs, viewerUserID).
				Find(&viewerRatings).Error; err != nil {
				return nil, err
			}
			byChannel := make(map[string]int, len(viewerRatings))
			for _, rating := range viewerRatings {
				byChannel[rating.ChannelID] = rating.Stars
			}
			for index := range items {
				items[index].ViewerStars = byChannel[items[index].ID]
			}
		}
	}
	return &CommunityChannelList{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func communityMemberChannelQuery(ownerUserID int, keyword string) *gorm.DB {
	query := eligibleCommunityChannelQuery(ownerUserID)
	if keyword != "" {
		escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(keyword))
		term := "%" + escaped + "%"
		query = query.Where("(LOWER(community_groups.system_display_name) LIKE ? ESCAPE '!' OR LOWER(community_groups.public_slug) LIKE ? ESCAPE '!')", term, term)
	}
	return query
}

func validCommunityChannelSort(sort string) bool {
	switch sort {
	case "rating", "recent", "name":
		return true
	default:
		return false
	}
}

func communityChannelSortScope(sort string) func(*gorm.DB) *gorm.DB {
	return func(query *gorm.DB) *gorm.DB {
		switch sort {
		case "recent":
			return query.Order("community_groups.updated_at DESC").Order("community_channels.id ASC")
		case "name":
			return query.Order("community_groups.system_display_name ASC").Order("community_channels.id ASC")
		default:
			return query.Order("CASE WHEN COALESCE(community_rating_stats.rating_count, 0) > 0 THEN 0 ELSE 1 END ASC").
				Order("COALESCE(community_rating_stats.average_score, 0) DESC").
				Order("COALESCE(community_rating_stats.rating_count, 0) DESC").
				Order("community_groups.updated_at DESC").
				Order("community_channels.id ASC")
		}
	}
}

// RateCommunityChannel updates one user's rating for an eligible public channel.
// One star maps to two points, so the exposed average score is always on a 2-10 scale.
func RateCommunityChannel(request CommunityRatingRequest, channelID string) (*CommunityRatingResult, error) {
	channelID = strings.TrimSpace(channelID)
	if request.Stars < 1 || request.Stars > 5 || channelID == "" || len(channelID) > 64 {
		return nil, ErrInvalidCommunityRating
	}
	viewerSubject, err := normalizeCommunitySubject(request.ViewerSubject)
	if err != nil {
		return nil, err
	}
	viewer, err := loadCommunityMember(viewerSubject)
	if err != nil {
		return nil, err
	}
	if viewer.Status != constant.UserStatusEnabled {
		return nil, ErrCommunityMemberInactive
	}

	type channelOwnerRow struct {
		OwnerUserID int `gorm:"column:owner_user_id"`
	}
	var channel channelOwnerRow
	err = publicCommunityChannelQuery().
		Select("community_groups.owner_user_id AS owner_user_id").
		Where("community_channels.id = ?", channelID).
		Take(&channel).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCommunityChannelNotFound
	}
	if err != nil {
		return nil, err
	}
	if channel.OwnerUserID == viewer.Id {
		return nil, ErrCommunitySelfRating
	}

	rating := communityschema.ChannelRating{ChannelID: channelID, UserID: viewer.Id, Stars: request.Stars}
	if err := platformdb.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"stars", "updated_at"}),
	}).Create(&rating).Error; err != nil {
		return nil, err
	}
	channelSummary, err := loadCommunityChannelRatingSummary(channelID, viewer.Id)
	if err != nil {
		return nil, err
	}
	sellerSummary, err := loadCommunityOwnerRatingSummary(channel.OwnerUserID)
	if err != nil {
		return nil, err
	}
	return &CommunityRatingResult{Channel: channelSummary, Seller: sellerSummary}, nil
}

func loadCommunityChannelRatingSummary(channelID string, viewerUserID int) (CommunityRatingSummary, error) {
	var summary CommunityRatingSummary
	if err := platformdb.DB.Model(&communityschema.ChannelRating{}).
		Select("COALESCE(AVG(stars) * 2.0, 0) AS average_score, COUNT(id) AS rating_count").
		Where("channel_id = ?", channelID).Scan(&summary).Error; err != nil {
		return summary, err
	}
	if viewerUserID > 0 {
		var rating communityschema.ChannelRating
		err := platformdb.DB.Select("stars").Where("channel_id = ? AND user_id = ?", channelID, viewerUserID).Take(&rating).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return summary, err
		}
		if err == nil {
			summary.ViewerStars = rating.Stars
		}
	}
	return summary, nil
}

func loadCommunityOwnerRatingSummary(ownerUserID int) (CommunityRatingSummary, error) {
	var summary CommunityRatingSummary
	err := eligibleCommunityChannelQuery(ownerUserID).
		Joins("LEFT JOIN " + communityschema.ChannelRating{}.TableName() + " AS community_ratings ON community_ratings.channel_id = community_channels.id").
		Select("COALESCE(AVG(community_ratings.stars) * 2.0, 0) AS average_score, COUNT(community_ratings.id) AS rating_count").
		Scan(&summary).Error
	return summary, err
}

func loadCommunityMember(subject string) (*identityschema.User, error) {
	var user identityschema.User
	err := platformdb.DB.Select("id", "external_id", "username", "display_name", "status").
		Where("external_id = ?", subject).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCommunityMemberNotFound
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func hasVerifiedCommunityChannel(ownerUserID int) (bool, error) {
	var count int64
	if err := eligibleCommunityChannelQuery(ownerUserID).Limit(1).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func eligibleCommunityChannelQuery(ownerUserID int) *gorm.DB {
	return publicCommunityChannelQuery().Where("community_groups.owner_user_id = ?", ownerUserID)
}

func publicCommunityChannelQuery() *gorm.DB {
	groupTable := marketplaceschema.Group{}.TableName()
	channelTable := marketplaceschema.Channel{}.TableName()
	return platformdb.DB.Table(groupTable+" AS community_groups").
		Joins("JOIN "+channelTable+" AS community_channels ON community_channels.id = community_groups.channel_id").
		Where("community_channels.owner_user_id = community_groups.owner_user_id").
		Where("community_groups.deleted_at IS NULL AND community_channels.deleted_at IS NULL").
		Where("community_groups.visibility = ?", marketplacedomain.VisibilityPublic).
		Where("community_groups.verification_status = ?", marketplacedomain.VerificationPassed).
		Where("community_groups.lifecycle_status IN ?", []string{marketplacedomain.LifecycleActive, marketplacedomain.LifecycleDegraded})
}

func normalizeCommunitySubject(subject string) (string, error) {
	value := strings.TrimSpace(subject)
	if len(value) != identityschema.ExternalUserIDLength {
		return "", ErrInvalidCommunitySubject
	}
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	for _, character := range value {
		if !strings.ContainsRune(alphabet, character) {
			return "", ErrInvalidCommunitySubject
		}
	}
	return value, nil
}
