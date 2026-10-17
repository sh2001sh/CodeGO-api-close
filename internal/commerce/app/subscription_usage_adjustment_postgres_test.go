package app

import (
	"fmt"
	"testing"
	"time"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSubscriptionLedgerPostgresConcurrentAdjustments(t *testing.T) {
	db := co04PostgresDB(t)
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS billing").Error)
	require.NoError(t, db.AutoMigrate(&commerceschema.SubscriptionPlan{}, &commerceschema.UserSubscription{},
		&billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingLedgerEntry{}, &billingschema.BillingOutboxEvent{}))
	for _, scenario := range []string{"shared-receipt", "over-budget", "bonus-and-debit"} {
		t.Run(scenario, func(t *testing.T) {
			id := int(time.Now().UnixNano() % 1000000000)
			plan := insertSubscriptionResetAppTestPlan(t, id, 0, 1000)
			sub := &commerceschema.UserSubscription{Id: id, UserId: id, PlanId: plan.Id, AmountTotal: 1000,
				Status: "active", StartTime: time.Now().Add(-time.Hour).Unix(), EndTime: time.Now().Add(time.Hour).Unix()}
			require.NoError(t, db.Create(sub).Error)
			initial := *sub
			type result struct {
				applied bool
				err     error
			}
			start := make(chan struct{})
			workers := 8
			if scenario == "bonus-and-debit" {
				workers = 16
			}
			results := make(chan result, workers)
			for index := range workers {
				go func(index int) {
					<-start
					if scenario == "bonus-and-debit" && index%2 == 0 {
						local := initial
						err := db.Transaction(func(tx *gorm.DB) error {
							return addSubscriptionBonusTx(tx, &local, 100, fmt.Sprintf("pg-bonus:%d:%d", id, index))
						})
						results <- result{err == nil, err}
						return
					}
					key, delta := fmt.Sprintf("pg-adjust:%d:%d", id, index), int64(50)
					if scenario == "shared-receipt" {
						key, delta = fmt.Sprintf("pg-shared:%d", id), 100
					}
					if scenario == "over-budget" {
						delta = 250
					}
					applied, err := AdjustUserSubscriptionUsageDelta(sub.Id, "gpt-5", delta, key)
					results <- result{applied, err}
				}(index)
			}
			close(start)
			applied, insufficient := 0, 0
			for range workers {
				outcome := <-results
				if scenario == "over-budget" && outcome.err != nil {
					require.ErrorIs(t, outcome.err, billingdomain.ErrInsufficientBalance)
					insufficient++
				} else {
					require.NoError(t, outcome.err)
				}
				if outcome.applied {
					applied++
				}
			}
			var account billingschema.BillingAccount
			require.NoError(t, db.First(&account, "owner_type = ? AND owner_id = ?", "user_subscription", sub.Id).Error)
			var stored commerceschema.UserSubscription
			require.NoError(t, db.First(&stored, sub.Id).Error)
			switch scenario {
			case "shared-receipt":
				require.Equal(t, 1, applied)
				require.EqualValues(t, 100, stored.AmountUsed)
				adminQuotaSnapshot(t, db, account.AccountID, 900, 0)
			case "over-budget":
				require.Equal(t, 4, applied)
				require.Equal(t, 4, insufficient)
				require.EqualValues(t, 1000, stored.AmountUsed)
				adminQuotaSnapshot(t, db, account.AccountID, 0, 0)
			case "bonus-and-debit":
				require.Equal(t, 16, applied)
				require.EqualValues(t, 1800, stored.AmountTotal)
				require.EqualValues(t, 400, stored.AmountUsed)
				adminQuotaSnapshot(t, db, account.AccountID, 1400, 0)
			}
			changed, err := reconcileSubscriptionLedgerProjection(sub.Id)
			require.NoError(t, err)
			require.False(t, changed)
		})
	}
}
