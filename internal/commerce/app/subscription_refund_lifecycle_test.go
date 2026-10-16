package app

import (
	"fmt"
	"testing"

	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSubscriptionPaymentCallbackCannotReactivateRefundWithStalePreview(t *testing.T) {
	for _, status := range []string{"refund_pending", commerceschema.SubscriptionStatusSettledCancelled} {
		for _, action := range []string{"renew", "upgrade"} {
			t.Run(fmt.Sprintf("%s_%s", status, action), func(t *testing.T) {
				db := setupRedemptionTestDB(t)
				sub := commerceschema.UserSubscription{Id: 84220, UserId: 84221, PlanId: 1, Status: "active", AmountTotal: 1000, AmountUsed: 300, EndTime: 4102444800}
				require.NoError(t, db.Create(&sub).Error)
				stale := sub // The preview was loaded before the refund acquired its lock.
				require.NoError(t, db.Model(&sub).Update("status", status).Error)
				plan := &commerceschema.SubscriptionPlan{Id: 2, DurationUnit: commerceschema.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 2000}
				err := db.Transaction(func(tx *gorm.DB) error {
					if action == "renew" {
						_, err := renewUserSubscriptionWithPlanTx(tx, &stale, plan, "order")
						return err
					}
					_, err := upgradeUserSubscriptionWithPlanTx(tx, &stale, plan, "order", true)
					return err
				})
				require.ErrorContains(t, err, "套餐退款处理中或已结算取消")
				var saved commerceschema.UserSubscription
				require.NoError(t, db.First(&saved, sub.Id).Error)
				require.Equal(t, status, saved.Status)
				require.EqualValues(t, 1000, saved.AmountTotal)
				require.EqualValues(t, 300, saved.AmountUsed)
				require.Equal(t, sub.EndTime, saved.EndTime)
				require.Equal(t, sub.PlanId, saved.PlanId)
			})
		}
	}
}
