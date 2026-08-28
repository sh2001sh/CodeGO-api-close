package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformstore "github.com/sh2001sh/new-api/internal/platform/store"
	"gorm.io/gorm"
)

type verificationReport struct {
	MissingMigrations                  []string
	UnmaterializedWalletAccounts       int
	UnmaterializedClaudeWalletAccounts int
	UnmaterializedTokenAccounts        int
	UnmaterializedSubscriptionAccounts int
	InconsistentLedgers                int
	PendingOutboxEvents                int64
	LegacyBlindBoxCredits              int64
	MissingSettlementColumns           []string
}

func main() {
	strict := flag.Bool("strict", false, "fail when pending outbox events or legacy blind-box credits remain")
	flag.Parse()

	platformconfig.IsMasterNode = true
	if path := os.Getenv("SQLITE_PATH"); path != "" {
		platformdb.SQLitePath = path
	}
	if err := platformstore.InitPrimaryDB(); err != nil {
		panic(fmt.Errorf("initialize primary database: %w", err))
	}
	defer platformstore.CloseDatabases()

	report, err := verify(context.Background())
	if err != nil {
		panic(err)
	}
	printReport(report)
	if report.hasFailures(*strict) {
		os.Exit(1)
	}
}

func verify(ctx context.Context) (verificationReport, error) {
	report := verificationReport{}
	if platformdb.DB == nil {
		return report, fmt.Errorf("primary database is not initialized")
	}
	if !platformdb.DB.Migrator().HasTable(migrationTableName()) {
		return report, fmt.Errorf("v2 migration table is missing")
	}
	for _, id := range platformstore.V2MigrationIDs() {
		var count int64
		if err := platformdb.DB.WithContext(ctx).Table(migrationTableName()).Where("id = ?", id).Count(&count).Error; err != nil {
			return report, err
		}
		if count == 0 {
			report.MissingMigrations = append(report.MissingMigrations, id)
		}
	}
	if platformdb.DB.Migrator().HasTable(&marketplaceschema.Settlement{}) {
		for _, column := range []string{"ReclaimedAt", "ForfeitedAt"} {
			if !platformdb.DB.Migrator().HasColumn(&marketplaceschema.Settlement{}, column) {
				report.MissingSettlementColumns = append(report.MissingSettlementColumns, column)
			}
		}
	}

	var err error
	if report.UnmaterializedWalletAccounts, err = countUnmaterializedUserAccounts(ctx, "wallet"); err != nil {
		return report, err
	}
	if report.UnmaterializedClaudeWalletAccounts, err = countUnmaterializedUserAccounts(ctx, "claude_wallet"); err != nil {
		return report, err
	}
	if report.UnmaterializedTokenAccounts, err = countUnmaterializedTokenAccounts(ctx); err != nil {
		return report, err
	}
	if report.UnmaterializedSubscriptionAccounts, err = countUnmaterializedSubscriptionAccounts(ctx); err != nil {
		return report, err
	}
	if report.InconsistentLedgers, err = billingapp.CountLedgerInconsistencies(ctx); err != nil {
		return report, err
	}
	if err := platformdb.DB.WithContext(ctx).Model(&billingschema.BillingOutboxEvent{}).
		Where("status = ?", billingschema.BillingOutboxStatusPending).Count(&report.PendingOutboxEvents).Error; err != nil {
		return report, err
	}
	if platformdb.DB.Migrator().HasTable(&commerceschema.BlindBoxCredit{}) {
		if err := platformdb.DB.WithContext(ctx).Model(&commerceschema.BlindBoxCredit{}).
			Where("migrated_at = ?", 0).
			Count(&report.LegacyBlindBoxCredits).Error; err != nil {
			return report, err
		}
	}
	return report, nil
}

func migrationTableName() string {
	if platformdb.UsingPostgreSQL {
		return "platform.schema_migrations"
	}
	return "platform_schema_migrations"
}

func countUnmaterializedUserAccounts(ctx context.Context, accountType string) (int, error) {
	return countUnmaterializedAccounts(ctx, &identityschema.User{}, func(record *identityschema.User) billingOwner {
		if !userAccountRequiresVerification(record, accountType) {
			return billingOwner{}
		}
		return billingOwner{OwnerType: "user", OwnerID: int64(record.Id), AccountType: accountType}
	})
}

