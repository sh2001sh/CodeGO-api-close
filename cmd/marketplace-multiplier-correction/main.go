package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"sort"

	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformstore "github.com/sh2001sh/new-api/internal/platform/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Keep generated idempotency keys below the billing schema's varchar(64) limit.
const correctionKeyPrefix = "mmc:"

type candidate struct {
	ID                     string  `gorm:"column:id"`
	RequestID              string  `gorm:"column:request_id"`
	BillingSource          string  `gorm:"column:billing_source"`
	ConsumerUserID         int     `gorm:"column:consumer_user_id"`
	ConsumerAmount         int64   `gorm:"column:consumer_amount"`
	SettlementGrossAmount  int64   `gorm:"column:settlement_gross_amount"`
	PlatformCommission     int64   `gorm:"column:platform_commission"`
	OwnerNetAmount         int64   `gorm:"column:owner_net_amount"`
	Multiplier             float64 `gorm:"column:multiplier"`
	SubscriptionMultiplier float64 `gorm:"column:subscription_multiplier"`
	PendingAccountID       string  `gorm:"column:pending_account_id"`
	Status                 string  `gorm:"column:status"`
	SubscriptionID         int     `gorm:"column:subscription_id"`
	ModelName              string  `gorm:"column:model_name"`
}

type plan struct {
	Candidates   int
	Users        map[int]int64
	Wallet       int64
	Subscription int64
	Owner        int64
	Commission   int64
}

func main() {
	apply := flag.Bool("apply", false, "apply the correction")
	groupID := flag.String("group-id", "17a3b8b105444d1e81e06ba959c5661f", "marketplace group id")
	oldMultiplier := flag.Float64("old-multiplier", 0.9, "incorrect multiplier")
	newMultiplier := flag.Float64("new-multiplier", 0.09, "correct multiplier")
	flag.Parse()
	if *oldMultiplier <= 0 || *newMultiplier <= 0 || *newMultiplier >= *oldMultiplier {
		log.Fatal("new multiplier must be positive and lower than old multiplier")
	}

	if err := platformstore.InitPrimaryDB(); err != nil {
		log.Fatalf("initialize database: %v", err)
	}
	defer platformstore.CloseDatabases()

	candidates, err := loadCandidates(*groupID, *oldMultiplier)
	if err != nil {
		log.Fatalf("load candidates: %v", err)
	}
	printPlan(candidates, *oldMultiplier, *newMultiplier)
	if !*apply {
		fmt.Println("dry-run only; rerun with --apply to apply the correction")
		return
	}

	for _, item := range candidates {
		if err := correctOne(item, *oldMultiplier, *newMultiplier); err != nil {
			log.Fatalf("correct request %s: %v", item.RequestID, err)
		}
	}
	fmt.Printf("marketplace multiplier correction completed: %d settlements\n", len(candidates))
}

func loadCandidates(groupID string, oldMultiplier float64) ([]candidate, error) {
	const query = `
SELECT s.id, s.request_id, s.billing_source, s.consumer_user_id,
       s.consumer_amount, s.settlement_gross_amount, s.platform_commission,
       s.owner_net_amount, s.multiplier, s.subscription_multiplier,
       s.pending_account_id, s.status,
       COALESCE(l.subscription_id, 0) AS subscription_id,
       COALESCE(l.model_name, '') AS model_name
FROM marketplace.settlements AS s
LEFT JOIN LATERAL (
  SELECT COALESCE(NULLIF((x.other::jsonb->>'subscription_id'), '')::bigint, 0)::int AS subscription_id,
         x.model_name
  FROM logs AS x
  WHERE x.request_id = s.request_id
  ORDER BY CASE WHEN x.type = 2 THEN 0 ELSE 1 END, x.id DESC
  LIMIT 1
) AS l ON true
WHERE s.group_id = ? AND s.multiplier = ?
ORDER BY s.created_at ASC`
	var candidates []candidate
	if err := platformdb.DB.Raw(query, groupID, oldMultiplier).Scan(&candidates).Error; err != nil {
		return nil, err
	}
	for _, item := range candidates {
		if item.ConsumerAmount <= 0 || item.SettlementGrossAmount <= 0 || item.Status != "pending" {
			return nil, fmt.Errorf("request %s is not an adjustable pending settlement", item.RequestID)
		}
		if item.BillingSource == "subscription" && item.SubscriptionID <= 0 {
			return nil, fmt.Errorf("request %s has no subscription id", item.RequestID)
		}
	}
	return candidates, nil
}

