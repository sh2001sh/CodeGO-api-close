package channelmarket

import (
	"context"
	"errors"
	"math/big"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type SettlementInput struct {
	RequestID                 string
	ChannelID, ConsumerUserID int64
	ConsumerMicro, GrossMicro credits.Micro
	BillingSource             string
	MultiplierPPM             int64
}
type IncomeResult struct {
	Count  int           `json:"count"`
	Amount credits.Micro `json:"amount_micro"`
}

func accountTx(ctx context.Context, tx pgx.Tx, owner int64, kind string) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `SELECT id FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=$1 AND kind=$2`, owner, kind).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',$1,$2) ON CONFLICT(owner_type,owner_id,kind) DO UPDATE SET owner_id=EXCLUDED.owner_id RETURNING id`, owner, kind).Scan(&id)
	return id, err
}
func lockAccounts(ctx context.Context, tx pgx.Tx, ids ...int64) error {
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if _, err := tx.Exec(ctx, `SELECT id FROM v3_billing.accounts WHERE id=$1 FOR UPDATE`, id); err != nil {
			return err
		}
	}
	return nil
}

// AccrueTx must be called from the accepted usage ledger transaction, never
// from the request handler. Its uniqueness and fingerprint reject altered replay.
func (s *Service) AccrueTx(ctx context.Context, tx pgx.Tx, p SettlementInput) error {
	if p.RequestID == "" || p.ConsumerUserID <= 0 || p.ChannelID <= 0 || p.ConsumerMicro < 0 || p.GrossMicro < 0 || p.MultiplierPPM < 0 {
		return ErrInvalid
	}
	if p.GrossMicro == 0 {
		return nil
	}
	if s.poster == nil {
		return ErrUnavailable
	}
	if p.BillingSource == "" {
		p.BillingSource = "wallet"
	}
	var owner int64
	err := tx.QueryRow(ctx, `SELECT owner_user_id FROM v3_channelmarket.groups WHERE channel_id=$1`, p.ChannelID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	commission, net := splitAccrueCommission(p.GrossMicro)
	created, err := insertSettlementTx(ctx, tx, s.cfg.Now().Add(s.cfg.Hold), p, owner, commission, net)
	if err != nil || !created {
		return err
	}
	return s.postAccrueEntriesTx(ctx, tx, p.RequestID, owner, commission, net)
}

// splitAccrueCommission computes the platform commission and owner net share
// of gross. v2 rounds the five percent share in its integer unit, which is
// two micro-credits. Retain that quantum so replaying an old usage sample
// gives the same platform and owner split after the exact x2 conversion.
func splitAccrueCommission(gross credits.Micro) (commission, net int64) {
	commission = new(big.Int).Mul(new(big.Int).Quo(new(big.Int).Add(new(big.Int).Mul(big.NewInt(int64(gross)), big.NewInt(5)), big.NewInt(100)), big.NewInt(200)), big.NewInt(2)).Int64()
	net = int64(gross) - commission
	return commission, net
}

// insertSettlementTx inserts the settlement row, or on a replay conflict,
// verifies the existing row matches the fingerprint. The returned bool
// reports whether a new row was created (false means an identical replay,
// true an error from a mismatched fingerprint).
func insertSettlementTx(ctx context.Context, tx pgx.Tx, availableAt time.Time, p SettlementInput, owner int64, commission, net int64) (bool, error) {
	id := "usage:" + p.RequestID
	tag, err := tx.Exec(ctx, `INSERT INTO v3_channelmarket.settlements(id,request_id,channel_id,owner_user_id,consumer_user_id,billing_source,consumer_micro,gross_micro,commission_micro,fee_micro,net_micro,multiplier_ppm,available_at,group_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,0,$10,$11,$12,(SELECT id FROM v3_channelmarket.groups WHERE channel_id=$3)) ON CONFLICT(request_id) DO NOTHING`, id, p.RequestID, p.ChannelID, owner, p.ConsumerUserID, p.BillingSource, int64(p.ConsumerMicro), int64(p.GrossMicro), commission, net, p.MultiplierPPM, availableAt)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() != 0 {
		return true, nil
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT channel_id=$2 AND owner_user_id=$3 AND consumer_user_id=$4 AND billing_source=$5 AND consumer_micro=$6 AND gross_micro=$7 AND multiplier_ppm=$8 FROM v3_channelmarket.settlements WHERE request_id=$1`, p.RequestID, p.ChannelID, owner, p.ConsumerUserID, p.BillingSource, int64(p.ConsumerMicro), int64(p.GrossMicro), p.MultiplierPPM).Scan(&same)
	if err != nil {
		return false, err
	}
	if !same {
		return false, ErrConflict
	}
	return false, nil
}

// postAccrueEntriesTx posts the owner's held-earnings entry and the
// platform's commission entry for a newly created settlement.
func (s *Service) postAccrueEntriesTx(ctx context.Context, tx pgx.Tx, requestID string, owner int64, commission, net int64) error {
	pending, err := accountTx(ctx, tx, owner, "marketplace_pending")
	if err != nil {
		return err
	}
	if net > 0 {
		if _, err = s.poster.PostTx(ctx, tx, billing.Entry{AccountID: pending, Amount: credits.Micro(net), Kind: "marketplace_accrue", OperationID: "market-pending:" + requestID, RequestID: requestID, Reason: "channel owner held earnings"}); err != nil {
			return err
		}
	}
	if commission <= 0 {
		return nil
	}
	var platform int64
	if err = tx.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('platform',1,'platform_revenue') ON CONFLICT(owner_type,owner_id,kind) DO UPDATE SET owner_id=EXCLUDED.owner_id RETURNING id`).Scan(&platform); err != nil {
		return err
	}
	_, err = s.poster.PostTx(ctx, tx, billing.Entry{AccountID: platform, Amount: credits.Micro(commission), Kind: "marketplace_accrue", OperationID: "market-commission:" + requestID, RequestID: requestID, Reason: "marketplace five percent commission"})
	return err
}

// releasableSettlement is a pending settlement whose hold period has elapsed,
// locked for release.
type releasableSettlement struct {
	id         string
	owner, net int64
}

func (s *Service) ReleaseIncome(ctx context.Context, limit int) (IncomeResult, error) {
	var result IncomeResult
	if s.poster == nil {
		return result, ErrUnavailable
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	err := s.transaction(ctx, func(tx pgx.Tx) error {
		items, err := loadReleasableSettlementsTx(ctx, tx, s.cfg.Now(), limit)
		if err != nil {
			return err
		}
		for _, i := range items {
			if err := s.releaseSettlementTx(ctx, tx, i); err != nil {
				return err
			}
			result.Count++
			result.Amount, err = result.Amount.Add(credits.Micro(i.net))
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return IncomeResult{}, err
	}
	return result, nil
}

// loadReleasableSettlementsTx locks up to limit pending settlements whose
// hold period has elapsed.
func loadReleasableSettlementsTx(ctx context.Context, tx pgx.Tx, now time.Time, limit int) ([]releasableSettlement, error) {
	rows, err := tx.Query(ctx, `SELECT id,owner_user_id,net_micro-reclaimed_micro FROM v3_channelmarket.settlements WHERE status='pending' AND available_at<=$1 ORDER BY owner_user_id,id LIMIT $2 FOR UPDATE SKIP LOCKED`, now, limit)
	if err != nil {
		return nil, err
	}
	var items []releasableSettlement
	for rows.Next() {
		var i releasableSettlement
		if err = rows.Scan(&i.id, &i.owner, &i.net); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, i)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// releaseSettlementTx moves one settlement's net earnings from the owner's
// pending account to their wallet and marks the settlement released.
func (s *Service) releaseSettlementTx(ctx context.Context, tx pgx.Tx, i releasableSettlement) error {
	pending, err := accountTx(ctx, tx, i.owner, "marketplace_pending")
	if err != nil {
		return err
	}
	wallet, err := accountTx(ctx, tx, i.owner, "wallet")
	if err != nil {
		return err
	}
	if err := lockAccounts(ctx, tx, pending, wallet); err != nil {
		return err
	}
	if i.net > 0 {
		for _, entry := range []billing.Entry{{AccountID: pending, Amount: credits.Micro(-i.net), Kind: "marketplace_release", OperationID: "market-release-out:" + i.id, Reason: "release held owner earnings"}, {AccountID: wallet, Amount: credits.Micro(i.net), Kind: "marketplace_release", OperationID: "market-release-in:" + i.id, Reason: "released owner earnings"}} {
			if _, err := s.poster.PostTx(ctx, tx, entry); err != nil {
				return err
			}
		}
	}
	tag, err := tx.Exec(ctx, `UPDATE v3_channelmarket.settlements SET status='released',released_at=$2 WHERE id=$1 AND status='pending'`, i.id, s.cfg.Now())
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

type ReclaimRequest struct {
	OperationID string        `json:"operation_id"`
	OwnerIDs    []int64       `json:"owner_user_ids"`
	MaxAmount   credits.Micro `json:"max_amount_micro"`
}
