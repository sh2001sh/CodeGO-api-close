package app

import (
	"errors"

	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func groupBuyDB(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return platformdb.DB
}

func lockGroupBuyPlanTx(tx *gorm.DB, planID int) error {
	if tx == nil || planID <= 0 {
		return nil
	}
	var plan commerceschema.SubscriptionPlan
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id").
		Where("id = ?", planID).
		First(&plan).Error
}

func findJoinableGroupBuyByPlanTx(tx *gorm.DB, planID int) (*commerceschema.GroupBuyOrder, error) {
	db := groupBuyDB(tx)
	var order commerceschema.GroupBuyOrder
	query := db.Where("plan_id = ? AND status = ? AND expires_at > ? AND current_count < target_count", planID, commerceschema.GroupBuyStatusPending, platformruntime.GetTimestamp()).
		Order("created_at asc, id asc")
	if tx != nil {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &order, nil
}

func getJoinableGroupBuyTx(tx *gorm.DB, groupBuyID int64) (*commerceschema.GroupBuyOrder, error) {
	db := groupBuyDB(tx)
	var order commerceschema.GroupBuyOrder
	query := db.Where("id = ?", groupBuyID)
	if tx != nil {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.First(&order).Error; err != nil {
		return nil, err
	}
	if order.Status != commerceschema.GroupBuyStatusPending || order.ExpiresAt <= platformruntime.GetTimestamp() || order.CurrentCount >= order.TargetCount {
		return nil, ErrGroupBuyNotJoinable
	}
	return &order, nil
}

func ensureUserCanJoinGroupBuyTx(tx *gorm.DB, groupBuyID int64, userID int) error {
	var count int64
	if err := groupBuyDB(tx).Model(&commerceschema.GroupBuyMember{}).Where("group_buy_id = ? AND user_id = ?", groupBuyID, userID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrGroupBuyAlreadyJoined
	}
	return nil
}

func joinGroupBuyTx(tx *gorm.DB, userID int, groupBuyID int64, orderID int, expectedPlanID int, userSubscriptionID int) error {
	var order commerceschema.GroupBuyOrder
	if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ?", groupBuyID).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrGroupBuyNotFound
		}
		return err
	}
	if expectedPlanID > 0 && order.PlanId != expectedPlanID {
		return ErrGroupBuyNotJoinable
	}
	if order.Status != commerceschema.GroupBuyStatusPending || order.ExpiresAt <= platformruntime.GetTimestamp() || order.CurrentCount >= order.TargetCount {
		return ErrGroupBuyNotJoinable
	}

	plan, err := getSubscriptionPlanRecordTx(tx, order.PlanId)
	if err != nil {
		return err
	}
	if !supportsGroupBuyPlan(plan) {
		return ErrGroupBuyPlanNotEnabled
	}

	if err := ensureUserCanJoinGroupBuyTx(tx, groupBuyID, userID); err != nil {
		return err
	}

	member := commerceschema.GroupBuyMember{
		GroupBuyId:         groupBuyID,
		UserId:             userID,
		OrderId:            orderID,
		UserSubscriptionId: userSubscriptionID,
	}
	if err := tx.Create(&member).Error; err != nil {
		return err
	}
	return tx.Model(&commerceschema.GroupBuyOrder{}).
		Where("id = ?", groupBuyID).
		Updates(map[string]any{
			"current_count": gorm.Expr("current_count + ?", 1),
			"updated_at":    platformruntime.GetTimestamp(),
		}).Error
}
