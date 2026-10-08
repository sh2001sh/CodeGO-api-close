package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// A conversion is not a payment. Its paid and reward slices must be separate
// entries, freezing the original revenue basis rather than today's policy.
// Paid metadata requires source, subscription_id, original_order_id,
// paid_principal_credits, reward_credits and revenue_multiplier_ppm. Reward
// metadata additionally requires non_transferable=true, non_refundable=true.
func fundingEntryPolicy(e billing.Entry) (ppm *int64, nonTransferable, nonRefundable bool, err error) {
	source := fundingSource(e)
	if source == "referral_reward" {
		zero := int64(0)
		return &zero, true, true, nil
	}
	if source != "subscription_conversion" {
		return nil, false, false, nil
	}
	sub, err := fundingMetadataInteger(e.Metadata, "subscription_id")
	if err != nil || sub <= 0 {
		return nil, false, false, errors.New("ledger: conversion requires original subscription")
	}
	order, err := fundingMetadataInteger(e.Metadata, "original_order_id")
	if err != nil || order < 0 {
		return nil, false, false, errors.New("ledger: conversion requires original order attribution")
	}
	paid, err := fundingMetadataInteger(e.Metadata, "paid_principal_credits")
	if err != nil || paid < 0 {
		return nil, false, false, errors.New("ledger: conversion requires exact paid principal")
	}
	reward, err := fundingMetadataInteger(e.Metadata, "reward_credits")
	if err != nil || reward < 0 {
		return nil, false, false, errors.New("ledger: conversion requires exact reward attribution")
	}
	if (paid > 0 && reward > 0) || (paid != int64(e.Amount) && reward != int64(e.Amount)) {
		return nil, false, false, errors.New("ledger: conversion slices must be separate and equal posted credits")
	}
	if reward > 0 {
		if e.Metadata["non_transferable"] != true || e.Metadata["non_refundable"] != true {
			return nil, false, false, errors.New("ledger: conversion reward requires permanent restrictions")
		}
		zero := int64(0)
		return &zero, true, true, nil
	}
	if order <= 0 {
		return nil, false, false, errors.New("ledger: conversion paid slice requires a paid order")
	}
	value, err := fundingMetadataInteger(e.Metadata, "revenue_multiplier_ppm")
	if err != nil || value < 0 {
		return nil, false, false, errors.New("ledger: conversion requires frozen original revenue multiplier")
	}
	return &value, false, false, nil
}

// Do not accept floating-point attribution, which can silently round bigint
// principal above 2^53. In-process callers use integers; persisted JSON may use
// json.Number or an exact decimal string.
func fundingMetadataInteger(meta map[string]any, key string) (int64, error) {
	value, ok := meta[key]
	if !ok {
		return 0, fmt.Errorf("ledger: missing funding metadata %s", key)
	}
	switch value := value.(type) {
	case int:
		return int64(value), nil
	case int64:
		return value, nil
	case credits.Micro:
		return int64(value), nil
	case json.Number:
		return value.Int64()
	case string:
		return strconv.ParseInt(value, 10, 64)
	default:
		return 0, fmt.Errorf("ledger: funding metadata %s must be an exact integer", key)
	}
}

func createFundingEntryLotTx(ctx context.Context, tx pgx.Tx, e billing.Entry, now time.Time) error {
	ppm, nonTransferable, nonRefundable, err := fundingEntryPolicy(e)
	if err != nil {
		return err
	}
	metadata := e.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	payload, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("ledger: funding metadata: %w", err)
	}
	return createFundingLotPolicyTx(ctx, tx, e.AccountID, e.Amount, fundingSource(e), e.Kind, e.OperationID, ppm, nonTransferable, nonRefundable, payload, now)
}

func createFundingLotPolicyTx(ctx context.Context, tx pgx.Tx, account int64, amount credits.Micro, source, origin, key string, frozenPPM *int64, nonTransferable, nonRefundable bool, metadata []byte, now time.Time) error {
	var ppm int64
	if frozenPPM != nil {
		ppm = *frozenPPM
	} else {
		err := tx.QueryRow(ctx, `SELECT revenue_multiplier_ppm FROM v3_billing.funding_source_policies WHERE source=$1`, source).Scan(&ppm)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	_, err := tx.Exec(ctx, `INSERT INTO v3_billing.funding_lots
	 (lot_id,source_account_id,account_id,source,reference_type,reference_id,idempotency_key,original_amount,remaining_amount,revenue_multiplier_ppm,non_transferable,non_refundable,metadata,created_at)
	 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8,$9,$10,$11,$12,$13)`, fundingID("lot", key), fmt.Sprintf("native:account:%d", account), account, source, origin, key, "native:lot:"+key, int64(amount), ppm, nonTransferable, nonRefundable, metadata, now)
	return err
}

type fundingSpendMode uint8

const (
	fundingOwnerSpend fundingSpendMode = iota
	fundingPeerTransfer
	fundingRefund
)

// RefundableBalanceTx excludes permanently restricted reward provenance.
// Order-specific refund eligibility remains the responsibility of commerce.
func RefundableBalanceTx(ctx context.Context, tx pgx.Tx, account int64) (credits.Micro, error) {
	var balance, locked credits.Micro
	if err := tx.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE id=$1 AND kind='wallet' FOR UPDATE`, account).Scan(&balance); err != nil {
		return 0, err
	}
	err := tx.QueryRow(ctx, `SELECT coalesce(sum(remaining_amount),0)::bigint FROM v3_billing.funding_lots WHERE account_id=$1 AND non_refundable`, account).Scan(&locked)
	if err != nil || balance <= locked {
		return 0, err
	}
	return balance - locked, nil
}
