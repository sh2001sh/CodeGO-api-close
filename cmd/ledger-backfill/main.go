package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformstore "github.com/sh2001sh/new-api/internal/platform/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type summary struct {
	WalletAccounts       int
	ClaudeWalletAccounts int
	TokenAccounts        int
	SubscriptionAccounts int
}

type negativeWalletLegacyCandidate struct {
	UserID      int
	AccountID   string
	LegacyQuota int64
}

type negativeWalletLegacySummary struct {
	Candidates           []negativeWalletLegacyCandidate
	TotalNormalizedQuota int64
}

func main() {
	apply := flag.Bool("apply", false, "persist ledger accounts and bootstrap entries")
	limit := flag.Int("limit", 0, "optional maximum subjects per account type")
	normalizeNegativeWalletLegacy := flag.Bool("normalize-negative-wallet-legacy", false, "normalize strict negative legacy wallet quotas to the canonical zero ledger balance")
	groupBuyID := flag.Int64("reconcile-group-buy-id", 0, "reconcile missing group-buy tier bonus for one group")
	flag.Parse()

	platformconfig.IsMasterNode = true
	if path := os.Getenv("SQLITE_PATH"); path != "" {
		platformdb.SQLitePath = path
	}
	if err := platformstore.InitPrimaryDB(); err != nil {
		log.Fatalf("initialize database: %v", err)
	}
	defer platformstore.CloseDatabases()
	if err := platformstore.ApplyV2Migrations(context.Background(), false); err != nil {
		log.Fatalf("apply v2 migrations: %v", err)
	}
	if *groupBuyID > 0 {
		if !*apply {
			log.Fatal("reconcile-group-buy-id requires --apply")
		}
		adjusted, err := commerceapp.ReconcileGroupBuyBonus(*groupBuyID)
		if err != nil {
			log.Fatalf("reconcile group buy bonus: %v", err)
		}
		fmt.Printf("group-buy members adjusted: %d\n", adjusted)
		return
	}
	if *normalizeNegativeWalletLegacy {
		plan, err := inspectNegativeWalletLegacy(*limit)
		if err != nil {
			log.Fatalf("inspect negative legacy wallet quotas: %v", err)
		}
		fmt.Printf("negative legacy wallet quotas to normalize: %d\n", len(plan.Candidates))
		fmt.Printf("total legacy quota to normalize: %d\n", plan.TotalNormalizedQuota)
		if !*apply {
			fmt.Println("dry-run only; rerun with --apply --normalize-negative-wallet-legacy to normalize matching legacy quotas")
			return
		}
		if err := applyNegativeWalletLegacyNormalization(*limit); err != nil {
			log.Fatalf("normalize negative legacy wallet quotas: %v", err)
		}
		fmt.Println("negative legacy wallet quota normalization completed")
		return
	}

	plan, err := inspect(*limit)
	if err != nil {
		log.Fatalf("inspect ledger backfill: %v", err)
	}
	fmt.Printf("wallet accounts to backfill: %d\n", plan.WalletAccounts)
	fmt.Printf("claude wallet accounts to backfill: %d\n", plan.ClaudeWalletAccounts)
	fmt.Printf("token accounts to backfill: %d\n", plan.TokenAccounts)
	fmt.Printf("subscription accounts to backfill: %d\n", plan.SubscriptionAccounts)
	if !*apply {
		fmt.Println("dry-run only; rerun with --apply to create missing ledger accounts")
		return
	}
	if err := applyBackfill(*limit); err != nil {
		log.Fatalf("apply ledger backfill: %v", err)
	}
	fmt.Println("ledger backfill completed")
}

func inspectNegativeWalletLegacy(limit int) (negativeWalletLegacySummary, error) {
	return inspectNegativeWalletLegacyTx(platformdb.DB, limit)
}

func inspectNegativeWalletLegacyTx(tx *gorm.DB, limit int) (negativeWalletLegacySummary, error) {
	var result negativeWalletLegacySummary
	if !tx.Migrator().HasTable(&identityschema.User{}) {
		return result, nil
	}
	var users []identityschema.User
	query := tx.Where("quota < ?", 0).Order("id asc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&users).Error; err != nil {
		return result, err
	}
	for _, user := range users {
		candidate, found, err := strictNegativeWalletLegacyCandidate(tx, user, false)
		if err != nil {
			return result, err
		}
		if !found {
			continue
		}
		result.Candidates = append(result.Candidates, candidate)
		result.TotalNormalizedQuota += -candidate.LegacyQuota
	}
	return result, nil
}