func countUnmaterializedTokenAccounts(ctx context.Context) (int, error) {
	return countUnmaterializedAccounts(ctx, &identityschema.Token{}, func(record *identityschema.Token) billingOwner {
		if record.UnlimitedQuota || (record.RemainQuota == 0 && record.UsedQuota == 0) {
			return billingOwner{}
		}
		return billingOwner{OwnerType: "token", OwnerID: int64(record.Id), AccountType: "token"}
	})
}

func countUnmaterializedSubscriptionAccounts(ctx context.Context) (int, error) {
	return countUnmaterializedAccounts(ctx, &commerceschema.UserSubscription{}, func(record *commerceschema.UserSubscription) billingOwner {
		if record.AmountTotal == 0 && record.AmountUsed == 0 && record.PeriodAmount == 0 && record.PeriodUsed == 0 {
			return billingOwner{}
		}
		return billingOwner{OwnerType: "user_subscription", OwnerID: int64(record.Id), AccountType: "subscription"}
	})
}

func userAccountRequiresVerification(user *identityschema.User, accountType string) bool {
	if user == nil {
		return false
	}
	switch accountType {
	case "wallet":
		return user.Quota != 0 || user.UsedQuota != 0
	case "claude_wallet":
		return user.ClaudeQuota != 0
	default:
		return false
	}
}

type billingOwner struct {
	OwnerType   string
	OwnerID     int64
	AccountType string
}

// countUnmaterializedAccounts reports records that will create a ledger account on first use.
// They are expected in a lazy-account system and must not fail deployment verification.
func countUnmaterializedAccounts[T any](ctx context.Context, model *T, ownerFor func(*T) billingOwner) (int, error) {
	if !platformdb.DB.Migrator().HasTable(model) {
		return 0, nil
	}
	unmaterialized := 0
	var records []T
	err := platformdb.DB.WithContext(ctx).Order("id asc").FindInBatches(&records, 500, func(tx *gorm.DB, _ int) error {
		for index := range records {
			owner := ownerFor(&records[index])
			if owner.OwnerType == "" {
				continue
			}
			var count int64
			if err := tx.Model(&billingschema.BillingAccount{}).
				Where("owner_type = ? AND owner_id = ? AND account_type = ? AND quota_unit = ?", owner.OwnerType, owner.OwnerID, owner.AccountType, "quota").
				Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				unmaterialized++
			}
		}
		return nil
	}).Error
	return unmaterialized, err
}

func (report verificationReport) hasFailures(strict bool) bool {
	return len(report.MissingMigrations) > 0 ||
		len(report.MissingSettlementColumns) > 0 ||
		report.InconsistentLedgers > 0 ||
		(strict && (report.PendingOutboxEvents > 0 || report.LegacyBlindBoxCredits > 0))
}

func printReport(report verificationReport) {
	fmt.Printf("missing migrations: %d\n", len(report.MissingMigrations))
	for _, id := range report.MissingMigrations {
		fmt.Printf("  %s\n", id)
	}
	fmt.Printf("missing marketplace settlement columns: %d\n", len(report.MissingSettlementColumns))
	for _, column := range report.MissingSettlementColumns {
		fmt.Printf("  %s\n", column)
	}
	fmt.Printf("unmaterialized wallet accounts: %d\n", report.UnmaterializedWalletAccounts)
	fmt.Printf("unmaterialized Claude wallet accounts: %d\n", report.UnmaterializedClaudeWalletAccounts)
	fmt.Printf("unmaterialized token accounts: %d\n", report.UnmaterializedTokenAccounts)
	fmt.Printf("unmaterialized subscription accounts: %d\n", report.UnmaterializedSubscriptionAccounts)
	fmt.Printf("inconsistent ledger snapshots: %d\n", report.InconsistentLedgers)
	fmt.Printf("pending ledger outbox events: %d\n", report.PendingOutboxEvents)
	fmt.Printf("remaining legacy blind-box credits: %d\n", report.LegacyBlindBoxCredits)
}
