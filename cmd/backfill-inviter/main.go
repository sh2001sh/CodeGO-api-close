package main

import (
	"flag"
	"fmt"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"log"
	"sort"
	"strconv"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceapp "github.com/sh2001sh/new-api/internal/commerce/app"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformstore "github.com/sh2001sh/new-api/internal/platform/store"
	"gorm.io/gorm"
)

type referralLedgerRow struct {
	LedgerID        int    `gorm:"column:ledger_id"`
	InviteeID       int    `gorm:"column:invitee_id"`
	InviterSourceID string `gorm:"column:inviter_source_id"`
	CreatedAt       int64  `gorm:"column:created_at"`
}

type conflictRecord struct {
	InviteeID  int
	InviterIDs []int
}

func main() {
	apply := flag.Bool("apply", false, "apply backfill changes to database")
	limit := flag.Int("limit", 0, "optional max number of invitees to update")
	flag.Parse()

	if err := platformstore.InitPrimaryDB(); err != nil {
		log.Fatalf("init db failed: %v", err)
	}
	if err := commerceapp.MigrateBlindBoxLegacyCredits(); err != nil {
		log.Fatalf("migrate blind box legacy credits failed: %v", err)
	}
	defer func() {
		if err := platformstore.CloseDatabases(); err != nil {
			log.Printf("close db failed: %v", err)
		}
	}()

	rows, err := loadReferralLedgerRows()
	if err != nil {
		log.Fatalf("load referral ledger rows failed: %v", err)
	}

	plans, conflicts, invalidRows := buildBackfillPlan(rows)
	fmt.Printf("referral ledger rows scanned: %d\n", len(rows))
	fmt.Printf("candidate invitees to backfill: %d\n", len(plans))
	fmt.Printf("conflicting invitees skipped: %d\n", len(conflicts))
	fmt.Printf("invalid ledger rows skipped: %d\n", invalidRows)

	if len(conflicts) > 0 {
		fmt.Println("conflicts:")
		for _, item := range conflicts {
			fmt.Printf("  invitee_id=%d inviters=%v\n", item.InviteeID, item.InviterIDs)
		}
	}

	if len(plans) == 0 {
		fmt.Println("no inviter_id backfill needed")
		return
	}

	sort.Slice(plans, func(i, j int) bool {
		if plans[i].InviteeID == plans[j].InviteeID {
			return plans[i].InviterID < plans[j].InviterID
		}
		return plans[i].InviteeID < plans[j].InviteeID
	})

	if *limit > 0 && *limit < len(plans) {
		plans = plans[:*limit]
		fmt.Printf("apply limit enabled, truncated candidate count to %d\n", len(plans))
	}

	fmt.Println("sample backfill plan:")
	for i, item := range plans {
		if i >= 10 {
			break
		}
		fmt.Printf("  invitee_id=%d -> inviter_id=%d\n", item.InviteeID, item.InviterID)
	}

	if !*apply {
		fmt.Println("dry-run only, rerun with --apply to persist changes")
		return
	}

	updated, err := applyBackfill(plans)
	if err != nil {
		log.Fatalf("apply backfill failed: %v", err)
	}
	fmt.Printf("backfill completed, updated users: %d\n", updated)
}

type backfillPlanItem struct {
	InviteeID int
	InviterID int
}

func loadReferralLedgerRows() ([]referralLedgerRow, error) {
	rows := make([]referralLedgerRow, 0)
	err := platformdb.DB.Table("point_ledgers AS pl").
		Select("pl.id AS ledger_id, pl.user_id AS invitee_id, pl.source_id AS inviter_source_id, pl.created_at").
		Joins("JOIN users AS u ON u.id = pl.user_id").
		Where("pl.source_type = ? AND pl.type = ? AND COALESCE(u.inviter_id, 0) = 0",
			billingschema.PointSourceReferralRegister,
			billingschema.PointLedgerTypeEarn,
		).
		Order("pl.created_at ASC, pl.id ASC").
		Scan(&rows).Error
	return rows, err
}

func buildBackfillPlan(rows []referralLedgerRow) ([]backfillPlanItem, []conflictRecord, int) {
	inviteeInviters := make(map[int]map[int]struct{})
	invalidRows := 0

	for _, row := range rows {
		inviterID, err := strconv.Atoi(row.InviterSourceID)
		if err != nil || inviterID <= 0 || inviterID == row.InviteeID {
			invalidRows++
			continue
		}
		if _, ok := inviteeInviters[row.InviteeID]; !ok {
			inviteeInviters[row.InviteeID] = make(map[int]struct{})
		}
		inviteeInviters[row.InviteeID][inviterID] = struct{}{}
	}

	plans := make([]backfillPlanItem, 0, len(inviteeInviters))
	conflicts := make([]conflictRecord, 0)

	for inviteeID, inviterSet := range inviteeInviters {
		if len(inviterSet) != 1 {
			inviterIDs := make([]int, 0, len(inviterSet))
			for inviterID := range inviterSet {
				inviterIDs = append(inviterIDs, inviterID)
			}
			sort.Ints(inviterIDs)
			conflicts = append(conflicts, conflictRecord{
				InviteeID:  inviteeID,
				InviterIDs: inviterIDs,
			})
			continue
		}
		for inviterID := range inviterSet {
			plans = append(plans, backfillPlanItem{
				InviteeID: inviteeID,
				InviterID: inviterID,
			})
		}
	}

	return plans, conflicts, invalidRows
}

func applyBackfill(plans []backfillPlanItem) (int64, error) {
	var updated int64
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		for _, item := range plans {
			result := tx.Model(&identityschema.User{}).
				Where("id = ? AND COALESCE(inviter_id, 0) = 0", item.InviteeID).
				Update("inviter_id", item.InviterID)
			if result.Error != nil {
				return result.Error
			}
			updated += result.RowsAffected
		}
		return nil
	})
	return updated, err
}