func strictNegativeWalletLegacyCandidate(tx *gorm.DB, user identityschema.User, lockSnapshot bool) (negativeWalletLegacyCandidate, bool, error) {
	if user.Quota >= 0 {
		return negativeWalletLegacyCandidate{}, false, nil
	}
	var account billingschema.BillingAccount
	err := tx.Where("owner_type = ? AND owner_id = ? AND account_type = ? AND quota_unit = ?", "user", user.Id, "wallet", "quota").First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return negativeWalletLegacyCandidate{}, false, nil
	}
	if err != nil {
		return negativeWalletLegacyCandidate{}, false, err
	}
	var snapshot billingschema.BillingBalanceSnapshot
	snapshotQuery := tx.Where("account_id = ?", account.AccountID)
	if lockSnapshot {
		snapshotQuery = snapshotQuery.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err = snapshotQuery.First(&snapshot).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return negativeWalletLegacyCandidate{}, false, nil
	}
	if err != nil {
		return negativeWalletLegacyCandidate{}, false, err
	}
	if snapshot.AvailableBalance != 0 || snapshot.ReservedBalance != 0 || snapshot.GrantedTotal != 0 || snapshot.ConsumedTotal != 0 || snapshot.RefundedTotal != 0 {
		return negativeWalletLegacyCandidate{}, false, nil
	}
	var entryCount int64
	if err := tx.Model(&billingschema.BillingLedgerEntry{}).Where("account_id = ?", account.AccountID).Count(&entryCount).Error; err != nil {
		return negativeWalletLegacyCandidate{}, false, err
	}
	if entryCount != 0 {
		return negativeWalletLegacyCandidate{}, false, nil
	}
	var openReservationCount int64
	if err := tx.Model(&billingschema.BillingReservation{}).Where("account_id = ? AND status = ?", account.AccountID, billingschema.BillingReservationStatusOpen).Count(&openReservationCount).Error; err != nil {
		return negativeWalletLegacyCandidate{}, false, err
	}
	if openReservationCount != 0 {
		return negativeWalletLegacyCandidate{}, false, nil
	}
	return negativeWalletLegacyCandidate{UserID: user.Id, AccountID: account.AccountID, LegacyQuota: int64(user.Quota)}, true, nil
}

func applyNegativeWalletLegacyNormalization(limit int) error {
	plan, err := inspectNegativeWalletLegacy(limit)
	if err != nil {
		return err
	}
	for _, candidate := range plan.Candidates {
		if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
			var user identityschema.User
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND quota < ?", candidate.UserID, 0).First(&user).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				return err
			}
			current, found, err := strictNegativeWalletLegacyCandidate(tx, user, true)
			if err != nil {
				return err
			}
			if !found {
				return nil
			}
			metadata, err := json.Marshal(map[string]any{
				"migration":             "legacy_negative_wallet_normalization",
				"previous_legacy_quota": current.LegacyQuota,
				"canonical_balance":     0,
			})
			if err != nil {
				return err
			}
			if _, err := billingdomain.RecordAdjustmentTx(tx, billingdomain.RecordAdjustmentParams{
				AccountID: current.AccountID, IdempotencyKey: fmt.Sprintf("migration:legacy-negative-wallet-normalization:user:%d", current.UserID),
				ReasonCode: "legacy_negative_quota_normalized", ReasonDetail: fmt.Sprintf("normalized legacy users.quota from %d to 0; ledger balance remains canonical at 0", current.LegacyQuota),
				ReferenceType: "user", ReferenceID: fmt.Sprintf("%d", current.UserID), OperatorType: "migration", Metadata: metadata,
			}); err != nil {
				return err
			}
			return tx.Model(&identityschema.User{}).Where("id = ? AND quota = ?", current.UserID, current.LegacyQuota).Update("quota", 0).Error
		}); err != nil {
			return err
		}
	}
	return nil
}

func inspect(limit int) (summary, error) {
	var result summary
	if !platformdb.DB.Migrator().HasTable(&identityschema.User{}) {
		return result, nil
	}
	var users []identityschema.User
	query := platformdb.DB.Order("id asc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&users).Error; err != nil {
		return result, err
	}
	for _, user := range users {
		if missingAccount("user", int64(user.Id), "wallet") {
			result.WalletAccounts++
		}
		if missingAccount("user", int64(user.Id), "claude_wallet") {
			result.ClaudeWalletAccounts++
		}
	}
	if platformdb.DB.Migrator().HasTable(&identityschema.Token{}) {
		var tokens []identityschema.Token
		tokenQuery := platformdb.DB.Order("id asc")
		if limit > 0 {
			tokenQuery = tokenQuery.Limit(limit)
		}
		if err := tokenQuery.Find(&tokens).Error; err != nil {
			return result, err
		}
		for _, token := range tokens {
			if !token.UnlimitedQuota && missingAccount("token", int64(token.Id), "token") {
				result.TokenAccounts++
			}
		}
	}

	if !platformdb.DB.Migrator().HasTable(&commerceschema.UserSubscription{}) {
		return result, nil
	}
	var subscriptions []commerceschema.UserSubscription
	subscriptionQuery := platformdb.DB.Order("id asc")
	if limit > 0 {
		subscriptionQuery = subscriptionQuery.Limit(limit)
	}
	if err := subscriptionQuery.Find(&subscriptions).Error; err != nil {
		return result, err
	}
	for _, subscription := range subscriptions {
		if missingAccount("user_subscription", int64(subscription.Id), "subscription") {
			result.SubscriptionAccounts++
		}
	}
	return result, nil
}

