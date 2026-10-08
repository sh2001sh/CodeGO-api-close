package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// Receiving a peer transfer does not manufacture a new topup or erase the
// original revenue basis. Drawn lots are copied into the recipient account;
// the transfer's fee is the tail of the sender allocation and is not copied.
func createPeerTransferFundingLotsTx(ctx context.Context, tx pgx.Tx, e billing.Entry, now time.Time) error {
	request, _ := e.Metadata["request_id"].(string)
	if request == "" || e.OperationID != "wallet-transfer:"+request+":credit" {
		return errors.New("ledger: peer credit requires matching transfer operation")
	}
	debitOperation := "wallet-transfer:" + request + ":debit"
	rows, err := tx.Query(ctx, `SELECT a.amount,l.source_account_id,l.source,l.lot_id,l.revenue_multiplier_ppm,l.non_transferable,l.non_refundable,l.metadata
	 FROM v3_billing.funding_allocations a
	 JOIN v3_billing.funding_lots l ON l.lot_id=a.lot_id
	 JOIN v3_billing.ledger_entries debit ON debit.operation_id=$1 AND debit.account_id=a.account_id
	 WHERE a.request_id='native:operation:'||$1 AND debit.amount<0 AND debit.reason='wallet_peer_transfer_debit'
	 AND debit.metadata->>'recipient_user_id'=(SELECT owner_id::text FROM v3_billing.accounts WHERE id=$2 AND owner_type='user' AND kind='wallet')
	 ORDER BY l.created_at,l.lot_id`, debitOperation, e.AccountID)
	if err != nil {
		return err
	}
	type origin struct {
		amount, ppm                    int64
		sourceAccount, source, lot     string
		nonTransferable, nonRefundable bool
		metadata                       []byte
	}
	var origins []origin
	for rows.Next() {
		var o origin
		if err := rows.Scan(&o.amount, &o.sourceAccount, &o.source, &o.lot, &o.ppm, &o.nonTransferable, &o.nonRefundable, &o.metadata); err != nil {
			rows.Close()
			return err
		}
		origins = append(origins, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	remaining := int64(e.Amount)
	for _, o := range origins {
		amount := min(o.amount, remaining)
		if amount == 0 {
			break
		}
		if o.nonTransferable {
			return ErrWalletRewardTransferLocked
		}
		metadata := map[string]any{}
		decoder := json.NewDecoder(bytes.NewReader(o.metadata))
		decoder.UseNumber()
		if err := decoder.Decode(&metadata); err != nil {
			return err
		}
		metadata["origin_funding_lot_id"], metadata["transfer_operation_id"] = o.lot, debitOperation
		if o.source == "subscription_conversion" {
			metadata["paid_principal_credits"], metadata["reward_credits"] = amount, int64(0)
		}
		payload, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		key := e.OperationID + ":" + o.lot
		_, err = tx.Exec(ctx, `INSERT INTO v3_billing.funding_lots
		 (lot_id,source_account_id,account_id,source,reference_type,reference_id,idempotency_key,original_amount,remaining_amount,revenue_multiplier_ppm,non_transferable,non_refundable,metadata,created_at)
		 VALUES($1,$2,$3,$4,'peer_transfer',$5,$6,$7,$7,$8,false,$9,$10,$11)`, fundingID("lot", key), o.sourceAccount, e.AccountID, o.source, e.OperationID, "native:lot:"+key, amount, o.ppm, o.nonRefundable, payload, now)
		if err != nil {
			return err
		}
		remaining -= amount
	}
	if remaining != 0 {
		return fmt.Errorf("ledger: peer credit missing original funding %d", remaining)
	}
	return nil
}

// A provider-confirmed failed refund restores its reserved original lots.
// Crediting a new 'other' lot would lose revenue and refund origin attribution.
func restoreRefundFundingLotsTx(ctx context.Context, tx pgx.Tx, e billing.Entry) (bool, error) {
	no, _ := e.Metadata["refund_no"].(string)
	if no == "" || e.OperationID != "user-refund:"+no+":release" {
		return false, nil // unrelated historical positive refund entries
	}
	rows, err := tx.Query(ctx, `SELECT a.lot_id,a.amount FROM v3_billing.funding_allocations a
	 JOIN v3_billing.funding_lots l ON l.lot_id=a.lot_id
	 WHERE a.account_id=$1 AND a.request_id=$2 ORDER BY l.created_at,l.lot_id FOR UPDATE OF l`, e.AccountID, "native:operation:user-refund:"+no+":reserve")
	if err != nil {
		return true, err
	}
	type restore struct {
		lot    string
		amount credits.Micro
	}
	var restores []restore
	var total credits.Micro
	for rows.Next() {
		var r restore
		if err := rows.Scan(&r.lot, &r.amount); err != nil {
			rows.Close()
			return true, err
		}
		total, err = total.Add(r.amount)
		if err != nil {
			rows.Close()
			return true, err
		}
		restores = append(restores, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return true, err
	}
	if total != e.Amount {
		return true, errors.New("ledger: refund release differs from reserved original funding")
	}
	if err := ensureRefundOriginNotRevokedTx(ctx, tx, e.AccountID, "native:operation:user-refund:"+no+":reserve"); err != nil {
		return true, err
	}
	for _, r := range restores {
		tag, err := tx.Exec(ctx, `UPDATE v3_billing.funding_lots SET remaining_amount=remaining_amount+$2
		 WHERE lot_id=$1 AND original_amount-remaining_amount >= $2`, r.lot, int64(r.amount))
		if err != nil {
			return true, err
		}
		if tag.RowsAffected() != 1 {
			return true, billing.ErrPostConflict
		}
	}
	return true, nil
}
