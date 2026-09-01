package app

import (
	"errors"
	"fmt"
	"github.com/sh2001sh/new-api/constant"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	blindboxsettings "github.com/sh2001sh/new-api/internal/commerce/blindboxsettings"
	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"strings"
	"time"

	// GetBlindBoxOverview returns the current blind-box overview snapshot for a user.
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

const (
	blindBoxHistoryRetentionDays = 30
	blindBoxHistoryDefaultSize   = 20
	blindBoxHistoryMaxSize       = 50
)

// BlindBoxHistoryPage is the paginated, user-scoped blind-box history payload.
type BlindBoxHistoryPage struct {
	Page          int                                 `json:"page"`
	PageSize      int                                 `json:"page_size"`
	Total         int64                               `json:"total"`
	RetentionDays int                                 `json:"retention_days"`
	CutoffTime    int64                               `json:"cutoff_time"`
	Records       []commerceschema.BlindBoxOpenRecord `json:"records"`
}

func GetBlindBoxOverview(userID int, recentLimit int) (*commercedomain.BlindBoxOverview, error) {
	if userID <= 0 {
		return nil, errors.New("invalid userId")
	}

	now := platformruntime.GetTimestamp()
	userQuota, err := billingapp.GetUserClaudeWalletQuota(userID)
	if err != nil {
		return nil, err
	}

	var orders []commerceschema.BlindBoxOrder
	if err := platformdb.DB.Where("user_id = ? AND status = ?", userID, constant.TopUpStatusSuccess).
		Order("id desc").
		Find(&orders).Error; err != nil {
		return nil, err
	}

	overview := &commercedomain.BlindBoxOverview{}
	for _, order := range orders {
		remaining := order.Quantity - order.OpenedCount
		if remaining > 0 {
			overview.AvailableBoxes += remaining
		}
	}
	if err := platformdb.DB.Model(&commerceschema.BlindBoxOrder{}).
		Where("user_id = ? AND status = ? AND create_time > ?", userID, constant.TopUpStatusPending, pendingBlindBoxOrderCutoff(now)).
		Select("COALESCE(SUM(quantity - opened_count), 0)").
		Scan(&overview.PendingBoxes).Error; err != nil {
		return nil, err
	}

	if recentLimit <= 0 {
		recentLimit = 20
	}
	if err := platformdb.DB.Where("user_id = ? AND create_time >= ?", userID, blindBoxHistoryCutoff(now)).
		Order("create_time desc, id desc").
		Limit(recentLimit).
		Find(&overview.RecentRecords).Error; err != nil {
		return nil, err
	}
	if err := attachBlindBoxPropStateTx(platformdb.DB, overview.RecentRecords); err != nil {
		return nil, err
	}
	if err := attachBlindBoxLuckyNumbersTx(platformdb.DB, overview.RecentRecords); err != nil {
		return nil, err
	}
	for index := range overview.RecentRecords {
		normalizeBlindBoxOpenRecordDisplay(&overview.RecentRecords[index])
	}

	overview.Quota = int64(userQuota)

	var pity commerceschema.BlindBoxPityState
	if err := platformdb.DB.Where("user_id = ?", userID).First(&pity).Error; err == nil {
		overview.PityProgress = pity.ConsecutiveLowRewards
	}

	setting := blindboxsettings.Get()
	overview.PityThreshold = setting.PityThreshold
	overview.EffectivePityThreshold = setting.PityThreshold

	dayStart, dayEnd := getBlindBoxDayRange(now)
	monthStart, monthEnd := getBlindBoxMonthRange(now)
	if overview.PurchasedToday, err = sumBlindBoxOrderQuantity(userID, dayStart, dayEnd); err != nil {
		return nil, err
	}
	if overview.PurchasedThisMonth, err = sumBlindBoxOrderQuantity(userID, monthStart, monthEnd); err != nil {
		return nil, err
	}

	return overview, nil
}

// GetBlindBoxStatistics returns all-time, user-scoped blind-box totals without
// loading the complete opening history into memory.
func GetBlindBoxStatistics(userID int) (*commercedomain.BlindBoxStatistics, error) {
	if userID <= 0 {
		return nil, errors.New("invalid userId")
	}

	type statisticRow struct {
		RewardType   string
		OpenedCount  int64
		RewardUSD    float64
		CreditAmount int64
		PityWins     int64
	}

	var rows []statisticRow
	if err := platformdb.DB.Model(&commerceschema.BlindBoxOpenRecord{}).
		Select(`reward_type, COUNT(*) AS opened_count, COALESCE(SUM(reward_usd), 0) AS reward_usd, COALESCE(SUM(credit_amount), 0) AS credit_amount, COALESCE(SUM(CASE WHEN is_pity THEN 1 ELSE 0 END), 0) AS pity_wins`).
		Where("user_id = ?", userID).
		Group("reward_type").
		Order("reward_type asc").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	statistics := &commercedomain.BlindBoxStatistics{
		Rewards: make([]commercedomain.BlindBoxRewardStatistics, 0, len(rows)),
	}
	for _, row := range rows {
		statistics.TotalOpened += row.OpenedCount
		statistics.PityWins += row.PityWins
		statistics.Rewards = append(statistics.Rewards, commercedomain.BlindBoxRewardStatistics{
			RewardType:   row.RewardType,
			OpenedCount:  row.OpenedCount,
			RewardUSD:    row.RewardUSD,
			CreditAmount: row.CreditAmount,
		})
	}
	return statistics, nil
}

// ListBlindBoxHistory returns one page of the user's opening records from the last 30 days.
func ListBlindBoxHistory(userID int, page int, pageSize int) (*BlindBoxHistoryPage, error) {
	if userID <= 0 {
		return nil, errors.New("invalid userId")
	}
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = blindBoxHistoryDefaultSize
	}
	if pageSize > blindBoxHistoryMaxSize {
		pageSize = blindBoxHistoryMaxSize
	}

	cutoff := blindBoxHistoryCutoff(platformruntime.GetTimestamp())
	query := platformdb.DB.Model(&commerceschema.BlindBoxOpenRecord{}).
		Where("user_id = ? AND create_time >= ?", userID, cutoff)
	result := &BlindBoxHistoryPage{
		Page:          page,
		PageSize:      pageSize,
		RetentionDays: blindBoxHistoryRetentionDays,
		CutoffTime:    cutoff,
		Records:       []commerceschema.BlindBoxOpenRecord{},
	}
	if err := query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	if err := query.Order("create_time desc, id desc").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&result.Records).Error; err != nil {
		return nil, err
	}
	if err := attachBlindBoxPropStateTx(platformdb.DB, result.Records); err != nil {
		return nil, err
	}
	if err := attachBlindBoxLuckyNumbersTx(platformdb.DB, result.Records); err != nil {
		return nil, err
	}
	for index := range result.Records {
		normalizeBlindBoxOpenRecordDisplay(&result.Records[index])
	}
	return result, nil
}

