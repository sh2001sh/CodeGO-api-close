package app

import (
	"errors"
	"math/rand"
	"sync/atomic"

	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

var zeroHourStateTableReady atomic.Bool

func init() {
	billingapp.RegisterBlindBoxUsageHook(RecordBlindBoxZeroHourUsage)
}

const (
	ZeroHourGroup               = "zero-hour"
	zeroHourDurationSeconds     = int64(60 * 60)
	zeroHourProgressPerPaidOpen = int64(5)
	zeroHourProgressCap         = int64(1000)
	zeroHourBaseProbability     = 0.0001
	zeroHourProbabilityStep     = 0.0000049
	zeroHourProbabilityCap      = 0.005
)

type ZeroHourOverview struct {
	CurrentProbability float64 `json:"current_probability"`
	MaxProbability     float64 `json:"max_probability"`
	Points             int64   `json:"points"`
	PointCap           int64   `json:"point_cap"`
	Active             bool    `json:"active"`
	ActiveUntil        int64   `json:"active_until"`
}

func zeroHourProbability(points int64) float64 {
	if points < 0 {
		points = 0
	}
	if points > zeroHourProgressCap {
		points = zeroHourProgressCap
	}
	probability := zeroHourBaseProbability + float64(points)*zeroHourProbabilityStep
	if probability > zeroHourProbabilityCap {
		return zeroHourProbabilityCap
	}
	return probability
}

func getOrCreateZeroHourStateTx(tx *gorm.DB, userID int) (*commerceschema.BlindBoxZeroHourState, error) {
	if !zeroHourStateTableExists(tx) {
		return nil, nil
	}
	var state commerceschema.BlindBoxZeroHourState
	err := tx.Set("gorm:query_option", "FOR UPDATE").Where("user_id = ?", userID).First(&state).Error
	if err == nil {
		return &state, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	state = commerceschema.BlindBoxZeroHourState{UserId: userID}
	if err := tx.Create(&state).Error; err != nil {
		return nil, err
	}
	return &state, nil
}

func zeroHourStateTableExists(db *gorm.DB) bool {
	if zeroHourStateTableReady.Load() {
		return true
	}
	if db == nil || !db.Migrator().HasTable(&commerceschema.BlindBoxZeroHourState{}) {
		return false
	}
	zeroHourStateTableReady.Store(true)
	return true
}

func addZeroHourProgressTx(tx *gorm.DB, state *commerceschema.BlindBoxZeroHourState, points int64) error {
	if state == nil || points <= 0 {
		return nil
	}
	state.Points += points
	if state.Points > zeroHourProgressCap {
		state.Points = zeroHourProgressCap
	}
	state.UpdatedAt = platformruntime.GetTimestamp()
	return tx.Save(state).Error
}

// RecordBlindBoxZeroHourUsage adds one progress point per fully settled dollar.
func RecordBlindBoxZeroHourUsage(userID int, quota int) {
	if userID <= 0 || quota <= 0 {
		return
	}
	if !zeroHourStateTableExists(platformdb.DB) {
		return
	}
	quotaPerUnit := int64(platformruntime.QuotaPerUnit)
	quotaDelta := int64(quota)
	rowsAffected, err := incrementZeroHourUsageProgress(userID, quotaDelta, quotaPerUnit)
	if err != nil || rowsAffected > 0 {
		return
	}

	state := &commerceschema.BlindBoxZeroHourState{
		UserId:     userID,
		UsageQuota: quotaDelta,
		Points:     min(quotaDelta/quotaPerUnit, zeroHourProgressCap),
		UpdatedAt:  platformruntime.GetTimestamp(),
	}
	if platformdb.DB.Create(state).Error != nil {
		_, _ = incrementZeroHourUsageProgress(userID, quotaDelta, quotaPerUnit)
	}
}

func incrementZeroHourUsageProgress(userID int, quotaDelta int64, quotaPerUnit int64) (int64, error) {
	progressDelta := "((usage_quota + ?) / ?) - (usage_quota / ?)"
	update := platformdb.DB.Model(&commerceschema.BlindBoxZeroHourState{}).
		Where("user_id = ?", userID).
		Updates(map[string]any{
			"usage_quota": gorm.Expr("usage_quota + ?", quotaDelta),
			"points": gorm.Expr(
				"CASE WHEN points + "+progressDelta+" > ? THEN ? ELSE points + "+progressDelta+" END",
				quotaDelta, quotaPerUnit, quotaPerUnit, zeroHourProgressCap, zeroHourProgressCap,
				quotaDelta, quotaPerUnit, quotaPerUnit,
			),
			"updated_at": platformruntime.GetTimestamp(),
		})
	return update.RowsAffected, update.Error
}

func resetZeroHourProgressTx(tx *gorm.DB, state *commerceschema.BlindBoxZeroHourState) error {
	if state == nil {
		return nil
	}
	state.Points = 0
	state.UsageQuota = 0
	state.HitCount++
	state.UpdatedAt = platformruntime.GetTimestamp()
	return tx.Save(state).Error
}

func isPaidBlindBoxOrder(order *commerceschema.BlindBoxOrder) bool {
	return order != nil && order.Source == commerceschema.BlindBoxOrderSourcePurchase && order.Money > 0
}

func tryZeroHourRewardTx(tx *gorm.DB, userID int, state *commerceschema.BlindBoxZeroHourState) (bool, error) {
	if state == nil || hasAvailableOrActiveZeroHourPropTx(tx, userID) {
		return false, nil
	}
	return rand.Float64() < zeroHourProbability(state.Points), nil
}

func hasAvailableOrActiveZeroHourPropTx(tx *gorm.DB, userID int) bool {
	now := platformruntime.GetTimestamp()
	var count int64
	err := tx.Model(&commerceschema.BlindBoxProp{}).
		Where("user_id = ? AND prop_type = ?", userID, commerceschema.BlindBoxPropTypeZeroHourMultiplier).
		Where("status = ? OR (status = ? AND expires_at > ?)", commerceschema.BlindBoxPropStatusAvailable, commerceschema.BlindBoxPropStatusActive, now).
		Count(&count).Error
	return err == nil && count > 0
}

func hasActiveZeroHourPropTx(tx *gorm.DB, userID int) bool {
	now := platformruntime.GetTimestamp()
	var count int64
	err := tx.Model(&commerceschema.BlindBoxProp{}).
		Where("user_id = ? AND prop_type = ? AND status = ? AND expires_at > ?", userID, commerceschema.BlindBoxPropTypeZeroHourMultiplier, commerceschema.BlindBoxPropStatusActive, now).
		Count(&count).Error
	return err == nil && count > 0
}

func BuildZeroHourOverview(userID int) (ZeroHourOverview, error) {
	overview := ZeroHourOverview{CurrentProbability: zeroHourBaseProbability, MaxProbability: zeroHourProbabilityCap, PointCap: zeroHourProgressCap}
	if userID <= 0 {
		return overview, nil
	}
	var state commerceschema.BlindBoxZeroHourState
	err := platformdb.DB.Where("user_id = ?", userID).First(&state).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return overview, err
	}
	if err == nil {
		overview.Points = state.Points
		overview.CurrentProbability = zeroHourProbability(state.Points)
	}
	var prop commerceschema.BlindBoxProp
	now := platformruntime.GetTimestamp()
	if err := platformdb.DB.Where("user_id = ? AND prop_type = ? AND status = ? AND expires_at > ?", userID, commerceschema.BlindBoxPropTypeZeroHourMultiplier, commerceschema.BlindBoxPropStatusActive, now).First(&prop).Error; err == nil {
		overview.Active = true
		overview.ActiveUntil = prop.ExpiresAt
	}
	return overview, nil
}

// IsZeroHourGroupActive verifies the user-scoped group entitlement and expires stale props.
func IsZeroHourGroupActive(userID int) bool {
	if userID <= 0 {
		return false
	}
	now := platformruntime.GetTimestamp()
	var prop commerceschema.BlindBoxProp
	err := platformdb.DB.Where("user_id = ? AND prop_type = ? AND status = ? AND expires_at > ?", userID, commerceschema.BlindBoxPropTypeZeroHourMultiplier, commerceschema.BlindBoxPropStatusActive, now).First(&prop).Error
	return err == nil
}