func correctOne(item candidate, oldMultiplier, newMultiplier float64) error {
	ratio := newMultiplier / oldMultiplier
	refund := scaledAmount(item.ConsumerAmount, ratio)
	refund = item.ConsumerAmount - refund
	correctGross := scaledAmount(item.SettlementGrossAmount, ratio)
	correctCommission := percentage(correctGross, 5)
	correctOwnerNet := correctGross - correctCommission
	ownerExcess := item.OwnerNetAmount - correctOwnerNet
	commissionExcess := item.PlatformCommission - correctCommission

	return platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var settlement marketplaceschema.Settlement
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&settlement, "id = ?", item.ID).Error; err != nil {
			return err
		}
		if math.Abs(settlement.Multiplier-oldMultiplier) > 0.0000001 {
			return nil
		}
		if settlement.Status != "pending" {
			return fmt.Errorf("settlement %s is no longer pending", settlement.ID)
		}

		if ownerExcess > 0 {
			if err := debitAccountTx(tx, settlement.PendingAccountID, ownerExcess, correctionKeyPrefix+"owner-reserve:"+settlement.ID, correctionKeyPrefix+"owner-settle:"+settlement.ID); err != nil {
				return fmt.Errorf("debit owner pending balance: %w", err)
			}
		}
		if commissionExcess > 0 {
			platformAccount, err := billingdomain.EnsureBillingAccountTx(tx, billingdomain.EnsureAccountParams{
				AccountType: "marketplace_platform_revenue", OwnerType: "system", OwnerID: 1, QuotaUnit: "quota",
			})
			if err != nil {
				return err
			}
			if err := debitAccountTx(tx, platformAccount.AccountID, commissionExcess, correctionKeyPrefix+"commission-reserve:"+settlement.ID, correctionKeyPrefix+"commission-settle:"+settlement.ID); err != nil {
				return fmt.Errorf("debit platform commission: %w", err)
			}
		}

		if item.BillingSource == "subscription" {
			if err := commerceapp.CreditSubscriptionQuotaTx(tx, item.SubscriptionID, item.ModelName, refund, correctionKeyPrefix+"subscription-refund:"+settlement.ID, "marketplace_multiplier_correction"); err != nil {
				return fmt.Errorf("refund subscription quota: %w", err)
			}
		} else {
			if err := billingapp.CreditClaudeWalletQuotaTx(tx, settlement.ConsumerUserID, int(refund), correctionKeyPrefix+"wallet-refund:"+settlement.ID, "marketplace_multiplier_correction"); err != nil {
				return fmt.Errorf("refund wallet quota: %w", err)
			}
		}

		updates := map[string]any{
			"consumer_amount":         settlement.ConsumerAmount - refund,
			"settlement_gross_amount": correctGross,
			"platform_commission":     correctCommission,
			"owner_net_amount":        correctOwnerNet,
			"multiplier":              newMultiplier,
			"subscription_multiplier": newMultiplier * 10,
		}
		return tx.Model(&settlement).Updates(updates).Error
	})
}

func debitAccountTx(tx *gorm.DB, accountID string, amount int64, reserveKey, settleKey string) error {
	reservation, err := billingdomain.CreateReservationTx(tx, billingdomain.CreateReservationParams{
		AccountID: accountID, RequestID: reserveKey, ReservedAmount: amount, IdempotencyKey: reserveKey,
	})
	if err != nil {
		return err
	}
	_, err = billingdomain.SettleReservationTx(tx, billingdomain.SettleReservationParams{
		ReservationID: reservation.ReservationID, UsageEvidenceID: reserveKey, ActualAmount: amount, IdempotencyKey: settleKey,
	})
	return err
}

func scaledAmount(amount int64, ratio float64) int64 {
	return int64(math.Round(float64(amount) * ratio))
}

func percentage(amount, percent int64) int64 {
	return (amount*percent + 50) / 100
}

func printPlan(candidates []candidate, oldMultiplier, newMultiplier float64) {
	p := plan{Users: make(map[int]int64)}
	for _, item := range candidates {
		refund := item.ConsumerAmount - scaledAmount(item.ConsumerAmount, newMultiplier/oldMultiplier)
		p.Candidates++
		p.Users[item.ConsumerUserID] += refund
		if item.BillingSource == "subscription" {
			p.Subscription += refund
		} else {
			p.Wallet += refund
		}
		correctGross := scaledAmount(item.SettlementGrossAmount, newMultiplier/oldMultiplier)
		p.Owner += item.OwnerNetAmount - (correctGross - percentage(correctGross, 5))
		p.Commission += item.PlatformCommission - percentage(correctGross, 5)
	}
	userIDs := make([]int, 0, len(p.Users))
	for userID := range p.Users {
		userIDs = append(userIDs, userID)
	}
	sort.Ints(userIDs)
	fmt.Printf("marketplace multiplier correction: %.4fx -> %.4fx\n", oldMultiplier, newMultiplier)
	fmt.Printf("settlements=%d users=%d wallet_refund=%d subscription_refund=%d total_refund=%d owner_income_reduction=%d platform_commission_reduction=%d\n",
		p.Candidates, len(userIDs), p.Wallet, p.Subscription, p.Wallet+p.Subscription, p.Owner, p.Commission)
	for _, userID := range userIDs {
		fmt.Printf("  user_id=%d refund_quota=%d\n", userID, p.Users[userID])
	}
}
