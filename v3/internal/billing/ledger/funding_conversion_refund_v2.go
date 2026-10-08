package ledger

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

const conversionProviderRefundReason = "subscription_conversion_provider_refund"

var (
	ErrConversionOriginUnknown = errors.New("ledger: original conversion funding requires review")
	ErrConversionOriginChanged = errors.New("ledger: conversion origin accounts changed; retry after transfers settle")
	ErrConversionOriginRevoked = errors.New("ledger: revoked conversion refund release requires review")
)

type conversionRevocationLot struct {
	account  int64
	original credits.Micro
	root     bool
	fundingLot
}

// RevokeSubscriptionConversionTx follows a verified original provider refund.
// It reclaims only this conversion's remaining paid/reward funding, including
// peer-transferred descendants, and never debits unrelated principal or debt.
// A positive alreadyConsumed is explicit exposure for commerce payment review;
// it is not permission for an owner to obtain a full cash refund.
func (p *Poster) RevokeSubscriptionConversionTx(ctx context.Context, tx pgx.Tx, subscriptionID, originalOrderID int64, operationID string) (remainingRevoked, alreadyConsumed credits.Micro, err error) {
	if subscriptionID <= 0 || originalOrderID <= 0 || operationID == "" {
		return 0, 0, errors.New("ledger: conversion revocation requires subscription, original order and operation")
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0)),pg_advisory_xact_lock(hashtextextended($2,0))`,
		"conversion-revoke-operation:"+operationID, fmt.Sprintf("conversion-revoke-origin:%d:%d", subscriptionID, originalOrderID)); err != nil {
		return 0, 0, err
	}
	remainingRevoked, alreadyConsumed, found, err := conversionRevocationReceiptTx(ctx, tx, subscriptionID, originalOrderID, operationID)
	if err != nil || found {
		return remainingRevoked, alreadyConsumed, err
	}
	accounts, err := lockConversionOriginAccountsTx(ctx, tx, subscriptionID, originalOrderID)
	if err != nil {
		return 0, 0, err
	}
	lots, original, remainingRevoked, err := conversionRevocationLotsTx(ctx, tx, subscriptionID, originalOrderID)
	if err != nil {
		return 0, 0, err
	}
	if original <= 0 || remainingRevoked > original {
		return 0, 0, ErrConversionOriginUnknown
	}
	alreadyConsumed = original - remainingRevoked
	if err := p.postConversionRevocationAccountsTx(ctx, tx, accounts, lots, subscriptionID, originalOrderID, operationID); err != nil {
		return 0, 0, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_billing.subscription_conversion_revocations
	 (operation_id,subscription_id,original_order_id,revoked_credits,consumed_credits,created_at) VALUES($1,$2,$3,$4,$5,$6)`,
		operationID, subscriptionID, originalOrderID, int64(remainingRevoked), int64(alreadyConsumed), p.now())
	return remainingRevoked, alreadyConsumed, err
}

func conversionRevocationReceiptTx(ctx context.Context, tx pgx.Tx, subscription, order int64, operation string) (revoked, consumed credits.Micro, found bool, err error) {
	var recordedSub, recordedOrder int64
	var recordedOperation string
	err = tx.QueryRow(ctx, `SELECT operation_id,subscription_id,original_order_id,revoked_credits,consumed_credits
	 FROM v3_billing.subscription_conversion_revocations WHERE operation_id=$1 OR (subscription_id=$2 AND original_order_id=$3)
	 ORDER BY (operation_id=$1) DESC LIMIT 1`, operation, subscription, order).Scan(&recordedOperation, &recordedSub, &recordedOrder, &revoked, &consumed)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	if recordedOperation != operation || recordedSub != subscription || recordedOrder != order {
		return 0, 0, false, billing.ErrPostConflict
	}
	return revoked, consumed, true, nil
}

