package app

import (
	"fmt"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"math"
	"strconv"
	"time"
)

func UserUsageTimeSeries(owner int, channelID, userID string, rangeHours int) (map[string]any, error) {
	uid, err := strconv.Atoi(userID)
	if err != nil || uid <= 0 {
		return nil, fmt.Errorf("invalid user id")
	}
	var c marketplaceschema.Channel
	if err := platformdb.DB.Where("id=? AND owner_user_id=?", channelID, owner).First(&c).Error; err != nil {
		return nil, err
	}
	start := time.Now().Add(-time.Duration(rangeHours) * time.Hour).Unix()
	var logs []auditschema.Log
	if err := platformdb.LogDB.Select("created_at,type,prompt_tokens,completion_tokens").Where("user_id=? AND channel_id=? AND created_at>=?", uid, c.InternalChannelID, start).Find(&logs).Error; err != nil {
		return nil, err
	}
	bucket := int64(3600)
	points := map[int64]map[string]int64{}
	for _, l := range logs {
		ts := (l.CreatedAt / bucket) * bucket
		p := points[ts]
		if p == nil {
			p = map[string]int64{}
			points[ts] = p
		}
		p["request_count"]++
		if l.Type == auditschema.LogTypeConsume {
			p["success_count"]++
			p["token_count"] += int64(l.PromptTokens + l.CompletionTokens)
		}
	}
	result := make([]map[string]any, 0, len(points))
	for ts, p := range points {
		result = append(result, map[string]any{"timestamp": ts, "request_count": p["request_count"], "success_count": p["success_count"], "token_count": p["token_count"]})
	}
	return map[string]any{"user_id": userID, "channel_id": channelID, "bucket_seconds": bucket, "points": result}, nil
}

type OwnerUserUsageQuery struct {
	ChannelID, Search, Sort, Direction string
	StartTimestamp, EndTimestamp       int64
	Page, PageSize                     int
}

func EnsureOwnerChannel(owner int, channelID string) error {
	var c marketplaceschema.Channel
	return platformdb.DB.Where("id = ? AND owner_user_id = ?", channelID, owner).First(&c).Error
}

type OwnerUserUsageItem struct {
	UserID                                  string    `json:"user_id"`
	ExternalUserID                          string    `json:"external_user_id"`
	ChannelID                               string    `json:"channel_id"`
	ChannelName                             string    `json:"channel_name"`
	GroupID                                 string    `json:"group_id"`
	RequestCount, SuccessCount, FailedCount int64     `json:"request_count"`
	SuccessRate                             float64   `json:"success_rate"`
	TotalTokens                             int64     `json:"total_tokens"`
	AvgLatencyMs, AvgTTFTMs                 float64   `json:"avg_latency_ms"`
	TotalConsumerAmount, TotalOwnerIncome   int64     `json:"total_consumer_amount"`
	UserMultiplier                          *float64  `json:"user_multiplier"`
	LastRequestAt                           time.Time `json:"last_request_at"`
}

