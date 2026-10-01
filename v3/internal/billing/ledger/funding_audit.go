package ledger

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// Account locks always precede provenance rows. Commerce may already hold
// several account locks; funding routines never reverse that order.
func accountKindTx(ctx context.Context, tx pgx.Tx, id int64) (string, error) {
	var kind string
	err := tx.QueryRow(ctx, `SELECT kind FROM v3_billing.accounts WHERE id=$1`, id).Scan(&kind)
	return kind, err
}

func fundingID(parts ...string) string {
	var b strings.Builder
	for _, part := range parts {
		_, _ = fmt.Fprintf(&b, "%d:%s", len(part), part)
	}
	return fmt.Sprintf("native:%x", sha256.Sum256([]byte(b.String())))
}

func fundingSource(e billing.Entry) string {
	switch {
	case e.Kind == "topup":
		return "topup"
	case e.Kind == "reward" && e.Reason == "blind_box_reward":
		return "blind_box"
	case e.Kind == "subscription_grant":
		return "subscription"
	case e.Kind == "opening":
		return "legacy_unattributed"
	default:
		return "other"
	}
}

func createFundingLotTx(ctx context.Context, tx pgx.Tx, account int64, amount credits.Micro, source, origin, key string, now time.Time) error {
	var ppm int64
	err := tx.QueryRow(ctx, `SELECT revenue_multiplier_ppm FROM v3_billing.funding_source_policies WHERE source=$1`, source).Scan(&ppm)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_billing.funding_lots
	 (lot_id,source_account_id,account_id,source,reference_type,reference_id,idempotency_key,original_amount,remaining_amount,revenue_multiplier_ppm,created_at)
	 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8,$9,$10)`, fundingID("lot", key), fmt.Sprintf("native:account:%d", account), account, source, origin, key, "native:lot:"+key, int64(amount), ppm, now)
	return err
}

func recordFundingEntryTx(ctx context.Context, tx pgx.Tx, e billing.Entry, before credits.Micro, now time.Time) error {
	kind, err := accountKindTx(ctx, tx, e.AccountID)
	if err != nil || kind != "wallet" {
		return err // limits/subscriptions/platform accounts are not wallet money
	}
	if e.Amount > 0 {
		if err := createFundingLotTx(ctx, tx, e.AccountID, e.Amount, fundingSource(e), e.Kind, e.OperationID, now); err != nil {
			return err
		}
		if e.Kind == "reward" && e.Reason == "blind_box_reward" {
			var uid int64
			if err := tx.QueryRow(ctx, `SELECT owner_id FROM v3_billing.accounts WHERE id=$1 AND owner_type='user'`, e.AccountID).Scan(&uid); err != nil {
				return err
			}
			return CreateWalletRewardHoldTx(ctx, tx, e.AccountID, uid, e.Amount, e.OperationID, now)
		}
		return nil
	}
	if e.Amount == credits.Micro(math.MinInt64) {
		return credits.ErrOverflow
	}
	peer := e.Reason == "wallet_peer_transfer_debit"
	request := e.RequestID
	if request == "" {
		request = "native:operation:" + e.OperationID
	}
	if err := allocateFundingTx(ctx, tx, e.AccountID, request, -e.Amount, before, now); err != nil {
		return err
	}
	if !peer {
		return consumeWalletRewardHoldsTx(ctx, tx, e.AccountID, -e.Amount)
	}
	return nil
}

// Reject a known locked reward before taking any Redis posting reservation.
func ensureWalletTransferTx(ctx context.Context, tx pgx.Tx, e billing.Entry, now time.Time) error {
	if e.Amount >= 0 || e.Reason != "wallet_peer_transfer_debit" {
		return nil
	}
	if e.Amount == credits.Micro(math.MinInt64) {
		return credits.ErrOverflow
	}
	available, err := TransferableBalanceTx(ctx, tx, e.AccountID, now)
	if err != nil {
		return err
	}
	if available < -e.Amount {
		return ErrWalletRewardTransferLocked
	}
	return nil
}

type fundingLot struct {
	id, sourceAccount, source string
	remaining, ppm            int64
}

// Each caller is a fresh immutable ledger operation. Historical allocation
// collisions are errors, not permission to debit the same money again.
func allocateFundingTx(ctx context.Context, tx pgx.Tx, account int64, request string, amount, before credits.Micro, now time.Time) error {
	exists, err := fundingAllocationExistsTx(ctx, tx, request, account)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("%w: historical funding allocation %s", billing.ErrPostConflict, request)
	}
	lots, known, err := lockFundingLotsTx(ctx, tx, account)
	if err != nil {
		return err
	}
	// Preserve explicitly unattributed preexisting money as such. Native credits
	// always create exact-origin lots; no topup is relabeled as a legacy reward.
	if deficit := amount - known; deficit > 0 {
		lots, err = backfillUnattributedLotsTx(ctx, tx, account, request, deficit, before, known, lots, now)
		if err != nil {
			return err
		}
	}
	return consumeFundingLotsTx(ctx, tx, account, request, amount, lots, now)
}

// fundingAllocationExistsTx reports whether request has already been
// allocated against account.
func fundingAllocationExistsTx(ctx context.Context, tx pgx.Tx, request string, account int64) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_billing.funding_allocations WHERE request_id=$1 AND account_id=$2)`, request, account).Scan(&exists)
	return exists, err
}

