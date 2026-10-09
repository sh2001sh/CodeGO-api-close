package channelmarket

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
	"github.com/sh2001sh/new-api/v3/pkg/exactfactor"
)

type accrualBatchPoster interface {
	PostAccrualsTx(context.Context, pgx.Tx, []billing.Entry, []int64) error
}

type accrualGroup struct {
	id    string
	owner int64
}

// AccrueBatchTx shares the accepted usage transaction and retains one exact
// settlement per request. Group lookups, replay checks and credits are batched.
func (s *Service) AccrueBatchTx(ctx context.Context, tx pgx.Tx, input []SettlementInput) error {
	channels := make([]int64, 0, len(input))
	for _, p := range input {
		if p.RequestID == "" || p.ConsumerUserID <= 0 || p.ChannelID <= 0 || p.ConsumerMicro < 0 || p.GrossMicro < 0 {
			return ErrInvalid
		}
		if _, err := exactfactor.Resolve(p.MultiplierPPM, p.MultiplierPPMExact); err != nil {
			return ErrInvalid
		}
		if p.GrossMicro != 0 {
			channels = append(channels, p.ChannelID)
		}
	}
	if len(channels) == 0 {
		return nil
	}
	if s.poster == nil {
		return ErrUnavailable
	}
	groups, err := loadAccrualGroupsTx(ctx, tx, channels)
	if err != nil {
		return err
	}
	return s.accrueKnownGroupsTx(ctx, tx, input, groups, nil)
}

func loadAccrualGroupsTx(ctx context.Context, tx pgx.Tx, channels []int64) (map[int64]accrualGroup, error) {
	rows, err := tx.Query(ctx, `SELECT channel_id,id,owner_user_id FROM v3_channelmarket.groups WHERE channel_id=ANY($1)`, channels)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := make(map[int64]accrualGroup)
	for rows.Next() {
		var channel int64
		var g accrualGroup
		if err = rows.Scan(&channel, &g.id, &g.owner); err != nil {
			return nil, err
		}
		groups[channel] = g
	}
	return groups, rows.Err()
}

func (s *Service) accrueKnownGroupsTx(ctx context.Context, tx pgx.Tx, input []SettlementInput, groups map[int64]accrualGroup, debitAccounts []int64) error {
	unique := make([]SettlementInput, 0, len(input))
	seen := make(map[string]SettlementInput, len(input))
	for _, p := range input {
		factor, err := exactfactor.Resolve(p.MultiplierPPM, p.MultiplierPPMExact)
		if err != nil {
			return ErrInvalid
		}
		p.MultiplierPPM, p.MultiplierPPMExact = 0, factor
		if p.GrossMicro == 0 || groups[p.ChannelID].owner == 0 {
			continue
		}
		if p.BillingSource == "" {
			p.BillingSource = "wallet"
		}
		if prior, ok := seen[p.RequestID]; ok {
			if prior != p {
				return ErrConflict
			}
			continue
		}
		seen[p.RequestID] = p
		unique = append(unique, p)
	}
	if len(unique) == 0 {
		return nil
	}
	if s.poster == nil {
		return ErrUnavailable
	}
	created, err := insertAccrualSettlementsTx(ctx, tx, unique, groups, s.cfg.Now().Add(s.cfg.Hold))
	if err != nil || len(created) == 0 {
		return err
	}
	owners := make([]int64, 0, len(created))
	needsPlatform := false
	for _, p := range unique {
		if created[p.RequestID] {
			owners = append(owners, groups[p.ChannelID].owner)
			commission, _ := splitAccrueCommission(p.GrossMicro)
			needsPlatform = needsPlatform || commission > 0
		}
	}
	accounts, platform, err := accrualAccountsTx(ctx, tx, owners, needsPlatform)
	if err != nil {
		return err
	}
	entries := make([]billing.Entry, 0, len(created)*2)
	for _, p := range unique {
		if !created[p.RequestID] {
			continue
		}
		commission, net := splitAccrueCommission(p.GrossMicro)
		if net > 0 {
			entries = append(entries, billing.Entry{AccountID: accounts[groups[p.ChannelID].owner], Amount: credits.Micro(net), Kind: "marketplace_accrue", OperationID: "market-pending:" + p.RequestID, RequestID: p.RequestID, Reason: "channel owner held earnings"})
		}
		if commission > 0 {
			entries = append(entries, billing.Entry{AccountID: platform, Amount: credits.Micro(commission), Kind: "marketplace_accrue", OperationID: "market-commission:" + p.RequestID, RequestID: p.RequestID, Reason: "marketplace five percent commission"})
		}
	}
	if poster, ok := s.poster.(accrualBatchPoster); ok {
		return poster.PostAccrualsTx(ctx, tx, entries, debitAccounts)
	}
	ids := append([]int64(nil), debitAccounts...)
	for _, entry := range entries {
		ids = append(ids, entry.AccountID)
	}
	if err = lockAccounts(ctx, tx, ids...); err != nil {
		return err
	}
	for _, e := range entries {
		if _, err = s.poster.PostTx(ctx, tx, e); err != nil {
			return err
		}
	}
	return nil
}

func accrualAccountsTx(ctx context.Context, tx pgx.Tx, owners []int64, needsPlatform bool) (map[int64]int64, int64, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind)
	 SELECT owner_type,owner_id,kind FROM (
	 SELECT 'user'::text owner_type,owner_id,'marketplace_pending'::text kind FROM unnest($1::bigint[]) owner_id
	 UNION SELECT 'platform',1,'platform_revenue' WHERE $2) desired ORDER BY owner_type,owner_id,kind
	 ON CONFLICT(owner_type,owner_id,kind) DO NOTHING`, owners, needsPlatform); err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, `SELECT id,owner_type,owner_id FROM v3_billing.accounts
	 WHERE (owner_type='user' AND owner_id=ANY($1) AND kind='marketplace_pending')
	 OR ($2 AND owner_type='platform' AND owner_id=1 AND kind='platform_revenue')`, owners, needsPlatform)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	accounts := make(map[int64]int64, len(owners))
	var platform int64
	for rows.Next() {
		var id, owner int64
		var typ string
		if err = rows.Scan(&id, &typ, &owner); err != nil {
			return nil, 0, err
		}
		if typ == "platform" {
			platform = id
		} else {
			accounts[owner] = id
		}
	}
	return accounts, platform, rows.Err()
}
