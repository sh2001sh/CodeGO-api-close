package ledger

import (
	"context"
	"crypto/sha256"
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
	if (e.Reason == "blind_box_batch_base" || e.Reason == "blind_box_batch_reward") && e.Kind == "reward" {
		return e.Reason
	}
	if source, _ := e.Metadata["source"].(string); source == "referral_reward" || source == "subscription_conversion" {
		return source
	}
	switch {
	case e.Kind == "referral_reward" || e.Reason == "referral_consumption_reward" || e.Reason == "referral_reward":
		return "referral_reward"
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
	return createFundingLotPolicyTx(ctx, tx, account, amount, source, origin, key, nil, false, false, []byte(`{}`), now)
}

func recordFundingEntryTx(ctx context.Context, tx pgx.Tx, e billing.Entry, before credits.Micro, now time.Time) error {
	kind, err := accountKindTx(ctx, tx, e.AccountID)
	if err != nil || kind != "wallet" {
		return err // limits/subscriptions/platform accounts are not wallet money
	}
	if e.Amount > 0 {
		if e.Reason == "wallet_peer_transfer_credit" {
			return createPeerTransferFundingLotsTx(ctx, tx, e, now)
		}
		if e.Kind == "refund" {
			if handled, err := restoreRefundFundingLotsTx(ctx, tx, e); handled || err != nil {
				return err
			}
		}
		if err := createFundingEntryLotTx(ctx, tx, e, now); err != nil {
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
	if e.Reason == conversionProviderRefundReason {
		return recordConversionRevocationEntryTx(ctx, tx, e, now)
	}
	peer := e.Reason == "wallet_peer_transfer_debit"
	request := e.RequestID
	if request == "" {
		request = "native:operation:" + e.OperationID
	}
	mode := fundingOwnerSpend
	if peer {
		mode = fundingPeerTransfer
	} else if e.Kind == "refund" {
		mode = fundingRefund
	} else if e.Kind != "usage" && e.Kind != "adjustment" {
		mode = fundingProductPurchase
	}
	if err := allocateFundingModeTx(ctx, tx, e.AccountID, request, -e.Amount, before, now, mode, fundingTopupRefundTrade(e)); err != nil {
		return err
	}
	if mode == fundingOwnerSpend || mode == fundingProductPurchase {
		return consumeWalletRewardHoldsTx(ctx, tx, e.AccountID, -e.Amount)
	}
	return nil
}

// Reject a known locked reward before taking any Redis posting reservation.
func ensureWalletTransferTx(ctx context.Context, tx pgx.Tx, e billing.Entry, now time.Time) error {
	if e.Amount >= 0 || (e.Reason != "wallet_peer_transfer_debit" && e.Kind != "refund") {
		return nil
	}
	kind, err := accountKindTx(ctx, tx, e.AccountID)
	if err != nil || kind != "wallet" {
		return err
	}
	if e.Amount == credits.Micro(math.MinInt64) {
		return credits.ErrOverflow
	}
	var available credits.Micro
	if e.Kind == "refund" {
		available, err = RefundableBalanceTx(ctx, tx, e.AccountID)
	} else {
		available, err = TransferableBalanceTx(ctx, tx, e.AccountID, now)
	}
	if err != nil {
		return err
	}
	if available < -e.Amount {
		if e.Kind == "refund" {
			return ErrWalletRewardRefundLocked
		}
		return ErrWalletRewardTransferLocked
	}
	return nil
}

type fundingLot struct {
	id, sourceAccount, source string
	remaining, ppm            int64
	nonTransferable           bool
	nonRefundable             bool
}

// Each caller is a fresh immutable ledger operation. Historical allocation
// collisions are errors, not permission to debit the same money again.
func allocateFundingTx(ctx context.Context, tx pgx.Tx, account int64, request string, amount, before credits.Micro, now time.Time) error {
	return allocateFundingModeTx(ctx, tx, account, request, amount, before, now, fundingOwnerSpend, "")
}

func allocateFundingModeTx(ctx context.Context, tx pgx.Tx, account int64, request string, amount, before credits.Micro, now time.Time, mode fundingSpendMode, topupTrade string) error {
	exists, err := fundingAllocationExistsTx(ctx, tx, request, account)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("%w: historical funding allocation %s", billing.ErrPostConflict, request)
	}
	lots, known, err := lockFundingLotsTx(ctx, tx, account, topupTrade)
	if err != nil {
		return err
	}
	// Preserve explicitly unattributed preexisting money as such. Native credits
	// always create exact-origin lots; no topup is relabeled as a legacy reward.
	deficit := amount - known
	if mode != fundingOwnerSpend {
		// A restricted transfer may need pre-provenance paid money even when
		// known reward lots exceed its amount. Never invent overdraft funds to
		// bypass permanent restrictions.
		deficit = before - known
	}
	if deficit > 0 {
		lots, err = backfillUnattributedLotsTx(ctx, tx, account, request, deficit, before, known, lots, now)
		if err != nil {
			return err
		}
	}
	return consumeFundingModeLotsTx(ctx, tx, account, request, amount, lots, now, mode)
}

// fundingAllocationExistsTx reports whether request has already been
// allocated against account.
func fundingAllocationExistsTx(ctx context.Context, tx pgx.Tx, request string, account int64) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_billing.funding_allocations WHERE request_id=$1 AND account_id=$2)`, request, account).Scan(&exists)
	return exists, err
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

// Consume only eligible source lots, preserving the original attribution.
func consumeFundingModeLotsTx(ctx context.Context, tx pgx.Tx, account int64, request string, amount credits.Micro, lots []fundingLot, now time.Time, mode fundingSpendMode) error {
	var legacyLocked credits.Micro
	if mode == fundingPeerTransfer {
		var err error
		legacyLocked, err = legacyLockedRewardAmountTx(ctx, tx, account, now)
		if err != nil {
			return err
		}
	}
	remaining := int64(amount)
	for _, lot := range lots {
		if remaining == 0 {
			break
		}
		if mode == fundingPeerTransfer && lot.nonTransferable || mode == fundingRefund && lot.nonRefundable {
			continue
		}
		if mode == fundingProductPurchase && (lot.source == "blind_box_batch_base" || lot.source == "blind_box_batch_reward") {
			continue
		}
		available := lot.remaining
		if mode == fundingPeerTransfer && lot.source == "blind_box" {
			protected := min(available, int64(legacyLocked))
			available -= protected
			legacyLocked -= credits.Micro(protected)
		}
		used := min(available, remaining)
		if used == 0 {
			continue
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
	if remaining != 0 {
		if mode == fundingPeerTransfer {
			return ErrWalletRewardTransferLocked
		}
		if mode == fundingRefund {
			return ErrWalletRewardRefundLocked
		}
		if mode == fundingProductPurchase {
			return ErrWalletAPICreditsPurchaseLocked
		}
		return fmt.Errorf("ledger: funding allocation shortfall %d of %d", remaining, amount)
	}
	return nil
}