// lockFundingLotsTx locks account's open funding lots in allocation order
// and returns them along with their total remaining amount.
func lockFundingLotsTx(ctx context.Context, tx pgx.Tx, account int64) ([]fundingLot, credits.Micro, error) {
	rows, err := tx.Query(ctx, `SELECT lot_id,source_account_id,source,remaining_amount,revenue_multiplier_ppm FROM v3_billing.funding_lots
	 WHERE account_id=$1 AND remaining_amount>0 ORDER BY created_at,lot_id FOR UPDATE`, account)
	if err != nil {
		return nil, 0, err
	}
	var lots []fundingLot
	var known credits.Micro
	for rows.Next() {
		var lot fundingLot
		if err := rows.Scan(&lot.id, &lot.sourceAccount, &lot.source, &lot.remaining, &lot.ppm); err != nil {
			rows.Close()
			return nil, 0, err
		}
		known, err = known.Add(credits.Micro(lot.remaining))
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		lots = append(lots, lot)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return lots, known, nil
}

// backfillUnattributedLotsTx creates lots to cover a deficit between what
// is being allocated and the account's known lot total: the portion backed
// by balance predating lot tracking becomes "legacy_unattributed", and any
// remainder becomes "other"/"gateway_overdraft". It returns lots with the
// new entries appended.
func backfillUnattributedLotsTx(ctx context.Context, tx pgx.Tx, account int64, request string, deficit, before, known credits.Micro, lots []fundingLot, now time.Time) ([]fundingLot, error) {
	var backed credits.Micro
	if before > known {
		backed = min(deficit, before-known)
	}
	for _, missing := range []struct {
		amount         credits.Micro
		source, origin string
	}{{backed, "legacy_unattributed", "unattributed_balance"}, {deficit - backed, "other", "gateway_overdraft"}} {
		if missing.amount == 0 {
			continue
		}
		key := fmt.Sprintf("%s:%d:%s", missing.origin, account, request)
		if err := createFundingLotTx(ctx, tx, account, missing.amount, missing.source, missing.origin, key, now); err != nil {
			return nil, err
		}
		lot := fundingLot{id: fundingID("lot", key), sourceAccount: fmt.Sprintf("native:account:%d", account), source: missing.source, remaining: int64(missing.amount)}
		if err := tx.QueryRow(ctx, `SELECT revenue_multiplier_ppm FROM v3_billing.funding_lots WHERE lot_id=$1`, lot.id).Scan(&lot.ppm); err != nil {
			return nil, err
		}
		lots = append(lots, lot)
	}
	return lots, nil
}

// consumeFundingLotsTx draws amount from lots in order, recording each draw
// as a funding allocation.
func consumeFundingLotsTx(ctx context.Context, tx pgx.Tx, account int64, request string, amount credits.Micro, lots []fundingLot, now time.Time) error {
	remaining := int64(amount)
	for _, lot := range lots {
		used := min(lot.remaining, remaining)
		if used == 0 {
			break
		}
		if _, err := tx.Exec(ctx, `UPDATE v3_billing.funding_lots SET remaining_amount=remaining_amount-$2 WHERE lot_id=$1`, lot.id, used); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO v3_billing.funding_allocations
		 (allocation_id,request_id,lot_id,source_account_id,account_id,source,amount,revenue_multiplier_ppm,created_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, fundingID("allocation", request, lot.id), request, lot.id, lot.sourceAccount, account, lot.source, used, lot.ppm, now); err != nil {
			return err
		}
		remaining -= used
	}
	return nil
}
