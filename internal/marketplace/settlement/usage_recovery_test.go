package settlement

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func recoveryFixture(t *testing.T, source string, pack bool) (*gorm.DB, auditschema.Log) {
	t.Helper()
	db := openSettlementTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Group{}, &commerceschema.UserSubscription{}))
	internalID := 101
	require.NoError(t, db.Create(&marketplaceschema.Channel{ID: "channel", OwnerUserID: 10, InternalChannelID: &internalID}).Error)
	require.NoError(t, db.Create(&marketplaceschema.Group{ID: "group", ChannelID: "channel", OwnerUserID: 10}).Error)
	log := auditschema.Log{RequestId: "recovery-request", UserId: 20, ChannelId: 101, Type: auditschema.LogTypeConsume, Quota: 1000, CreatedAt: time.Now().Add(-25 * time.Hour).Unix()}
	log.Other = fmt.Sprintf(`{"marketplace_group_id":"group","marketplace_multiplier":1.5,"billing_source":%q}`, source)
	fundingID := log.RequestId
	account := billingschema.BillingAccount{AccountID: "consumer", OwnerType: "user", OwnerID: 20, AccountType: "claude_wallet", QuotaUnit: "quota"}
	if source == "subscription" {
		account.OwnerType, account.OwnerID, account.AccountType = "user_subscription", 1, "subscription"
		sub := commerceschema.UserSubscription{Id: 1, UserId: 20}
		if pack {
			sub.CreditPackGrantedQuota, sub.CreditPackPaidQuota, sub.CreditPackChannelShareBPS = 1100, 1000, 6000
			log.Other = `{"marketplace_group_id":"group","billing_source":"subscription","subscription_billing_version":"credit_pack","credit_pack_granted_quota":1100,"credit_pack_paid_quota":1000,"credit_pack_channel_share_bps":6000,"marketplace_fee_policy_version":"paid_bonus_rate_v2","subscription_id":1}`
			digest := sha256.Sum256([]byte("credit-pack:" + log.RequestId))
			fundingID = fmt.Sprintf("cp:%x", digest[:30])
		}
		require.NoError(t, db.Create(&sub).Error)
	}
	require.NoError(t, db.Create(&account).Error)
	// A request can spend several native reservations; checking only the first
	// would falsely reject a fully paid subscription request.
	for i, amount := range []int64{400, 600} {
		id := fmt.Sprintf("reservation-%d", i)
		require.NoError(t, db.Create(&billingschema.BillingReservation{ReservationID: id, RequestID: fundingID, AccountID: account.AccountID, Status: "settled", IdempotencyKey: id}).Error)
		require.NoError(t, db.Create(&billingschema.BillingSettlement{ReservationID: id, ActualAmount: amount, Status: "completed", IdempotencyKey: id + ":settle"}).Error)
	}
	return db, log
}

func TestRecoverUsageOnlyCreditsConfirmedDebitOnce(t *testing.T) {
	for _, test := range []struct {
		name, source string
		pack         bool
		wantNet      int64
	}{
		{"wallet", "wallet", false, 950},
		{"legacy subscription", "subscription", false, 95},
		{"credit pack", "subscription", true, 890},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, log := recoveryFixture(t, test.source, test.pack)
			var item *marketplaceschema.Settlement
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
				var err error
				item, err = RecoverUsageTx(tx, log)
				return err
			}))
			require.Equal(t, test.wantNet, item.OwnerNetAmount)
			require.Equal(t, time.Unix(log.CreatedAt, 0).UTC(), item.CreatedAt)
			require.Equal(t, time.Unix(log.CreatedAt, 0).Add(24*time.Hour).UTC(), item.AvailableAt)
			var snapshot billingschema.BillingBalanceSnapshot
			require.NoError(t, db.First(&snapshot, "account_id = ?", item.PendingAccountID).Error)
			require.Equal(t, test.wantNet, snapshot.AvailableBalance)
			for _, status := range []string{"pending", "released", "reclaimed", "forfeited"} {
				require.NoError(t, db.Model(item).Update("status", status).Error)
				require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
					replay, err := RecoverUsageTx(tx, log)
					if err == nil {
						require.Equal(t, status, replay.Status)
					}
					return err
				}))
			}
			var count int64
			require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("idempotency_key = ?", "marketplace-pending:"+log.RequestId).Count(&count).Error)
			require.EqualValues(t, 1, count)
			require.NoError(t, db.First(&snapshot, "account_id = ?", item.PendingAccountID).Error)
			require.Equal(t, test.wantNet, snapshot.AvailableBalance)
		})
	}
}

