package channelmarket

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

func insertAccrualSettlementsTx(ctx context.Context, tx pgx.Tx, input []SettlementInput, groups map[int64]accrualGroup, availableAt time.Time) (map[string]bool, error) {
	requests, sources, groupIDs := make([]string, len(input)), make([]string, len(input)), make([]string, len(input))
	channels, owners, consumers := make([]int64, len(input)), make([]int64, len(input)), make([]int64, len(input))
	consumerAmounts, grosses, commissions, nets, factors := make([]int64, len(input)), make([]int64, len(input)), make([]int64, len(input)), make([]int64, len(input)), make([]int64, len(input))
	for i, p := range input {
		g := groups[p.ChannelID]
		requests[i], sources[i], groupIDs[i] = p.RequestID, p.BillingSource, g.id
		channels[i], owners[i], consumers[i] = p.ChannelID, g.owner, p.ConsumerUserID
		consumerAmounts[i], grosses[i], factors[i] = int64(p.ConsumerMicro), int64(p.GrossMicro), p.MultiplierPPM
		commissions[i], nets[i] = splitAccrueCommission(p.GrossMicro)
	}
	rows, err := tx.Query(ctx, `INSERT INTO v3_channelmarket.settlements
	 (id,request_id,channel_id,owner_user_id,consumer_user_id,billing_source,consumer_micro,gross_micro,commission_micro,fee_micro,net_micro,multiplier_ppm,available_at,group_id)
	 SELECT 'usage:'||request_id,request_id,channel_id,owner_user_id,consumer_user_id,billing_source,consumer_micro,gross_micro,commission_micro,0,net_micro,multiplier_ppm,$12,group_id
	 FROM unnest($1::text[],$2::bigint[],$3::bigint[],$4::bigint[],$5::text[],$6::bigint[],$7::bigint[],$8::bigint[],$9::bigint[],$10::bigint[],$11::text[])
	 i(request_id,channel_id,owner_user_id,consumer_user_id,billing_source,consumer_micro,gross_micro,commission_micro,net_micro,multiplier_ppm,group_id)
	 ORDER BY request_id ON CONFLICT(request_id) DO NOTHING RETURNING request_id`,
		requests, channels, owners, consumers, sources, consumerAmounts, grosses, commissions, nets, factors, groupIDs, availableAt)
	if err != nil {
		return nil, err
	}
	created := make(map[string]bool, len(input))
	for rows.Next() {
		var request string
		if err = rows.Scan(&request); err != nil {
			rows.Close()
			return nil, err
		}
		created[request] = true
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(created) == len(input) {
		return created, nil
	}
	// A separate statement sees a concurrent ON CONFLICT winner after commit.
	// Match AccrueTx's existing fingerprint; released/reclaimed state is ignored.
	var identical bool
	err = tx.QueryRow(ctx, `SELECT coalesce(bool_and(s.request_id IS NOT NULL
	 AND s.channel_id=i.channel_id AND s.owner_user_id=i.owner_user_id AND s.consumer_user_id=i.consumer_user_id
	 AND s.billing_source=i.billing_source AND s.consumer_micro=i.consumer_micro AND s.gross_micro=i.gross_micro
	 AND s.multiplier_ppm=i.multiplier_ppm),true)
	 FROM unnest($1::text[],$2::bigint[],$3::bigint[],$4::bigint[],$5::text[],$6::bigint[],$7::bigint[],$8::bigint[])
	 i(request_id,channel_id,owner_user_id,consumer_user_id,billing_source,consumer_micro,gross_micro,multiplier_ppm)
	 LEFT JOIN v3_channelmarket.settlements s USING(request_id)`,
		requests, channels, owners, consumers, sources, consumerAmounts, grosses, factors).Scan(&identical)
	if err != nil {
		return nil, err
	}
	if !identical {
		return nil, ErrConflict
	}
	return created, nil
}
