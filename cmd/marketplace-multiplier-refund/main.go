package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"sort"

	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformstore "github.com/sh2001sh/new-api/internal/platform/store"
	"gorm.io/gorm"
)

const refundKeyPrefix = "marketplace-multiplier-refund:"

type refundCandidate struct {
	RequestID      string  `gorm:"column:request_id"`
	UserID         int     `gorm:"column:user_id"`
	ConsumerAmount int64   `gorm:"column:consumer_amount"`
	Multiplier     float64 `gorm:"column:multiplier"`
	RefundAmount   int64
}

func main() {
	apply := flag.Bool("apply", false, "credit refunds to affected users")
	flag.Parse()

	if err := platformstore.InitPrimaryDB(); err != nil {
		log.Fatalf("initialize database: %v", err)
	}
	defer func() {
		if err := platformstore.CloseDatabases(); err != nil {
			log.Printf("close databases: %v", err)
		}
	}()

	candidates, err := loadCandidates()
	if err != nil {
		log.Fatalf("load refund candidates: %v", err)
	}
	printPlan(candidates)
	if !*apply {
		fmt.Println("dry-run only; rerun with --apply to credit the listed refunds")
		return
	}

	for _, candidate := range candidates {
		if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
			return billingapp.CreditWalletQuotaTx(
				tx,
				candidate.UserID,
				int(candidate.RefundAmount),
				refundKeyPrefix+candidate.RequestID,
				"marketplace_multiplier_refund",
			)
		}); err != nil {
			log.Fatalf("credit request %s: %v", candidate.RequestID, err)
		}
		identitystore.InvalidateUserCache(candidate.UserID)
	}
	fmt.Printf("marketplace multiplier refunds completed: %d requests\n", len(candidates))
}

func loadCandidates() ([]refundCandidate, error) {
	const query = `
SELECT s.request_id, s.consumer_user_id AS user_id, s.consumer_amount, s.multiplier
FROM marketplace.settlements AS s
WHERE s.consumer_amount > 0
  AND s.multiplier > 0
  AND s.multiplier <> 1
  AND EXISTS (
    SELECT 1 FROM logs AS l
    WHERE l.request_id = s.request_id
      AND l.other::jsonb #>> '{group_ratio}' = '1'
  )
  AND NOT EXISTS (
    SELECT 1 FROM billing.ledger_entries AS e
    WHERE e.idempotency_key = ? || s.request_id
  )
ORDER BY s.created_at ASC`

	var candidates []refundCandidate
	if err := platformdb.DB.Raw(query, refundKeyPrefix).Scan(&candidates).Error; err != nil {
		return nil, err
	}
	filtered := candidates[:0]
	for _, candidate := range candidates {
		candidate.RefundAmount = calculateRefund(candidate.ConsumerAmount, candidate.Multiplier)
		if candidate.RefundAmount > 0 {
			filtered = append(filtered, candidate)
		}
	}
	return filtered, nil
}

func calculateRefund(actualAmount int64, multiplier float64) int64 {
	if actualAmount <= 0 || multiplier <= 0 || multiplier >= 1 {
		return 0
	}
	correctAmount := int64(math.Round(float64(actualAmount) * multiplier))
	if correctAmount >= actualAmount {
		return 0
	}
	return actualAmount - correctAmount
}

func printPlan(candidates []refundCandidate) {
	byUser := make(map[int]int64)
	var total int64
	for _, candidate := range candidates {
		byUser[candidate.UserID] += candidate.RefundAmount
		total += candidate.RefundAmount
	}
	userIDs := make([]int, 0, len(byUser))
	for userID := range byUser {
		userIDs = append(userIDs, userID)
	}
	sort.Ints(userIDs)
	fmt.Printf("marketplace multiplier refund requests: %d\n", len(candidates))
	fmt.Printf("marketplace multiplier refund users: %d\n", len(userIDs))
	fmt.Printf("marketplace multiplier refund quota: %d\n", total)
	for _, userID := range userIDs {
		fmt.Printf("  user_id=%d refund_quota=%d\n", userID, byUser[userID])
	}
}