func blindBoxHistoryCutoff(now int64) int64 {
	return now - int64(blindBoxHistoryRetentionDays)*24*60*60
}

// IsBlindBoxFirstPurchaseEligible reports whether the user has no successful blind-box order yet.
func IsBlindBoxFirstPurchaseEligible(userID int) (bool, error) {
	if userID <= 0 {
		return false, nil
	}
	count, err := countSuccessfulBlindBoxOrdersTx(platformdb.DB, userID, nil)
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

// GetBlindBoxOrderByTradeNo returns a blind-box order by trade number.
func GetBlindBoxOrderByTradeNo(tradeNo string) *commerceschema.BlindBoxOrder {
	if strings.TrimSpace(tradeNo) == "" {
		return nil
	}
	var order commerceschema.BlindBoxOrder
	if err := platformdb.DB.Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		return nil
	}
	return &order
}

// GetBlindBoxOrderByTradeNoForUser returns a blind-box order for a specific user.
func GetBlindBoxOrderByTradeNoForUser(tradeNo string, userID int) (*commerceschema.BlindBoxOrder, error) {
	if strings.TrimSpace(tradeNo) == "" || userID <= 0 {
		return nil, errors.New("invalid tradeNo or userId")
	}
	var order commerceschema.BlindBoxOrder
	if err := platformdb.DB.Where("trade_no = ? AND user_id = ?", tradeNo, userID).First(&order).Error; err != nil {
		return nil, err
	}
	return &order, nil
}

func getBlindBoxDayRange(now int64) (int64, int64) {
	base := time.Unix(now, 0).In(time.Local)
	start := time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, base.Location()).Unix()
	return start, start + 24*3600
}

