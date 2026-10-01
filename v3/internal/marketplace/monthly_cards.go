package marketplace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const monthlyCardType = "monthly_pass_multiplier"

// GrantMonthlyCardTx grants monthly-pass time in the caller's transaction.
// The source reference is the receipt identity, including after the card expires.
func (s *Service) GrantMonthlyCardTx(ctx context.Context, tx pgx.Tx, userID, durationSeconds int64, benefitReference string) error {
	if tx == nil || userID <= 0 || durationSeconds <= 0 || durationSeconds > 366*24*60*60 || strings.TrimSpace(benefitReference) == "" {
		return ErrInvalidInput
	}
	if err := lockUser(ctx, tx, userID); err != nil {
		return err
	}
	input := struct {
		Reference string `json:"reference"`
		Duration  int64  `json:"duration_seconds"`
	}{benefitReference, durationSeconds}
	digest := sha256.Sum256([]byte(benefitReference))
	requestID := hex.EncodeToString(digest[:])
	receipt := input
	found, err := replay(ctx, tx, userID, "monthly_card", requestID, input, &receipt)
	if err != nil || found {
		return err
	}
	now := s.cfg.Now()
	cards, err := monthlyCardsTx(ctx, tx, userID)
	if err != nil {
		return err
	}
	alreadyGranted, err := monthlyCardAlreadyCovers(ctx, tx, userID, benefitReference, cards)
	if err != nil {
		return err
	}
	if alreadyGranted {
		return remember(ctx, tx, userID, "monthly_card", requestID, input, receipt)
	}
	if err := s.mergeMonthlyCardsTx(ctx, tx, userID, cards, durationSeconds, benefitReference, now); err != nil {
		return err
	}
	if err := s.profileTx(ctx, tx, userID); err != nil {
		return err
	}
	return remember(ctx, tx, userID, "monthly_card", requestID, input, receipt)
}

