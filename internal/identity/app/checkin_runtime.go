package app

import (
	"errors"
	"fmt"
	"math/rand"
	"time"

	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

type checkinRecord struct {
	CheckinDate  string `json:"checkin_date"`
	QuotaAwarded int    `json:"quota_awarded"`
}

func getUserCheckinRecords(userID int, startDate string, endDate string) ([]identitydomain.Checkin, error) {
	var records []identitydomain.Checkin
	err := platformdb.DB.Where("user_id = ? AND checkin_date >= ? AND checkin_date <= ?", userID, startDate, endDate).
		Order("checkin_date DESC").
		Find(&records).Error
	return records, err
}

func hasCheckedInToday(userID int) (bool, error) {
	today := time.Now().Format("2006-01-02")
	var count int64
	err := platformdb.DB.Model(&identitydomain.Checkin{}).
		Where("user_id = ? AND checkin_date = ?", userID, today).
		Count(&count).Error
	return count > 0, err
}

func performUserCheckin(userID int) (*identitydomain.Checkin, error) {
	hasChecked, err := hasCheckedInToday(userID)
	if err != nil {
		return nil, err
	}
	if hasChecked {
		return nil, errors.New("今日已签到")
	}

	checkin := buildTodayCheckin(userID)
	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(checkin).Error; err != nil {
			return errors.New("签到失败，请稍后重试")
		}
		idempotencyKey := "checkin:" + checkin.CheckinDate + ":" + fmt.Sprint(userID)
		if err := billingapp.CreditClaudeWalletQuotaTx(tx, userID, checkin.QuotaAwarded, idempotencyKey, "daily_checkin_credit"); err != nil {
			return errors.New("签到失败：更新额度出错")
		}
		return nil
	}); err != nil {
		return nil, err
	}

	_ = identitystore.InvalidateUserCache(userID)
	return checkin, nil
}

func buildTodayCheckin(userID int) *identitydomain.Checkin {
	setting := identitystore.GetCheckinSetting()
	quotaAwarded := setting.MinQuota
	if setting.MaxQuota > setting.MinQuota {
		quotaAwarded = setting.MinQuota + rand.Intn(setting.MaxQuota-setting.MinQuota+1)
	}
	return &identitydomain.Checkin{
		UserId:       userID,
		CheckinDate:  time.Now().Format("2006-01-02"),
		QuotaAwarded: quotaAwarded,
		CreatedAt:    time.Now().Unix(),
	}
}

func loadUserCheckinStats(userID int, month string) (map[string]any, error) {
	startDate := month + "-01"
	endDate := month + "-31"

	records, err := getUserCheckinRecords(userID, startDate, endDate)
	if err != nil {
		return nil, err
	}

	payloadRecords := make([]checkinRecord, len(records))
	for index, record := range records {
		payloadRecords[index] = checkinRecord{
			CheckinDate:  record.CheckinDate,
			QuotaAwarded: record.QuotaAwarded,
		}
	}

	hasCheckedToday, err := hasCheckedInToday(userID)
	if err != nil {
		return nil, err
	}

	var totalCheckins int64
	if err := platformdb.DB.Model(&identitydomain.Checkin{}).Where("user_id = ?", userID).Count(&totalCheckins).Error; err != nil {
		return nil, err
	}

	var totalQuota int64
	if err := platformdb.DB.Model(&identitydomain.Checkin{}).
		Where("user_id = ?", userID).
		Select("COALESCE(SUM(quota_awarded), 0)").
		Scan(&totalQuota).Error; err != nil {
		return nil, err
	}

	return map[string]any{
		"total_quota":      totalQuota,
		"total_checkins":   totalCheckins,
		"checkin_count":    len(records),
		"checked_in_today": hasCheckedToday,
		"records":          payloadRecords,
	}, nil
}