func getBlindBoxMonthRange(now int64) (int64, int64) {
	base := time.Unix(now, 0).In(time.Local)
	start := time.Date(base.Year(), base.Month(), 1, 0, 0, 0, 0, base.Location()).Unix()
	return start, time.Date(base.Year(), base.Month()+1, 1, 0, 0, 0, 0, base.Location()).Unix()
}

func sumBlindBoxOrderQuantity(userID int, start, end int64) (int, error) {
	type result struct {
		Total int64 `gorm:"column:total"`
	}
	var row result
	cutoff := pendingBlindBoxOrderCutoff(platformruntime.GetTimestamp())
	err := platformdb.DB.Model(&commerceschema.BlindBoxOrder{}).
		Select("COALESCE(SUM(quantity), 0) AS total").
		Where("user_id = ? AND create_time >= ? AND create_time < ? AND money > 0 AND (status = ? OR (status = ? AND create_time > ?))", userID, start, end, constant.TopUpStatusSuccess, constant.TopUpStatusPending, cutoff).
		Scan(&row).Error
	return int(row.Total), err
}

func countSuccessfulBlindBoxOrdersTx(tx *gorm.DB, userID int, beforeOrderID *int) (int64, error) {
	var count int64
	query := tx.Model(&commerceschema.BlindBoxOrder{}).
		Where("user_id = ? AND status = ? AND money > 0", userID, constant.TopUpStatusSuccess)
	if beforeOrderID != nil && *beforeOrderID > 0 {
		query = query.Where("id < ?", *beforeOrderID)
	}
	err := query.Count(&count).Error
	return count, err
}

func isFirstSuccessfulBlindBoxOrderTx(tx *gorm.DB, userID int, orderID int) (bool, error) {
	if userID <= 0 || orderID <= 0 {
		return false, nil
	}
	count, err := countSuccessfulBlindBoxOrdersTx(tx, userID, &orderID)
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

func normalizeBlindBoxOpenRecordDisplay(record *commerceschema.BlindBoxOpenRecord) {
	if record == nil {
		return
	}
	if record.RewardTier == "first_purchase" {
		record.RewardTitle = formatFirstPurchaseBlindBoxRewardTitle(record.RewardUSD)
	}
	if record.RewardType == commerceschema.BlindBoxRewardTypeProp && record.PropType != "" {
		if spec, ok := getBlindBoxPropSpecByType(record.PropType); ok {
			record.RewardTitle = spec.Title
		}
	}
	if record.RewardType == commerceschema.BlindBoxRewardTypeQuota {
		record.RewardTitle = fmt.Sprintf("%.2f 统一额度奖励", record.RewardUSD)
	}
	if record.RewardType == commerceschema.BlindBoxRewardTypeClaudeQuota {
		record.RewardTitle = fmt.Sprintf("%.2f 通用额度奖励", record.RewardUSD)
	}
	if record.RewardType == commerceschema.BlindBoxRewardTypeProp && record.RewardTitle == "" {
		record.RewardTitle = "实用道具奖励"
	}
}

func attachBlindBoxPropStateTx(tx *gorm.DB, records []commerceschema.BlindBoxOpenRecord) error {
	if tx == nil || len(records) == 0 {
		return nil
	}
	if err := ensureBlindBoxPropSchema(tx); err != nil {
		return err
	}
	openRecordIDs := make([]int, 0, len(records))
	indexByOpenRecordID := make(map[int]int, len(records))
	for index := range records {
		if records[index].RewardType != commerceschema.BlindBoxRewardTypeProp || records[index].Id <= 0 {
			continue
		}
		openRecordIDs = append(openRecordIDs, records[index].Id)
		indexByOpenRecordID[records[index].Id] = index
	}
	if len(openRecordIDs) == 0 {
		return nil
	}
	var props []commerceschema.BlindBoxProp
	if err := tx.Where("open_record_id IN ?", openRecordIDs).Find(&props).Error; err != nil {
		return err
	}
	for _, prop := range props {
		index, ok := indexByOpenRecordID[prop.OpenRecordId]
		if !ok {
			continue
		}
		records[index].PropId = prop.Id
		records[index].PropType = prop.PropType
		records[index].PropStatus = prop.Status
		records[index].PropExpiresAt = prop.ExpiresAt
	}
	return nil
}
