package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformstore "github.com/sh2001sh/new-api/internal/platform/store"
)

func main() {
	apply := flag.Bool("apply", false, "apply settled monthly group-buy tier differences")
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

	if !*apply {
		result, err := commerceapp.InspectSettledMonthlyGroupBuyBonuses()
		if err != nil {
			log.Fatalf("inspect settled monthly group-buy bonuses: %v", err)
		}
		fmt.Printf("settled monthly group buys scanned: %d\n", result.GroupsScanned)
		fmt.Printf("active subscription members to adjust: %d\n", result.MembersAdjusted)
		fmt.Printf("total subscription bonus to add: $%.2f\n", result.EligibleBonusUSD)
		fmt.Println("dry-run only; rerun with --apply to write subscription ledgers")
		return
	}

	result, err := commerceapp.ReconcileSettledMonthlyGroupBuyBonuses()
	if err != nil {
		log.Fatalf("reconcile settled monthly group-buy bonuses: %v", err)
	}
	fmt.Printf("settled monthly group buys scanned: %d\n", result.GroupsScanned)
	fmt.Printf("subscription members adjusted: %d\n", result.MembersAdjusted)
}