func ListOwnerChannelUserUsage(owner int, q OwnerUserUsageQuery) (map[string]any, error) {
	var groups []marketplaceschema.Group
	db := platformdb.DB.Where("owner_user_id = ?", owner)
	if q.ChannelID != "" {
		db = db.Where("channel_id = ?", q.ChannelID)
	}
	if err := db.Find(&groups).Error; err != nil {
		return nil, err
	}
	ids := make([]string, len(groups))
	groupsByID := make(map[string]marketplaceschema.Group, len(groups))
	for i := range groups {
		ids[i] = groups[i].ID
		groupsByID[groups[i].ID] = groups[i]
	}
	if len(ids) == 0 {
		return map[string]any{"items": []OwnerUserUsageItem{}, "total": 0, "page": q.Page, "page_size": q.PageSize, "summary": map[string]any{"total_users": 0}}, nil
	}
	var rows []struct {
		UserID  int
		GroupID string
		Cnt     int64
		Amount  int64
		Last    time.Time
	}
	if err := platformdb.DB.Model(&marketplaceschema.Settlement{}).
		Select("consumer_user_id as user_id, group_id, count(*) as cnt, sum(consumer_amount) as amount, max(created_at) as last").
		Where("group_id IN ?", ids).Group("consumer_user_id,group_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	userIDs := make([]int, 0, len(rows))
	for _, row := range rows {
		userIDs = append(userIDs, row.UserID)
	}
	users := make([]identityschema.User, 0, len(userIDs))
	if len(userIDs) > 0 {
		if err := platformdb.DB.Select("id,external_id").Where("id IN ?", userIDs).Find(&users).Error; err != nil {
			return nil, err
		}
	}
	externalIDs := make(map[int]string, len(users))
	for _, user := range users {
		externalIDs[user.Id] = user.ExternalId
	}
	items := make([]OwnerUserUsageItem, 0, len(rows))
	for _, r := range rows {
		group := groupsByID[r.GroupID]
		items = append(items, OwnerUserUsageItem{UserID: strconv.Itoa(r.UserID), ExternalUserID: externalIDs[r.UserID], ChannelID: group.ChannelID, ChannelName: group.SystemDisplayName, GroupID: r.GroupID, RequestCount: r.Cnt, SuccessCount: r.Cnt, SuccessRate: 1, TotalConsumerAmount: r.Amount, LastRequestAt: r.Last})
	}
	return map[string]any{"items": items, "total": len(items), "page": q.Page, "page_size": q.PageSize, "summary": map[string]any{"total_users": len(items), "total_requests": len(items)}}, nil
}
func SetUserMultiplier(owner int, channel string, user int, m *float64) error {
	var c marketplaceschema.Channel
	if err := platformdb.DB.Where("id=? AND owner_user_id=?", channel, owner).First(&c).Error; err != nil {
		return err
	}
	if m == nil {
		return platformdb.DB.Where("channel_id=? AND user_id=?", channel, user).Delete(&marketplaceschema.UserMultiplier{}).Error
	}
	return platformdb.DB.Where("channel_id=? AND user_id=?", channel, user).Assign(map[string]any{"multiplier": *m}).FirstOrCreate(&marketplaceschema.UserMultiplier{ChannelID: channel, UserID: user}).Error
}
func ListTimeRangeMultipliers(owner int, channel string) ([]marketplaceschema.TimeRangeMultiplier, error) {
	var c marketplaceschema.Channel
	if err := platformdb.DB.Where("id=? AND owner_user_id=?", channel, owner).First(&c).Error; err != nil {
		return nil, err
	}
	var r []marketplaceschema.TimeRangeMultiplier
	return r, platformdb.DB.Where("channel_id=?", channel).Order("start_timestamp").Find(&r).Error
}
func CreateTimeRangeMultiplier(owner int, channel string, start, end int64, m float64, label string) (marketplaceschema.TimeRangeMultiplier, error) {
	var c marketplaceschema.Channel
	if err := platformdb.DB.Where("id=? AND owner_user_id=?", channel, owner).First(&c).Error; err != nil {
		return marketplaceschema.TimeRangeMultiplier{}, err
	}
	r := marketplaceschema.TimeRangeMultiplier{ID: platformruntime.GetUUID(), ChannelID: channel, StartTimestamp: start, EndTimestamp: end, Multiplier: m, Label: label}
	return r, platformdb.DB.Create(&r).Error
}
func DeleteTimeRangeMultiplier(owner int, channel, id string) error {
	var c marketplaceschema.Channel
	if err := platformdb.DB.Where("id=? AND owner_user_id=?", channel, owner).First(&c).Error; err != nil {
		return err
	}
	return platformdb.DB.Where("id=? AND channel_id=?", id, channel).Delete(&marketplaceschema.TimeRangeMultiplier{}).Error
}
func CreateBargainRequest(user int, group string, m float64, reason string) (marketplaceschema.BargainRequest, error) {
	if m <= 0 || math.IsNaN(m) || math.IsInf(m, 0) {
		return marketplaceschema.BargainRequest{}, fmt.Errorf("proposed multiplier must be a finite positive number")
	}
	var g marketplaceschema.Group
	if err := platformdb.DB.First(&g, "id = ?", group).Error; err != nil {
		return marketplaceschema.BargainRequest{}, err
	}
	var pending int64
	if err := platformdb.DB.Model(&marketplaceschema.BargainRequest{}).
		Where("group_id=? AND user_id=? AND status=?", group, user, "pending").Count(&pending).Error; err != nil {
		return marketplaceschema.BargainRequest{}, err
	}
	if pending > 0 {
		return marketplaceschema.BargainRequest{}, fmt.Errorf("a bargain request is already pending for this group")
	}
	r := marketplaceschema.BargainRequest{ID: platformruntime.GetUUID(), GroupID: group, UserID: user, CurrentMultiplier: g.Multiplier, ProposedMultiplier: m, Status: "pending", Reason: reason}
	return r, platformdb.DB.Create(&r).Error
}

type BargainRequestItem struct {
	marketplaceschema.BargainRequest
	GroupName      string `json:"group_name"`
	UserExternalID string `json:"user_external_id"`
}

func ListOwnerBargainRequests(owner int, q map[string]string, page, size int) (map[string]any, error) {
	return listBargainRequests(q, page, size, &owner)
}

func listBargainRequests(q map[string]string, page, size int, owner *int) (map[string]any, error) {
	var rs []marketplaceschema.BargainRequest
	db := platformdb.DB.Order("created_at desc")
	if owner != nil {
		var groups []marketplaceschema.Group
		if err := platformdb.DB.Where("owner_user_id=?", *owner).Find(&groups).Error; err != nil {
			return nil, err
		}
		ids := make([]string, len(groups))
		for i := range groups {
			ids[i] = groups[i].ID
		}
		if len(ids) == 0 {
			return map[string]any{"items": []BargainRequestItem{}, "total": 0, "page": page, "page_size": size}, nil
		}
		db = db.Where("group_id IN ?", ids)
	}
	if q["status"] != "" {
		db = db.Where("status=?", q["status"])
	}
	if q["group_id"] != "" {
		db = db.Where("group_id=?", q["group_id"])
	}
	var total int64
	db.Model(&marketplaceschema.BargainRequest{}).Count(&total)
	if err := db.Offset((page - 1) * size).Limit(size).Find(&rs).Error; err != nil {
		return nil, err
	}
	groupIDs, userIDs := make([]string, 0, len(rs)), make([]int, 0, len(rs))
	for _, r := range rs {
		groupIDs = append(groupIDs, r.GroupID)
		userIDs = append(userIDs, r.UserID)
	}
	var groups []marketplaceschema.Group
	var users []identityschema.User
	platformdb.DB.Where("id IN ?", groupIDs).Find(&groups)
	platformdb.DB.Where("id IN ?", userIDs).Find(&users)
	groupNames, userExternalIDs := map[string]string{}, map[int]string{}
	for _, group := range groups {
		groupNames[group.ID] = group.SystemDisplayName
	}
	for _, user := range users {
		userExternalIDs[user.Id] = user.ExternalId
	}
	items := make([]BargainRequestItem, 0, len(rs))
	for _, r := range rs {
		items = append(items, BargainRequestItem{BargainRequest: r, GroupName: groupNames[r.GroupID], UserExternalID: userExternalIDs[r.UserID]})
	}
	return map[string]any{"items": items, "total": total, "page": page, "page_size": size}, nil
}
func ResolveOwnerBargainRequest(owner int, id, action, note string) (marketplaceschema.BargainRequest, error) {
	return resolveBargainRequest(id, action, note, &owner)
}
func resolveBargainRequest(id, action, note string, owner *int) (marketplaceschema.BargainRequest, error) {
	var r marketplaceschema.BargainRequest
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&r, "id=?", id).Error; err != nil {
			return err
		}
		if r.Status != "pending" {
			return fmt.Errorf("request already resolved")
		}
		var group marketplaceschema.Group
		if err := tx.First(&group, "id=?", r.GroupID).Error; err != nil {
			return err
		}
		if owner != nil && group.OwnerUserID != *owner {
			return fmt.Errorf("group does not belong to current owner")
		}
		now := time.Now()
		if action == "approve" {
			r.Status = "approved"
			if err := tx.Where("channel_id=? AND user_id=?", group.ChannelID, r.UserID).Assign(map[string]any{"multiplier": r.ProposedMultiplier}).FirstOrCreate(&marketplaceschema.UserMultiplier{ChannelID: group.ChannelID, UserID: r.UserID}).Error; err != nil {
				return err
			}
		} else if action == "reject" {
			r.Status = "rejected"
		} else {
			return fmt.Errorf("invalid action")
		}
		r.ResolutionNote, r.ResolvedAt = note, &now
		return tx.Save(&r).Error
	})
	return r, err
}

var _ = gorm.ErrRecordNotFound