func conversionOriginAccountIDsTx(ctx context.Context, tx pgx.Tx, subscription, order int64) ([]int64, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT coalesce(account_id,0) FROM v3_billing.funding_lots
	 WHERE source='subscription_conversion' AND metadata->>'subscription_id'=$1 AND metadata->>'original_order_id'=$2
	 ORDER BY coalesce(account_id,0)`, strconv.FormatInt(subscription, 10), strconv.FormatInt(order, 10))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

// Transfers lock their participating accounts in id order. Discover and lock
// the same origin accounts in that order, then verify none escaped while the
// locks were acquired. Once every origin account is locked, no transfer can
// create another descendant before this transaction commits.
func lockConversionOriginAccountsTx(ctx context.Context, tx pgx.Tx, subscription, order int64) ([]int64, error) {
	accounts, err := conversionOriginAccountIDsTx(ctx, tx, subscription, order)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 || accounts[0] <= 0 {
		return nil, ErrConversionOriginUnknown
	}
	rows, err := tx.Query(ctx, `SELECT id FROM v3_billing.accounts
	 WHERE id=ANY($1::bigint[]) AND owner_type='user' AND kind='wallet' ORDER BY id FOR UPDATE`, accounts)
	if err != nil {
		return nil, err
	}
	locked, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	if !slices.Equal(accounts, locked) {
		return nil, ErrConversionOriginUnknown
	}
	current, err := conversionOriginAccountIDsTx(ctx, tx, subscription, order)
	if err != nil {
		return nil, err
	}
	if !slices.Equal(accounts, current) {
		return nil, ErrConversionOriginChanged
	}
	return accounts, nil
}

func conversionRevocationLotsTx(ctx context.Context, tx pgx.Tx, subscription, order int64) ([]conversionRevocationLot, credits.Micro, credits.Micro, error) {
	rows, err := tx.Query(ctx, `SELECT account_id,original_amount,reference_type<>'peer_transfer',lot_id,source_account_id,source,remaining_amount,revenue_multiplier_ppm
	 FROM v3_billing.funding_lots WHERE source='subscription_conversion'
	 AND metadata->>'subscription_id'=$1 AND metadata->>'original_order_id'=$2 ORDER BY account_id,created_at,lot_id FOR UPDATE`, strconv.FormatInt(subscription, 10), strconv.FormatInt(order, 10))
	if err != nil {
		return nil, 0, 0, err
	}
	lots, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (conversionRevocationLot, error) {
		var lot conversionRevocationLot
		err := row.Scan(&lot.account, &lot.original, &lot.root, &lot.id, &lot.sourceAccount, &lot.source, &lot.remaining, &lot.ppm)
		return lot, err
	})
	if err != nil {
		return nil, 0, 0, err
	}
	var original, remaining credits.Micro
	for _, lot := range lots {
		if lot.root {
			original, err = original.Add(lot.original)
			if err != nil {
				return nil, 0, 0, err
			}
		}
		remaining, err = remaining.Add(credits.Micro(lot.remaining))
		if err != nil {
			return nil, 0, 0, err
		}
	}
	return lots, original, remaining, nil
}

func (p *Poster) postConversionRevocationAccountsTx(ctx context.Context, tx pgx.Tx, accounts []int64, lots []conversionRevocationLot, subscription, order int64, operation string) error {
	amounts := make(map[int64]credits.Micro, len(accounts))
	for _, lot := range lots {
		var err error
		amounts[lot.account], err = amounts[lot.account].Add(credits.Micro(lot.remaining))
		if err != nil {
			return err
		}
	}
	for _, account := range accounts {
		if amounts[account] == 0 {
			continue
		}
		_, err := p.PostTx(ctx, tx, billing.Entry{AccountID: account, Amount: -amounts[account], Kind: "transfer",
			OperationID: fmt.Sprintf("%s:account:%d", operation, account), Reason: conversionProviderRefundReason,
			Metadata: map[string]any{"subscription_id": subscription, "original_order_id": order, "conversion_revocation_operation": operation}})
		if err != nil {
			return err
		}
	}
	return nil
}

// Exact-origin audit hook: the normal wallet FIFO must not consume another
// topup merely because that unrelated principal happens to be older.
func recordConversionRevocationEntryTx(ctx context.Context, tx pgx.Tx, e billing.Entry, now time.Time) error {
	subscription, err := fundingMetadataInteger(e.Metadata, "subscription_id")
	if err != nil || subscription <= 0 {
		return ErrConversionOriginUnknown
	}
	order, err := fundingMetadataInteger(e.Metadata, "original_order_id")
	if err != nil || order <= 0 {
		return ErrConversionOriginUnknown
	}
	lots, _, _, err := conversionRevocationLotsTx(ctx, tx, subscription, order)
	if err != nil {
		return err
	}
	var selected []fundingLot
	var remaining credits.Micro
	for _, lot := range lots {
		if lot.account != e.AccountID {
			continue
		}
		remaining, err = remaining.Add(credits.Micro(lot.remaining))
		if err != nil {
			return err
		}
		selected = append(selected, lot.fundingLot)
	}
	if remaining != -e.Amount {
		return ErrConversionOriginChanged
	}
	var balance credits.Micro
	if err := tx.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE id=$1`, e.AccountID).Scan(&balance); err != nil {
		return err
	}
	if balance < remaining {
		return ErrConversionOriginUnknown // never take unrelated money into debt
	}
	request := "native:operation:" + e.OperationID
	if exists, err := fundingAllocationExistsTx(ctx, tx, request, e.AccountID); err != nil {
		return err
	} else if exists {
		return billing.ErrPostConflict
	}
	return consumeFundingModeLotsTx(ctx, tx, e.AccountID, request, remaining, selected, now, fundingOwnerSpend)
}

// Called while the refund release already owns its wallet and lot locks. A
// provider revocation must never be undone by a later failed refund release.
func ensureRefundOriginNotRevokedTx(ctx context.Context, tx pgx.Tx, account int64, request string) error {
	var revoked bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_billing.funding_allocations a
	 JOIN v3_billing.funding_lots l ON l.lot_id=a.lot_id AND l.source='subscription_conversion'
	 JOIN v3_billing.subscription_conversion_revocations r
	 ON r.subscription_id::text=l.metadata->>'subscription_id' AND r.original_order_id::text=l.metadata->>'original_order_id'
	 WHERE a.account_id=$1 AND a.request_id=$2)`, account, request).Scan(&revoked)
	if err != nil {
		return err
	}
	if revoked {
		return ErrConversionOriginRevoked
	}
	return nil
}
