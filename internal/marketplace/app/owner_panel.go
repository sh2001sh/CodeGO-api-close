package app

import (
	"fmt"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math"
	"sort"
	"strconv"
	"strings"
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
	UserID              string  `json:"user_id"`
	ExternalUserID      string  `json:"external_user_id"`
	ChannelID           string  `json:"channel_id"`
	ChannelName         string  `json:"channel_name"`
	GroupID             string  `json:"group_id"`
	RequestCount        int64   `json:"request_count"`
	SuccessCount        int64   `json:"success_count"`
	FailedCount         int64   `json:"failed_count"`
	SuccessRate         float64 `json:"success_rate"`
	TotalTokens         int64   `json:"total_tokens"`
	AvgLatencyMs        float64 `json:"avg_latency_ms"`
	AvgTTFTMs           float64 `json:"avg_ttft_ms"`
	TotalConsumerAmount int64   `json:"total_consumer_amount"`
	// Gross settlement uses the historical wallet multiplier for both billing sources.
	TotalSettlementGrossAmount int64     `json:"total_settlement_gross_amount"`
	TotalOwnerIncome           int64     `json:"total_owner_income"`
	UserMultiplier             *float64  `json:"user_multiplier"`
	LastRequestAt              time.Time `json:"last_request_at"`
}

func ListOwnerChannelUserUsage(owner int, q OwnerUserUsageQuery) (map[string]any, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 {
		q.PageSize = 50
	}
	if q.PageSize > 200 {
		q.PageSize = 200
	}
	if q.StartTimestamp > 0 && q.EndTimestamp > 0 && q.StartTimestamp > q.EndTimestamp {
		q.StartTimestamp, q.EndTimestamp = q.EndTimestamp, q.StartTimestamp
	}
	var groups []marketplaceschema.Group
	db := platformdb.DB.Unscoped().Where("owner_user_id = ?", owner)
	if q.ChannelID != "" {
		db = db.Where("channel_id = ?", q.ChannelID)
	}
	if err := db.Select("id,channel_id,system_display_name").Find(&groups).Error; err != nil {
		return nil, err
	}
	ids := make([]string, len(groups))
	groupsByID := make(map[string]marketplaceschema.Group, len(groups))
	for i := range groups {
		ids[i] = groups[i].ID
		groupsByID[groups[i].ID] = groups[i]
	}
	if len(ids) == 0 {
		return map[string]any{"items": []OwnerUserUsageItem{}, "total": 0, "page": q.Page, "page_size": q.PageSize, "summary": map[string]any{"total_users": 0, "total_requests": int64(0), "consumer_amount": int64(0), "owner_income": int64(0)}}, nil
	}
	var rows []struct {
		UserID   int
		GroupID  string
		Cnt      int64
		Amount   int64
		Gross    int64
		Income   int64
		Last     time.Time
		LastUnix int64
	}
	lastRequestSelect := "max(created_at) as last"
	if platformdb.DB.Dialector.Name() == "sqlite" {
		// SQLite loses the datetime column type inside MAX().
		lastRequestSelect = "cast(strftime('%s', max(created_at)) as integer) as last_unix"
	}
	settlementDB := platformdb.DB.Model(&marketplaceschema.Settlement{}).
		Select("consumer_user_id as user_id, group_id, count(*) as cnt, sum(consumer_amount) as amount, sum(settlement_gross_amount) as gross, sum(owner_net_amount) as income, "+lastRequestSelect).
		Where("owner_user_id = ? AND group_id IN ?", owner, ids)
	if q.StartTimestamp > 0 {
		settlementDB = settlementDB.Where("created_at >= ?", time.Unix(q.StartTimestamp, 0))
	}
	if q.EndTimestamp > 0 {
		settlementDB = settlementDB.Where("created_at < ?", time.Unix(q.EndTimestamp+1, 0))
	}
	if err := settlementDB.Group("consumer_user_id,group_id").Scan(&rows).Error; err != nil {
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
	uniqueUsers := make(map[int]struct{}, len(rows))
	var totalRequests, totalConsumerAmount, totalOwnerIncome int64
	for _, r := range rows {
		group := groupsByID[r.GroupID]
		if platformdb.DB.Dialector.Name() == "sqlite" {
			r.Last = time.Unix(r.LastUnix, 0)
		}
		item := OwnerUserUsageItem{UserID: strconv.Itoa(r.UserID), ExternalUserID: externalIDs[r.UserID], ChannelID: group.ChannelID, ChannelName: group.SystemDisplayName, GroupID: r.GroupID, RequestCount: r.Cnt, SuccessCount: r.Cnt, SuccessRate: 1, TotalConsumerAmount: r.Amount, TotalSettlementGrossAmount: r.Gross, TotalOwnerIncome: r.Income, LastRequestAt: r.Last}
		if q.Search != "" && !strings.Contains(strings.ToLower(item.ExternalUserID+" "+item.UserID+" "+item.ChannelName), strings.ToLower(q.Search)) {
			continue
		}
		items = append(items, item)
		totalRequests += r.Cnt
		totalConsumerAmount += r.Amount
		totalOwnerIncome += r.Income
		uniqueUsers[r.UserID] = struct{}{}
	}
	sort.SliceStable(items, func(i, j int) bool {
		compare := func() int {
			switch q.Sort {
			case "amount":
				if items[i].TotalSettlementGrossAmount != items[j].TotalSettlementGrossAmount {
					if items[i].TotalSettlementGrossAmount < items[j].TotalSettlementGrossAmount {
						return -1
					}
					return 1
				}
			case "recent":
				if !items[i].LastRequestAt.Equal(items[j].LastRequestAt) {
					if items[i].LastRequestAt.Before(items[j].LastRequestAt) {
						return -1
					}
					return 1
				}
			default:
				if items[i].RequestCount != items[j].RequestCount {
					if items[i].RequestCount < items[j].RequestCount {
						return -1
					}
					return 1
				}
			}
			return 0
		}()
		if compare == 0 {
			return items[i].UserID < items[j].UserID
		}
		if q.Direction == "asc" {
			return compare < 0
		}
		return compare > 0
	})
	total := len(items)
	start := (q.Page - 1) * q.PageSize
	if start > total {
		start = total
	}
	end := start + q.PageSize
	if end > total {
		end = total
	}
	return map[string]any{"items": items[start:end], "total": total, "page": q.Page, "page_size": q.PageSize, "summary": map[string]any{"total_users": len(uniqueUsers), "total_requests": totalRequests, "consumer_amount": totalConsumerAmount, "owner_income": totalOwnerIncome}}, nil
}
func SetUserMultiplier(owner int, channel string, user int, m *float64) error {
	_, err := BatchSetUserMultipliers(owner, []MultiplierTarget{{ChannelID: channel, UserID: user}}, m)
	return err
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
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&r, "id=?", id).Error; err != nil {
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
			if err := validateMultiplier(r.ProposedMultiplier); err != nil {
				return err
			}
			var channel marketplaceschema.Channel
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&channel, "id=?", group.ChannelID).Error; err != nil {
				return err
			}
			r.Status = "approved"
			if _, err := setUserMultiplierTx(tx, group, r.UserID, &r.ProposedMultiplier, "bargain"); err != nil {
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