// monthlyCardAlreadyCovers reports whether benefitReference is already
// recorded against one of the user's existing cards, directly or via a
// retained v2 pipe-separated source reference.
func monthlyCardAlreadyCovers(ctx context.Context, tx pgx.Tx, userID int64, benefitReference string, cards []monthlyCard) (bool, error) {
	rows, err := tx.Query(ctx, `SELECT response->>'reference' FROM v3_marketplace.operations WHERE user_id=$1 AND kind='monthly_card' AND response ? 'reference' ORDER BY length(response->>'reference') DESC`, userID)
	if err != nil {
		return false, err
	}
	var currentSources []string
	for rows.Next() {
		var source string
		if err := rows.Scan(&source); err != nil {
			rows.Close()
			return false, err
		}
		currentSources = append(currentSources, source)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	for _, card := range cards {
		// Retained v2 cards carry pipe-separated source references. Their exact
		// grant durations were not recorded, so create the v3 receipt on retry.
		// Remove v3 sources first: each is opaque and can include the delimiter.
		stored := "|" + card.reference + "|"
		for _, source := range currentSources {
			stored = strings.ReplaceAll(stored, "|"+source+"|", "|")
		}
		if stored == "|"+benefitReference+"|" || (!strings.Contains(benefitReference, "|") && monthlyHasReference(stored, benefitReference)) {
			return true, nil
		}
	}
	return false, nil
}

type monthlyCard struct {
	id, duration, remaining int64
	status, reference       string
	expiry                  *time.Time
}

func monthlyCardsTx(ctx context.Context, tx pgx.Tx, userID int64) ([]monthlyCard, error) {
	rows, err := tx.Query(ctx, `SELECT id,duration_seconds,remaining_seconds,status,benefit_reference,expires_at FROM v3_marketplace.blind_box_props WHERE user_id=$1 AND prop_type=$2 ORDER BY (status='active') DESC,id FOR UPDATE`, userID, monthlyCardType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cards []monthlyCard
	for rows.Next() {
		var card monthlyCard
		if err := rows.Scan(&card.id, &card.duration, &card.remaining, &card.status, &card.reference, &card.expiry); err != nil {
			return nil, err
		}
		cards = append(cards, card)
	}
	return cards, rows.Err()
}

func (s *Service) mergeMonthlyCardsTx(ctx context.Context, tx pgx.Tx, userID int64, cards []monthlyCard, duration int64, reference string, now time.Time) error {
	eligible, remaining, active, err := collectEligibleMonthlyCardsTx(ctx, tx, cards, duration, now)
	if err != nil {
		return err
	}
	if len(eligible) == 0 {
		_, err := tx.Exec(ctx, `INSERT INTO v3_marketplace.blind_box_props(user_id,kind,title,status,multiplier_ppm,duration_seconds,remaining_seconds,prop_type,benefit_reference,created_at,updated_at) VALUES($1,'multiplier','月卡 0.1 倍率卡','available',100000,$2,$2,$3,$4,$5,$5)`, userID, duration, monthlyCardType, reference, now)
		return err
	}
	return applyMergedMonthlyCardTx(ctx, tx, eligible, remaining, active, duration, reference, now)
}

const monthlyCardMaxSeconds = math.MaxInt64 / int64(time.Second)

// collectEligibleMonthlyCardsTx expires any stale active card, then scans the
// remainder for cards eligible to receive the new grant, returning the
// combined remaining duration and whether any eligible card is currently active.
func collectEligibleMonthlyCardsTx(ctx context.Context, tx pgx.Tx, cards []monthlyCard, duration int64, now time.Time) ([]monthlyCard, time.Duration, bool, error) {
	remaining := time.Duration(duration) * time.Second
	var eligible []monthlyCard
	active := false
	for _, card := range cards {
		if card.status == "active" && (card.expiry == nil || !card.expiry.After(now)) {
			if _, err := tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_props SET status='expired',remaining_seconds=0,updated_at=$2 WHERE id=$1`, card.id, now); err != nil {
				return nil, 0, false, err
			}
			continue
		}
		if card.status != "active" && card.status != "paused" && card.status != "available" {
			continue
		}
		seconds := card.remaining
		var added time.Duration
		if card.status == "active" {
			// Compare before Sub: time.Sub saturates on an overflowing duration.
			if card.expiry.After(now.Add(time.Duration(monthlyCardMaxSeconds) * time.Second)) {
				return nil, 0, false, ErrConflict
			}
			added = card.expiry.Sub(now)
			seconds = 0
			active = true
		} else if seconds <= 0 {
			seconds = card.duration
		}
		if seconds < 0 || seconds > monthlyCardMaxSeconds || card.duration > monthlyCardMaxSeconds {
			return nil, 0, false, ErrConflict
		}
		if card.status != "active" {
			added = time.Duration(seconds) * time.Second
		}
		if added > time.Duration(monthlyCardMaxSeconds)*time.Second-remaining {
			return nil, 0, false, ErrConflict
		}
		remaining += added
		eligible = append(eligible, card)
	}
	return eligible, remaining, active, nil
}

// applyMergedMonthlyCardTx writes the combined remaining duration onto the
// primary (first eligible) card and marks every other eligible card used.
func applyMergedMonthlyCardTx(ctx context.Context, tx pgx.Tx, eligible []monthlyCard, remaining time.Duration, active bool, duration int64, reference string, now time.Time) error {
	primary := eligible[0]
	status := primary.status
	var expiry *time.Time
	if active {
		status = "active"
		end := now.Add(remaining)
		expiry = &end
	}
	if primary.reference != "" {
		reference = primary.reference + "|" + reference
	}
	seconds := int64(remaining / time.Second)
	if remaining%time.Second != 0 {
		seconds++
	}
	_, err := tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_props SET status=$2,multiplier_ppm=100000,duration_seconds=$3,remaining_seconds=$4,expires_at=$5,benefit_reference=$6,updated_at=$7 WHERE id=$1`, primary.id, status, max(primary.duration, duration), seconds, expiry, reference, now)
	if err != nil {
		return err
	}
	for _, card := range eligible[1:] {
		if _, err := tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_props SET status='used',remaining_seconds=0,used_at=$2,updated_at=$2 WHERE id=$1`, card.id, now); err != nil {
			return err
		}
	}
	return nil
}

func monthlyHasReference(stored, reference string) bool {
	for _, item := range strings.Split(stored, "|") {
		if strings.TrimSpace(item) == reference {
			return true
		}
	}
	return false
}
