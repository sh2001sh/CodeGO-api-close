package marketplace

import (
	"context"
	"errors"
	"math/big"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// RecordDiscountUsageTx audits a settled consumption discount in the ledger
// event transaction so retries cannot record or consume the card twice.
func (s *Service) RecordDiscountUsageTx(ctx context.Context, tx pgx.Tx, userID, propID, channelID int64, requestID string, before, after credits.Micro) error {
	if userID <= 0 || propID <= 0 || channelID <= 0 || requestID == "" || before < 0 || after < 0 || after > before {
		return ErrInvalidInput
	}
	// Serialize replays before reading the audit, without taking an identity
	// lock after zero-hour state locks held by the billing consumer.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "discount-usage:"+requestID); err != nil {
		return err
	}
	replayed, err := discountUsageReplayMatchesTx(ctx, tx, requestID, userID, propID, channelID, before, after)
	if err != nil || replayed {
		return err
	}
	card, err := lockDiscountCardTx(ctx, tx, propID, userID)
	if err != nil {
		return err
	}
	discount := before - after
	remaining, newUsed, err := applyDiscountCardUsage(card, discount)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_props SET used_discount_micro=$2,status=CASE WHEN $4::bigint>0 AND $2>=$4 THEN 'used' ELSE status END,updated_at=$3 WHERE id=$1`, propID, newUsed, s.cfg.Now(), card.maximum); err != nil {
		return err
	}
	rate := discountRatePPM(discount, before)
	_, err = tx.Exec(ctx, `INSERT INTO v3_marketplace.blind_box_prop_discount_usages(user_id,prop_id,channel_id,request_id,before_micro,after_micro,discount_micro,discount_rate_ppm,multiplier_ppm,effective_multiplier_ppm,remaining_micro,prop_title,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, userID, propID, channelID, requestID, before, after, discount, rate, card.factor, 1000000-rate, remaining, card.title, s.cfg.Now())
	if err != nil {
		return err
	}
	return s.profileTx(ctx, tx, userID)
}

// discountUsageReplayMatchesTx checks for a prior audit row with this
// request id. It returns (true, nil) when the prior row matches the current
// call exactly (a safe no-op replay), and ErrConflict when it diverges.
func discountUsageReplayMatchesTx(ctx context.Context, tx pgx.Tx, requestID string, userID, propID, channelID int64, before, after credits.Micro) (bool, error) {
	var oldUser, oldProp, oldChannel int64
	var oldBefore, oldAfter credits.Micro
	err := tx.QueryRow(ctx, `SELECT user_id,prop_id,channel_id,before_micro,after_micro FROM v3_marketplace.blind_box_prop_discount_usages WHERE request_id=$1`, requestID).Scan(&oldUser, &oldProp, &oldChannel, &oldBefore, &oldAfter)
	if err == nil {
		if oldUser != userID || oldProp != propID || oldChannel != channelID || oldBefore != before || oldAfter != after {
			return false, ErrConflict
		}
		return true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return false, err
}

// discountCard is the locked multiplier-card row backing a discount usage.
type discountCard struct {
	factor          int64
	maximum, used   credits.Micro
	title, propType string
}

// lockDiscountCardTx locks and loads the discount card, validating that it
// belongs to userID and is a multiplier card. Legacy unlimited consumption
// cards (explicitly unlimited in v2) have their cap cleared.
func lockDiscountCardTx(ctx context.Context, tx pgx.Tx, propID, userID int64) (discountCard, error) {
	var card discountCard
	var owner int64
	var kind string
	err := tx.QueryRow(ctx, `SELECT user_id,kind,title,multiplier_ppm,max_discount_micro,used_discount_micro,prop_type FROM v3_marketplace.blind_box_props WHERE id=$1 FOR UPDATE`, propID).Scan(&owner, &kind, &card.title, &card.factor, &card.maximum, &card.used, &card.propType)
	if errors.Is(err, pgx.ErrNoRows) {
		return card, ErrNotFound
	}
	if err != nil {
		return card, err
	}
	if owner != userID || kind != "multiplier" {
		return card, ErrConflict
	}
	if card.propType == "consume_discount_95" || card.propType == "consume_discount_90" || card.propType == "consume_discount_10" {
		card.maximum = 0
	}
	return card, nil
}

// applyDiscountCardUsage validates the discount against the card's remaining
// cap and returns the new remaining balance and cumulative used amount.
func applyDiscountCardUsage(card discountCard, discount credits.Micro) (remaining, newUsed credits.Micro, err error) {
	if card.maximum > 0 && discount > card.maximum-card.used {
		return 0, 0, ErrConflict
	}
	newUsed, err = card.used.Add(discount)
	if err != nil {
		return 0, 0, err
	}
	if card.maximum > 0 {
		remaining = card.maximum - newUsed
	}
	return remaining, newUsed, nil
}

// discountRatePPM computes the effective discount rate in parts-per-million
// of the pre-discount amount.
func discountRatePPM(discount, before credits.Micro) int64 {
	if before <= 0 {
		return 0
	}
	n := new(big.Int).Mul(big.NewInt(int64(discount)), big.NewInt(1000000))
	return n.Quo(n, big.NewInt(int64(before))).Int64()
}