func missingAccount(ownerType string, ownerID int64, accountType string) bool {
	var count int64
	if err := platformdb.DB.Model(&billingschema.BillingAccount{}).
		Where("owner_type = ? AND owner_id = ? AND account_type = ? AND quota_unit = ?", ownerType, ownerID, accountType, "quota").
		Count(&count).Error; err != nil {
		log.Fatalf("inspect account owner_type=%s owner_id=%d: %v", ownerType, ownerID, err)
	}
	return count == 0
}

func applyBackfill(limit int) error {
	if !platformdb.DB.Migrator().HasTable(&identityschema.User{}) {
		return nil
	}
	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := backfillUsers(tx, limit); err != nil {
			return err
		}
		if err := backfillTokens(tx, limit); err != nil {
			return err
		}
		if err := commerceapp.MigrateBlindBoxLegacyCreditsTx(tx); err != nil {
			return err
		}
		if !tx.Migrator().HasTable(&commerceschema.UserSubscription{}) {
			return nil
		}
		return backfillSubscriptions(tx, limit)
	})
}

func backfillTokens(tx *gorm.DB, limit int) error {
	if !tx.Migrator().HasTable(&identityschema.Token{}) {
		return nil
	}
	var tokens []identityschema.Token
	query := tx.Order("id asc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&tokens).Error; err != nil {
		return err
	}
	for _, token := range tokens {
		if token.UnlimitedQuota {
			continue
		}
		if err := ensureBootstrapCredit(tx, "token", "token", int64(token.Id), int64(token.RemainQuota), fmt.Sprintf("ledger-backfill:token:%d", token.Id)); err != nil {
			return err
		}
	}
	return nil
}

func backfillUsers(tx *gorm.DB, limit int) error {
	var users []identityschema.User
	query := tx.Order("id asc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&users).Error; err != nil {
		return err
	}
	for _, user := range users {
		if err := ensureBootstrapCredit(tx, "wallet", "user", int64(user.Id), int64(user.Quota), fmt.Sprintf("ledger-backfill:user:%d:wallet", user.Id)); err != nil {
			return err
		}
		if err := ensureBootstrapCredit(tx, "claude_wallet", "user", int64(user.Id), int64(user.ClaudeQuota), fmt.Sprintf("ledger-backfill:user:%d:claude_wallet", user.Id)); err != nil {
			return err
		}
	}
	return nil
}

func backfillSubscriptions(tx *gorm.DB, limit int) error {
	var subscriptions []commerceschema.UserSubscription
	query := tx.Order("id asc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&subscriptions).Error; err != nil {
		return err
	}
	for _, subscription := range subscriptions {
		available := subscription.AmountTotal - subscription.AmountUsed
		if available < 0 {
			available = 0
		}
		if err := ensureBootstrapCredit(tx, "subscription", "user_subscription", int64(subscription.Id), available, fmt.Sprintf("ledger-backfill:subscription:%d", subscription.Id)); err != nil {
			return err
		}
	}
	return nil
}

func ensureBootstrapCredit(tx *gorm.DB, accountType string, ownerType string, ownerID int64, amount int64, idempotencyKey string) error {
	account, err := billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{AccountType: accountType, OwnerType: ownerType, OwnerID: ownerID, QuotaUnit: "quota"})
	if err != nil {
		return err
	}
	var entryCount int64
	if err := tx.Model(&billingschema.BillingLedgerEntry{}).Where("account_id = ?", account.AccountID).Count(&entryCount).Error; err != nil {
		return err
	}
	// Legacy balances can be negative after a historical over-consumption.
	// Preserve the account for coverage checks, but never manufacture a
	// compensating credit for a balance the ledger cannot represent.
	if entryCount > 0 || amount <= 0 {
		return nil
	}
	_, err = billingdomain.CreditAccountTx(tx, billingdomain.CreditAccountParams{
		AccountID: account.AccountID, Amount: amount, IdempotencyKey: idempotencyKey,
		ReasonCode: "legacy_balance_backfill", ReferenceType: ownerType, ReferenceID: fmt.Sprintf("%d", ownerID), OperatorType: "migration",
	})
	return err
}
