package commerce

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func (s *UserRefunds) Eligible(ctx context.Context, uid int64) ([]RefundableOrder, error) {
	if uid <= 0 {
		return nil, ErrInvalid
	}
	rows, err := s.pool.Query(ctx, `SELECT `+orderColumns+` FROM v3_commerce.orders WHERE user_id=$1 AND provider='epay' AND state IN ('paid','refunded') ORDER BY id DESC LIMIT 100`, uid)
	if err != nil {
		return nil, err
	}
	var orders []Order
	for rows.Next() {
		o, e := scanOrder(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		orders = append(orders, o)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	result := make([]RefundableOrder, 0, len(orders))
	for _, o := range orders {
		var status string
		err = s.pool.QueryRow(ctx, `SELECT status FROM v3_commerce.user_refunds WHERE order_id=$1 ORDER BY created_at DESC LIMIT 1`, o.ID).Scan(&status)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		account, _, err := s.refundAccount(ctx, s.pool, o, false)
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
			item := refundQuote(o, 0, o.Credits, status)
			result = append(result, item)
			continue
		}
		remaining, _, err := s.refundableBalance(ctx, s.pool, o, account)
		if err != nil && !errors.Is(err, ErrRefundUnavailable) && !errors.Is(err, ErrFundingPending) {
			return nil, err
		}
		item := refundQuote(o, remaining, o.Credits, status)
		var event string
		err = s.pool.QueryRow(ctx, `SELECT coalesce(payment_event_id,'') FROM v3_commerce.orders WHERE id=$1`, o.ID).Scan(&event)
		if err != nil {
			return nil, err
		}
		if event == "" || s.provider == nil {
			item.Refundable = false
			item.UnavailableReason = "退款服务未配置或缺少支付平台订单号"
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *UserRefunds) refundAccount(ctx context.Context, q refundQuerier, o Order, lock bool) (int64, int64, error) {
	var account, sub int64
	var sql string
	if o.Kind == "topup" {
		sql = `SELECT id,0::bigint FROM v3_billing.accounts WHERE owner_type='user' AND owner_id=$1 AND kind='wallet'`
		if lock {
			sql += ` FOR UPDATE`
		}
		if err := q.QueryRow(ctx, sql, o.UserID).Scan(&account, &sub); err != nil {
			return 0, 0, err
		}
	} else {
		sql = `SELECT account_id,id FROM v3_commerce.subscriptions WHERE order_id=$1 AND user_id=$2 AND state IN ('active','expired')
		 AND NOT EXISTS(SELECT 1 FROM v3_commerce.package_checkouts pc WHERE pc.target_subscription_id=v3_commerce.subscriptions.id AND pc.state IN ('preparing','checkout'))`
		if lock {
			sql += ` FOR UPDATE`
		}
		if err := q.QueryRow(ctx, sql, o.ID, o.UserID).Scan(&account, &sub); err != nil {
			return 0, 0, err
		}
	}
	return account, sub, nil
}

func (s *UserRefunds) refundableBalance(ctx context.Context, q refundQuerier, o Order, account int64) (credits.Micro, credits.Micro, error) {
	if o.Kind == "topup" {
		lots, err := walletRefundLots(ctx, q, account)
		if err != nil {
			return 0, 0, err
		}
		remaining := min(lots[o.TradeNo], o.Credits)
		return remaining, remaining, nil
	}
	var used, current, balance credits.Micro
	var origin *credits.Micro
	var imported bool
	err := q.QueryRow(ctx, `SELECT s.used_credits,COALESCE(u.spent,0),a.balance,f.used_credits,
	 NOT EXISTS(SELECT 1 FROM v3_commerce.subscription_buckets b JOIN v3_billing.ledger_entries e ON e.account_id=b.account_id
	 WHERE b.subscription_id=s.id AND e.kind='subscription_grant' AND
	 (e.operation_id='subscription:grant:'||$3 OR e.operation_id='subscription:cycle:grant:'||s.id::text||':package:'||$3))
	 FROM v3_commerce.subscriptions s
	 JOIN v3_billing.accounts a ON a.id=s.account_id
	 LEFT JOIN v3_commerce.user_refund_subscription_origins f ON f.subscription_id=s.id AND f.order_id=s.order_id
	 LEFT JOIN LATERAL(SELECT GREATEST(-SUM(amount),0)::bigint spent FROM v3_billing.ledger_entries WHERE account_id=s.account_id AND kind IN ('usage','refund')) u ON true
	 WHERE s.order_id=$1 AND s.account_id=$2 AND s.state IN ('active','expired')`, o.ID, account, o.TradeNo).Scan(&used, &current, &balance, &origin, &imported)
	if err != nil {
		return 0, 0, err
	}
	if imported && origin == nil {
		return 0, 0, ErrRefundUnavailable
	}
	if origin != nil {
		used = max(used, *origin)
	}
	used, err = used.Add(current)
	if err != nil {
		return 0, 0, ErrRefundUnavailable
	}
	remaining := max(o.Credits-used, 0)
	return remaining, max(balance, 0), nil
}
