package app

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ShopProfileRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type ShopView struct {
	ID               int      `json:"id"`
	Name             string   `json:"name"`
	CustomName       string   `json:"custom_name"`
	Description      string   `json:"description"`
	ChannelCount     int      `json:"channel_count"`
	MinMultiplier    float64  `json:"min_multiplier"`
	Models           []string `json:"models"`
	Tags             []string `json:"tags"`
	Rating           float64  `json:"rating"`
	RatingCount      int64    `json:"rating_count"`
	ViewerStars      int      `json:"viewer_stars"`
	CanRate          bool     `json:"can_rate"`
	RatingPermission string   `json:"rating_permission"`
	ModerationStatus string   `json:"moderation_status"`
}

type ShopList struct {
	Items    []ShopView `json:"items"`
	Total    int        `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

func defaultShopView(ownerID int) ShopView {
	return ShopView{ID: ownerID, Name: fmt.Sprintf("店铺 #%d", ownerID), Models: []string{}, Tags: []string{}, RatingPermission: "usage_required"}
}

func loadShopViews(ownerIDs []int, viewerID int) (map[int]ShopView, error) {
	result := make(map[int]ShopView, len(ownerIDs))
	if len(ownerIDs) == 0 {
		return result, nil
	}
	for _, id := range ownerIDs {
		result[id] = defaultShopView(id)
	}
	var profiles []marketplaceschema.Shop
	if err := platformdb.DB.Where("owner_user_id IN ?", ownerIDs).Find(&profiles).Error; err != nil {
		return nil, err
	}
	for _, profile := range profiles {
		view := result[profile.OwnerUserID]
		if profile.ModerationStatus == "failed" {
			continue
		}
		if name := safePublicText(profile.Name, 40, true); name != "" {
			view.Name = name
			view.CustomName = name
		}
		view.Description = safePublicText(profile.Description, 300, false)
		result[view.ID] = view
	}
	var ratings []struct {
		OwnerUserID int
		Rating      float64
		RatingCount int64
	}
	if err := platformdb.DB.Model(&marketplaceschema.ShopRating{}).Select("owner_user_id, AVG(stars) AS rating, COUNT(*) AS rating_count").Where("owner_user_id IN ?", ownerIDs).Group("owner_user_id").Scan(&ratings).Error; err != nil {
		return nil, err
	}
	for _, rating := range ratings {
		view := result[rating.OwnerUserID]
		view.Rating, view.RatingCount = rating.Rating, rating.RatingCount
		result[view.ID] = view
	}
	if viewerID > 0 {
		var eligible []int
		if err := platformdb.DB.Model(&marketplaceschema.Settlement{}).Distinct("owner_user_id").Where("owner_user_id IN ? AND consumer_user_id = ? AND consumer_amount > 0", ownerIDs, viewerID).Pluck("owner_user_id", &eligible).Error; err != nil {
			return nil, err
		}
		for _, id := range eligible {
			view := result[id]
			view.CanRate = true
			view.RatingPermission = "allowed"
			result[id] = view
		}
		var ownRatings []marketplaceschema.ShopRating
		if err := platformdb.DB.Where("owner_user_id IN ? AND user_id = ?", ownerIDs, viewerID).Find(&ownRatings).Error; err != nil {
			return nil, err
		}
		for _, rating := range ownRatings {
			view := result[rating.OwnerUserID]
			view.ViewerStars = rating.Stars
			result[view.ID] = view
		}
	}
	for id, view := range result {
		if viewerID <= 0 {
			view.CanRate = false
			view.RatingPermission = "login_required"
		}
		if viewerID == id {
			view.CanRate = false
			view.RatingPermission = "owner"
		}
		result[id] = view
	}
	return result, nil
}

func ListMarketplaceShops(query GroupQuery) (*ShopList, error) {
	query = normalizeGroupQuery(query)
	// Shops only aggregate publicly discoverable products, even for owners.
	publicQuery := query
	publicQuery.ViewerUserID = 0
	publicQuery.IncludeAccess = false
	publicQuery.Search = ""
	groups, err := loadPublicGroupRows(publicQuery)
	if err != nil {
		return nil, err
	}
	channels, err := marketplaceChannelReadMap(groups, false)
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0)
	seen := map[int]bool{}
	for _, group := range groups {
		if _, exists := channels[group.ChannelID]; !exists {
			continue
		}
		if !seen[group.OwnerUserID] {
			ids = append(ids, group.OwnerUserID)
			seen[group.OwnerUserID] = true
		}
	}
	views, err := loadShopViews(ids, query.ViewerUserID)
	if err != nil {
		return nil, err
	}
	for _, group := range groups {
		view := views[group.OwnerUserID]
		channel, exists := channels[group.ChannelID]
		if !exists {
			continue
		}
		models := decodeModels(channel.DeclaredModels)
		matchQuery := query
		matchQuery.Search = ""
		search := strings.ToLower(strings.TrimSpace(query.Search))
		if search != "" && search != strconv.Itoa(view.ID) && !strings.Contains(strings.ToLower(view.Name), search) && !strings.Contains(strings.ToLower(view.Description), search) {
			continue
		}
		if !matchesGroupQuery(group, channel, models, matchQuery) {
			continue
		}
		view.ChannelCount++
		if view.MinMultiplier == 0 || group.Multiplier < view.MinMultiplier {
			view.MinMultiplier = group.Multiplier
		}
		view.Models = append(view.Models, models...)
		view.Tags = append(view.Tags, decodeModels(group.Tags)...)
		views[view.ID] = view
	}
	items := make([]ShopView, 0, len(views))
	for _, view := range views {
		if view.ChannelCount == 0 {
			continue
		}
		view.Models = normalizeModels(view.Models)
		view.Tags = normalizeModels(view.Tags)
		items = append(items, view)
	}
	sort.Slice(items, func(i, j int) bool {
		if query.Sort == "multiplier" && items[i].MinMultiplier != items[j].MinMultiplier {
			if query.Direction == "asc" {
				return items[i].MinMultiplier < items[j].MinMultiplier
			}
			return items[i].MinMultiplier > items[j].MinMultiplier
		}
		if items[i].Rating != items[j].Rating {
			return items[i].Rating > items[j].Rating
		}
		if items[i].RatingCount != items[j].RatingCount {
			return items[i].RatingCount > items[j].RatingCount
		}
		return items[i].ID < items[j].ID
	})
	total := len(items)
	start := total
	if query.Page-1 <= total/query.PageSize {
		start = (query.Page - 1) * query.PageSize
	}
	end := min(start+query.PageSize, total)
	return &ShopList{Items: items[start:end], Total: total, Page: query.Page, PageSize: query.PageSize}, nil
}

func GetOwnerShop(ownerID int) (*ShopView, error) {
	if ownerID <= 0 {
		return nil, errors.New("用户未登录")
	}
	views, err := loadShopViews([]int{ownerID}, ownerID)
	if err != nil {
		return nil, err
	}
	view := views[ownerID]
	var profile marketplaceschema.Shop
	if err := platformdb.DB.Where("owner_user_id = ?", ownerID).First(&profile).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	view.CustomName, view.Description, view.ModerationStatus = profile.Name, profile.Description, profile.ModerationStatus
	return &view, nil
}

func UpdateOwnerShop(ownerID int, req ShopProfileRequest) (*ShopView, error) {
	if ownerID <= 0 {
		return nil, errors.New("用户未登录")
	}
	name, err := normalizePublicText(req.Name, 20, true)
	if err != nil {
		return nil, err
	}
	description, err := normalizePublicText(req.Description, 300, false)
	if err != nil {
		return nil, err
	}
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var existing marketplaceschema.Shop
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("owner_user_id = ?", ownerID).First(&existing).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if existing.OwnerUserID > 0 && existing.Name == name && existing.Description == description {
			return nil
		}
		profile := marketplaceschema.Shop{OwnerUserID: ownerID, Name: name, Description: description, ModerationStatus: "pending"}
		profile.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
		if !profile.UpdatedAt.After(existing.UpdatedAt) {
			profile.UpdatedAt = existing.UpdatedAt.Add(time.Microsecond)
		}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "owner_user_id"}}, DoUpdates: clause.AssignmentColumns([]string{"name", "description", "moderation_status", "updated_at"})}).Create(&profile).Error; err != nil {
			return err
		}
		// Shared public content changed. Reopen review for this owner's groups
		// without changing their service lifecycle, prices or connectivity data.
		// Advancing the revision also prevents an old probe from writing back.
		return tx.Model(&marketplaceschema.Group{}).Where("owner_user_id = ?", ownerID).Updates(map[string]any{
			"moderation_status": "pending", "moderation_reason": "", "moderation_checked_at": nil, "updated_at": profile.UpdatedAt,
		}).Error
	}); err != nil {
		return nil, err
	}
	return GetOwnerShop(ownerID)
}

func RateMarketplaceShop(userID, ownerID, stars int) (*ShopView, error) {
	if userID <= 0 || ownerID <= 0 || userID == ownerID {
		return nil, errors.New("不能给自己的店铺评分，请先登录并使用其他店铺的渠道")
	}
	if stars < 1 || stars > 5 {
		return nil, errors.New("评分必须是 1 到 5 星的整数")
	}
	var count int64
	if err := publicGroupsQuery(GroupQuery{}).Where("owner_user_id = ?", ownerID).Count(&count).Error; err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	views, err := loadShopViews([]int{ownerID}, userID)
	if err != nil {
		return nil, err
	}
	if !views[ownerID].CanRate {
		return nil, errors.New("实际使用过该店铺渠道后才能评分")
	}
	rating := marketplaceschema.ShopRating{OwnerUserID: ownerID, UserID: userID, Stars: stars}
	if err := platformdb.DB.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "owner_user_id"}, {Name: "user_id"}}, DoUpdates: clause.AssignmentColumns([]string{"stars", "updated_at"})}).Create(&rating).Error; err != nil {
		return nil, err
	}
	views, err = loadShopViews([]int{ownerID}, userID)
	if err != nil {
		return nil, err
	}
	view := views[ownerID]
	return &view, nil
}

func attachShopPresentation(groups []marketplaceschema.Group) (map[int]ShopView, error) {
	ids := make([]int, 0, len(groups))
	seen := map[int]bool{}
	for _, group := range groups {
		if !seen[group.OwnerUserID] {
			ids = append(ids, group.OwnerUserID)
			seen[group.OwnerUserID] = true
		}
	}
	// Ratings are loaded by the shop listing. Product filtering only needs public names.
	views := make(map[int]ShopView, len(ids))
	for _, id := range ids {
		views[id] = defaultShopView(id)
	}
	if len(ids) == 0 || platformdb.DB == nil {
		return views, nil
	}
	var profiles []marketplaceschema.Shop
	if err := platformdb.DB.Where("owner_user_id IN ?", ids).Find(&profiles).Error; err != nil {
		return nil, err
	}
	for _, profile := range profiles {
		view := views[profile.OwnerUserID]
		if profile.ModerationStatus == "failed" {
			continue
		}
		if name := safePublicText(profile.Name, 40, true); name != "" {
			view.Name = name
		}
		views[view.ID] = view
	}
	return views, nil
}
