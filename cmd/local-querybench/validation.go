package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	billing "github.com/sh2001sh/new-api/internal/billing/app"
	schema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	settlement "github.com/sh2001sh/new-api/internal/marketplace/settlement"
	"gorm.io/gorm"
)

// Real PostgreSQL, real wallet transfers and ledger entries; no mocked money hook.
func validateReclaim(db *gorm.DB) error {
	const owner = 800010
	const poorOwner = 800011
	prefix := fmt.Sprintf("financial-%d", time.Now().UnixNano())
	if err := db.Exec(`INSERT INTO marketplace.settlements (id,request_id,group_id,owner_user_id,consumer_user_id,consumer_amount,platform_commission,transaction_fee,owner_net_amount,multiplier,status,created_at) SELECT ?||'-'||i,?||'-'||i,?, ?,800002,1,0,0,1,1,'released',now() FROM generate_series(1,10001) i`, prefix, prefix, prefix, owner).Error; err != nil {
		return err
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return billing.CreditMarketplaceOwnerEarningsTx(tx, owner, 10001, prefix+":credit", "marketplace_owner_release")
	}); err != nil {
		return err
	}
	var transfers atomic.Int64
	settlement.RegisterReclaimHook(func(tx *gorm.DB, owner, admin int, amount int64, key string) error {
		transfers.Add(1)
		return billing.ReclaimMarketplaceOwnerEarningsTx(tx, owner, admin, amount, key)
	})
	balance := func(id int) int {
		n, err := billing.GetUserClaudeWalletQuota(id)
		if err != nil {
			panic(err)
		}
		return n
	}
	adminStart := balance(1)
	f := settlement.ReleaseFilter{OwnerUserIDs: []int{owner}, MaxAmount: 8000, OperationID: prefix + ":exact"}
	r, err := settlement.ReclaimPending(f)
	if err != nil {
		return err
	}
	if r.Amount != 8000 || r.Count != 8000 || transfers.Load() != 1 || balance(owner) != 2001 || balance(1) != adminStart+8000 {
		return fmt.Errorf("partial reclaim mismatch")
	}
	fmt.Println("PASS exact partial amount across 5,000-row boundary: one wallet transfer")
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			repeated, e := settlement.ReclaimPending(f)
			if e == nil && repeated.Amount != 8000 {
				e = fmt.Errorf("duplicate amount mismatch")
			}
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			return e
		}
	}
	if transfers.Load() != 1 || balance(owner) != 2001 {
		return fmt.Errorf("duplicate reclaim debited twice")
	}
	f.MaxAmount = 1
	if _, e := settlement.ReclaimPending(f); e == nil {
		return fmt.Errorf("conflicting operation ID accepted")
	}
	fmt.Println("PASS eight concurrent duplicate submissions and conflicting operation ID")
	errs = make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := settlement.ReclaimPending(settlement.ReleaseFilter{OwnerUserIDs: []int{owner}, MaxAmount: 1000, OperationID: fmt.Sprintf("%s:concurrent:%d", prefix, i)})
			errs <- e
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			return e
		}
	}
	if balance(owner) != 1 || balance(1) != adminStart+10000 {
		return fmt.Errorf("concurrent amount mismatch")
	}
	fmt.Println("PASS two distinct concurrent reclaims on the same wallet")
	if _, e := settlement.ReclaimPending(settlement.ReleaseFilter{OwnerUserIDs: []int{owner}, MaxAmount: 2, OperationID: prefix + ":insufficient-income"}); e == nil {
		return fmt.Errorf("insufficient earnings accepted")
	}
	if err = db.Exec(`INSERT INTO marketplace.settlements (id,request_id,group_id,owner_user_id,consumer_user_id,consumer_amount,platform_commission,transaction_fee,owner_net_amount,multiplier,status,created_at) SELECT ?||'-extra-'||i,?||'-extra-'||i,?, ?,800002,1,0,0,1,1,'released',now() FROM generate_series(1,6000) i`, prefix, prefix, prefix, owner).Error; err != nil {
		return err
	}
	if err = db.Transaction(func(tx *gorm.DB) error {
		return billing.CreditMarketplaceOwnerEarningsTx(tx, owner, 6000, prefix+":extra-credit", "marketplace_owner_release")
	}); err != nil {
		return err
	}
	if err = db.Create(&schema.Settlement{ID: prefix + ":poor", RequestID: prefix + ":poor", GroupID: prefix, OwnerUserID: poorOwner, OwnerNetAmount: 50, Status: "released"}).Error; err != nil {
		return err
	}
	var ledgerBefore, ledgerAfter int64
	if err = db.Table("billing.ledger_entries").Count(&ledgerBefore).Error; err != nil {
		return err
	}
	f = settlement.ReleaseFilter{OwnerUserIDs: []int{owner, poorOwner}, OperationID: prefix + ":rollback"}
	if _, e := settlement.ReclaimPending(f); e == nil {
		return fmt.Errorf("insufficient wallet accepted")
	}
	if err = db.Table("billing.ledger_entries").Count(&ledgerAfter).Error; err != nil {
		return err
	}
	var auditSum int64
	if err = db.Model(&schema.Settlement{}).Where("group_id = ?", prefix).Select("sum(reclaimed_amount)").Scan(&auditSum).Error; err != nil {
		return err
	}
	if ledgerBefore != ledgerAfter || balance(owner) != 6001 || balance(1) != adminStart+10000 || auditSum != 10000 {
		return fmt.Errorf("whole-task rollback failed")
	}
	fmt.Println("PASS insufficient earnings and last-owner insufficient balance: audit, wallets and ledger rolled back")
	if err = db.Transaction(func(tx *gorm.DB) error {
		return billing.CreditMarketplaceOwnerEarningsTx(tx, poorOwner, 50, prefix+":fund-poor", "marketplace_owner_release")
	}); err != nil {
		return err
	}
	beforeResumeTransfers := transfers.Load()
	r, err = settlement.ReclaimPending(f)
	if err != nil {
		return err
	}
	if r.Amount != 6051 || len(r.OwnerAmounts) != 2 || transfers.Load()-beforeResumeTransfers != 2 || balance(owner) != 0 || balance(poorOwner) != 0 || balance(1) != adminStart+16051 {
		return fmt.Errorf("resumed task mismatch")
	}
	fmt.Println("PASS failed task resumed with the same operation ID; each owner transferred once")
	for _, q := range []string{
		"SELECT count(*) FROM billing.balance_snapshots WHERE available_balance<0 OR reserved_balance<0 OR available_balance+reserved_balance+consumed_total<>granted_total",
		"SELECT count(*) FROM billing.balance_snapshots b WHERE reserved_balance<>COALESCE((SELECT sum(reserved_amount) FROM billing.reservations r WHERE r.account_id=b.account_id AND r.status='open'),0)",
	} {
		var count int64
		if err = db.Raw(q).Scan(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("ledger conservation errors: %d", count)
		}
	}
	fmt.Println("PASS all database wallet balance and reservation conservation checks")
	return nil
}
