package commerce

import (
	"context"
	"encoding/json"
	"math"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type refundLot struct {
	trade     string
	remaining credits.Micro
}

type refundQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Replay each account movement in ledger order. Grants create distinct FIFO
// lots; spending consumes existing lots and can never be replenished by a later
// payment or gift. A failed refund restores only its own reserved lot.
func walletRefundLots(ctx context.Context, q refundQuerier, account int64) (map[string]credits.Micro, error) {
	lots, cursor, balance, err := refundOriginLots(ctx, q, account)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT amount,balance_after,kind,operation_id,metadata FROM v3_billing.ledger_entries WHERE account_id=$1 AND id>$2 ORDER BY id`, account, cursor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var amount, after credits.Micro
		var kind, operation string
		var meta []byte
		if err = rows.Scan(&amount, &after, &kind, &operation, &meta); err != nil {
			return nil, err
		}
		next, addErr := balance.Add(amount)
		if addErr != nil || next != after {
			return nil, ErrRefundUnavailable
		}
		if lots, err = applyRefundLotMovement(lots, amount, balance, next, kind, operation, meta); err != nil {
			return nil, err
		}
		balance = next
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	var actual credits.Micro
	if err = q.QueryRow(ctx, `SELECT balance FROM v3_billing.accounts WHERE id=$1`, account).Scan(&actual); err != nil {
		return nil, err
	}
	if actual != balance {
		return nil, ErrFundingPending
	}
	out := make(map[string]credits.Micro)
	for _, lot := range lots {
		if lot.trade != "" {
			out[lot.trade], err = out[lot.trade].Add(lot.remaining)
			if err != nil {
				return nil, ErrRefundUnavailable
			}
		}
	}
	return out, nil
}

// applyRefundLotMovement replays a single ledger entry against the running
// FIFO lots: a grant opens or restores a lot, a debit consumes the trade's
// own lot first and then falls through to generic FIFO consumption.
func applyRefundLotMovement(lots []refundLot, amount, balanceBefore, next credits.Micro, kind, operation string, meta []byte) ([]refundLot, error) {
	var metadata struct {
		TradeNo string `json:"refund_trade_no"`
	}
	if err := json.Unmarshal(meta, &metadata); err != nil {
		return nil, err
	}
	trade := metadata.TradeNo
	if amount > 0 {
		if kind == "topup" && strings.HasPrefix(operation, "order:paid:") {
			trade = strings.TrimPrefix(operation, "order:paid:")
		}
		grant := amount
		if balanceBefore < 0 {
			grant = max(next, 0)
		}
		if grant > 0 {
			restored := false
			if kind == "refund" && trade != "" {
				for i := range lots {
					if lots[i].trade != trade {
						continue
					}
					var err error
					lots[i].remaining, err = lots[i].remaining.Add(grant)
					if err != nil {
						return nil, ErrRefundUnavailable
					}
					restored = true
					break
				}
			}
			if !restored {
				lots = append(lots, refundLot{trade: trade, remaining: grant})
			}
		}
	} else if amount < 0 {
		if amount == credits.Micro(math.MinInt64) {
			return nil, ErrRefundUnavailable
		}
		debit := -amount
		// Refunds consume their own grant first. Any chargeback beyond its
		// unused grant becomes normal account debt/FIFO consumption.
		if trade == "" && strings.HasPrefix(operation, "order:refund:") {
			for _, lot := range lots {
				if lot.trade != "" && (operation == "order:refund:"+lot.trade || strings.HasPrefix(operation, "order:refund:"+lot.trade+":")) {
					trade = lot.trade
					break
				}
			}
		}
		if trade != "" {
			debit = consumeRefundLots(lots, debit, trade)
		}
		consumeRefundLots(lots, debit, "")
	}
	return lots, nil
}

func consumeRefundLots(lots []refundLot, debit credits.Micro, trade string) credits.Micro {
	for i := range lots {
		if trade != "" && lots[i].trade != trade {
			continue
		}
		removed := min(debit, lots[i].remaining)
		lots[i].remaining -= removed
		debit -= removed
		if debit == 0 {
			break
		}
	}
	return debit
}

func refundOriginLots(ctx context.Context, q refundQuerier, account int64) ([]refundLot, int64, credits.Micro, error) {
	rows, err := q.Query(ctx, `SELECT o.trade_no,f.remaining_credits,f.ledger_cursor,e.balance_after FROM v3_commerce.user_refund_origins f
	 JOIN v3_commerce.orders o ON o.id=f.order_id JOIN v3_billing.ledger_entries e ON e.id=f.ledger_cursor
	 WHERE f.account_id=$1 AND e.account_id=f.account_id ORDER BY o.created_at,o.id`, account)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()
	var lots []refundLot
	var cursor int64
	var balance, sum credits.Micro
	for rows.Next() {
		var lot refundLot
		var current int64
		var currentBalance credits.Micro
		if err = rows.Scan(&lot.trade, &lot.remaining, &current, &currentBalance); err != nil {
			return nil, 0, 0, err
		}
		if cursor != 0 && (cursor != current || balance != currentBalance) {
			return nil, 0, 0, ErrRefundUnavailable
		}
		cursor, balance = current, currentBalance
		var addErr error
		sum, addErr = sum.Add(lot.remaining)
		if addErr != nil {
			return nil, 0, 0, ErrRefundUnavailable
		}
		lots = append(lots, lot)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, 0, err
	}
	if sum > max(balance, 0) {
		return nil, 0, 0, ErrRefundUnavailable
	}
	// Unattributed imported balance is consumed after the verifiable old
	// grants, so it cannot disguise spent grants as refundable money.
	if balance > sum {
		lots = append(lots, refundLot{remaining: balance - sum})
	}
	return lots, cursor, balance, nil
}
