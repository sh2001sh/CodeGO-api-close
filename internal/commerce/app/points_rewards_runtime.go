package app

import (
	"fmt"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"strings"

	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

func packagePurchasePointReward(planTitle string) int64 {
	title := strings.TrimSpace(planTitle)
	cfg := getPointsRulesConfig()
	switch {
	case strings.HasPrefix(title, "Lite"):
		return cfg.PackagePurchasePoints["Lite"]
	case strings.HasPrefix(title, "Standard"):
		return cfg.PackagePurchasePoints["Standard"]
	case strings.HasPrefix(title, "Pro"):
		return cfg.PackagePurchasePoints["Pro"]
	case strings.HasPrefix(title, "Ultra"):
		return cfg.PackagePurchasePoints["Ultra"]
	default:
		return 0
	}
}

func awardPackagePurchasePointsTx(tx *gorm.DB, userID int, plan *commerceschema.SubscriptionPlan, orderID int) error {
	if tx == nil {
		tx = platformdb.DB
	}
	if userID <= 0 || plan == nil || orderID <= 0 {
		return nil
	}

	points := packagePurchasePointReward(plan.Title)
	if points <= 0 {
		return nil
	}

	key := fmt.Sprintf("package-purchase:%d:%d", userID, orderID)
	_, _, err := billingapp.AddPointLedgerTx(
		tx,
		userID,
		billingschema.PointLedgerTypeEarn,
		points,
		billingschema.PointSourcePackagePurchase,
		fmt.Sprintf("%d", orderID),
		key,
		"套餐购买赠送积分",
	)
	return err
}
