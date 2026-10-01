package commerce

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/sh2001sh/new-api/v3/internal/billing"
)

var userRefundFreeze = redis.NewScript(`
local owner=redis.call('HGET',KEYS[1],'user_refund_owner')
local closed=redis.call('HGET',KEYS[1],'closed')
if owner and owner~=ARGV[3] then return -1 end
if closed=='1' and not owner and ARGV[4]~='1' then return -1 end
if redis.call('HEXISTS',KEYS[1],'balance')==0 then
 redis.call('HSET',KEYS[1],'balance',ARGV[1],'reserved','0','ver',ARGV[2],'base',ARGV[2])
end
if not(closed=='1' and not owner) then redis.call('HSET',KEYS[1],'closed','1','user_refund_owner',ARGV[3]) end
local held=redis.call('HGET',KEYS[1],'reserved') or '0'
local ver=redis.call('HGET',KEYS[1],'ver') or '0'
local balance=redis.call('HGET',KEYS[1],'balance')
return held=='0' and ver==ARGV[2] and balance==ARGV[1] and 1 or 0
`)

var userRefundReopen = redis.NewScript(`
if redis.call('HGET',KEYS[1],'user_refund_owner')==ARGV[1] then
 redis.call('HDEL',KEYS[1],'closed','user_refund_owner')
 return 1
end
return 0
`)

func (s *UserRefunds) leaseAccount(ctx context.Context, account int64) (string, error) {
	token, err := tradeNumber()
	if err != nil {
		return "", err
	}
	tag, err := s.pool.Exec(ctx, `INSERT INTO v3_commerce.user_refund_freezes(account_id,token,expires_at) VALUES($1,$2,$3)
	 ON CONFLICT(account_id) DO NOTHING`, account, token, s.now().Add(2*time.Minute))
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() != 1 {
		return "", ErrFundingPending
	}
	return token, nil
}

func (s *UserRefunds) freezeTx(ctx context.Context, tx pgx.Tx, account int64, token string, expired bool) error {
	var balance, version int64
	if err := tx.QueryRow(ctx, `SELECT balance,version FROM v3_billing.accounts WHERE id=$1 FOR UPDATE`, account).Scan(&balance, &version); err != nil {
		return err
	}
	if s.rdb == nil {
		return nil
	}
	// Equal version counts can hide a PG-only adjustment and a different
	// Redis-only usage event. Require every business outbox movement delivered
	// before using version/balance equality as a proof of usage drainage.
	var pending bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_billing.balance_outbox WHERE account_id=$1)`, account).Scan(&pending); err != nil {
		return err
	}
	if pending {
		return ErrFundingPending
	}
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	allowClosed := "0"
	if expired {
		allowClosed = "1"
	}
	n, err := userRefundFreeze.Run(callCtx, s.rdb, []string{billing.BalanceKey(account)}, balance, version, token, allowClosed).Int()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrFundingPending
	}
	return nil
}

func (s *UserRefunds) releaseAccount(ctx context.Context, account int64, token string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var stored string
		err := tx.QueryRow(ctx, `SELECT token FROM v3_commerce.user_refund_freezes WHERE account_id=$1 FOR UPDATE`, account).Scan(&stored)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if stored != token {
			return ErrStateConflict
		}
		if s.rdb != nil {
			callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			if _, err = userRefundReopen.Run(callCtx, s.rdb, []string{billing.BalanceKey(account)}, token).Result(); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `DELETE FROM v3_commerce.user_refund_freezes WHERE account_id=$1 AND token=$2`, account, token)
		return err
	})
}

// Recover reopens abandoned admission leases from durable PG rows. It is safe
// to run from multiple workers; a different domain's closure is never removed.
func (s *UserRefunds) Recover(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `SELECT account_id,token FROM v3_commerce.user_refund_freezes WHERE expires_at<=$1 ORDER BY expires_at LIMIT 100`, s.now())
	if err != nil {
		return err
	}
	type lease struct {
		account int64
		token   string
	}
	var items []lease
	for rows.Next() {
		var l lease
		if err = rows.Scan(&l.account, &l.token); err != nil {
			rows.Close()
			return err
		}
		items = append(items, l)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, l := range items {
		if err = s.releaseAccount(ctx, l.account, l.token); err != nil && !errors.Is(err, ErrStateConflict) {
			return err
		}
	}
	return nil
}

func (s *UserRefunds) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := s.Recover(ctx); err != nil {
			return err
		}
		if err := s.SyncPending(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