func TestRecoverUsageRefusesUnconfirmedOrMismatchedFunding(t *testing.T) {
	for _, test := range []string{"open", "released", "short debit", "wrong consumer", "wrong channel", "missing price", "no funding"} {
		t.Run(test, func(t *testing.T) {
			db, log := recoveryFixture(t, "wallet", false)
			switch test {
			case "open", "released":
				require.NoError(t, db.Model(&billingschema.BillingReservation{}).Where("reservation_id = ?", "reservation-1").Update("status", test).Error)
			case "short debit":
				require.NoError(t, db.Model(&billingschema.BillingSettlement{}).Where("reservation_id = ?", "reservation-1").Update("actual_amount", 500).Error)
			case "wrong consumer":
				log.UserId++
			case "wrong channel":
				log.ChannelId++
			case "missing price":
				log.Other = `{"marketplace_group_id":"group","billing_source":"wallet"}`
			case "no funding":
				log.RequestId = "no-debit"
			}
			require.Error(t, db.Transaction(func(tx *gorm.DB) error { _, err := RecoverUsageTx(tx, log); return err }))
			var count int64
			require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestRecoverUsageRollsBackIfLedgerCreditFails(t *testing.T) {
	db, log := recoveryFixture(t, "wallet", false)
	require.NoError(t, db.Exec("CREATE TRIGGER reject_recovery_credit BEFORE INSERT ON billing_ledger_entries BEGIN SELECT RAISE(ABORT, 'credit failed'); END").Error)
	err := db.Transaction(func(tx *gorm.DB) error { _, err := RecoverUsageTx(tx, log); return err })
	require.ErrorContains(t, err, "credit failed")
	var count int64
	require.NoError(t, db.Model(&marketplaceschema.Settlement{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Exec("DROP TRIGGER reject_recovery_credit").Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { _, err := RecoverUsageTx(tx, log); return err }))
	rollback := errors.New("rollback rehearsal")
	err = db.Transaction(func(tx *gorm.DB) error {
		_, err := RecoverUsageTx(tx, log)
		if err != nil {
			return err
		}
		return rollback
	})
	require.ErrorIs(t, err, rollback)
}

func TestRecoverUsageLegacyRoundingDoesNotInventIncome(t *testing.T) {
	for _, test := range []struct{ debit, net int64 }{{4, 0}, {5, 1}, {14, 1}, {15, 2}} {
		t.Run(fmt.Sprint(test.debit), func(t *testing.T) {
			db, log := recoveryFixture(t, "subscription", false)
			log.Quota = int(test.debit)
			require.NoError(t, db.Model(&billingschema.BillingSettlement{}).Where("reservation_id = ?", "reservation-0").Update("actual_amount", 0).Error)
			require.NoError(t, db.Model(&billingschema.BillingSettlement{}).Where("reservation_id = ?", "reservation-1").Update("actual_amount", test.debit).Error)
			err := db.Transaction(func(tx *gorm.DB) error {
				item, err := RecoverUsageTx(tx, log)
				if err == nil {
					require.Equal(t, test.net, item.OwnerNetAmount)
				}
				return err
			})
			if test.net == 0 {
				require.ErrorIs(t, err, ErrUsageNoIncome)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
