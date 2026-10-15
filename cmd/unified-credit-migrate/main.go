package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformstore "github.com/sh2001sh/new-api/internal/platform/store"
)

func main() {
	apply := flag.Bool("apply", false, "apply the unified-credit cutover")
	flag.Parse()

	platformconfig.IsMasterNode = true
	if path := os.Getenv("SQLITE_PATH"); path != "" {
		platformdb.SQLitePath = path
	}
	if err := platformstore.InitPrimaryDB(); err != nil {
		panic(fmt.Errorf("initialize primary database: %w", err))
	}
	defer platformstore.CloseDatabases()
	if err := platformstore.ApplyV2Migrations(context.Background(), false); err != nil {
		panic(err)
	}

	summary, err := commerceapp.InspectUnifiedCreditMigration()
	if err != nil {
		panic(err)
	}
	printSummary(summary)
	if !*apply {
		fmt.Println("dry-run only; rerun with --apply after reviewing the settlement totals")
		return
	}
	result, err := commerceapp.ApplyUnifiedCreditMigration()
	if err != nil {
		panic(err)
	}
	fmt.Println("unified-credit migration completed")
	printSummary(result)
}

func printSummary(summary commerceapp.UnifiedCreditMigrationSummary) {
	fmt.Printf("users pending: %d\n", summary.UsersPending)
	fmt.Printf("subscriptions pending: %d\n", summary.SubscriptionsPending)
	fmt.Printf("subscriptions requiring review: %d\n", summary.SubscriptionsNeedReview)
	fmt.Printf("legacy GPT quota: %d\n", summary.LegacyGPTQuota)
	fmt.Printf("converted unified quota: %d\n", summary.ConvertedUnifiedQuota)
	fmt.Printf("special-rate users: %d\n", summary.SpecialRateUsers)
	fmt.Printf("special-rate converted quota: %d\n", summary.SpecialRateConvertedQuota)
	fmt.Printf("subscription settlement quota: %d\n", summary.SubscriptionUnifiedQuota)
}
