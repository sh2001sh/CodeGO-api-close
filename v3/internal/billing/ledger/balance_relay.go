package ledger

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// BalanceRelay applies committed business postings to hot balances. Redis is
// updated before deleting the durable outbox row; a crash in between replays
// safely through the delivery marker. Unloaded balances already include the
// posting when next loaded from PostgreSQL, identified by their base version.
type BalanceRelay struct {
	pool *pgxpool.Pool
	rdb  *redisx.Client
	log  *slog.Logger
}

func NewBalanceRelay(pool *pgxpool.Pool, rdb *redisx.Client, log *slog.Logger) *BalanceRelay {
	if log == nil {
		log = slog.Default()
	}
	return &BalanceRelay{pool: pool, rdb: rdb, log: log}
}

var businessBalanceScript = redis.NewScript(`
-- KEYS: balance hash, delivery marker; optional reservation,done,index,global-open,business-open.
-- ARGV: signed delta, PG version, marker TTL, optional reservation member.
if redis.call('EXISTS', KEYS[2]) == 1 then return 0 end
local held = KEYS[3] and redis.call('HGET',KEYS[3],'amount') or nil
if redis.call('EXISTS',KEYS[1]) == 1 then
  if redis.call('HGET',KEYS[1],'ver') == '9223372036854775807' then return redis.error_reply('billing version overflow') end
  if held and held ~= '0' then
    local reserved=redis.call('HGET',KEYS[1],'reserved') or '0'
    if string.sub(reserved,1,1) == '-' or #reserved < #held or (#reserved == #held and reserved < held) then
      return redis.error_reply('billing posting hold exceeds reserved')
    end
  end
end
if redis.call('EXISTS', KEYS[1]) == 1 then
  local base = redis.call('HGET', KEYS[1], 'base') or '0'
  if #base < #ARGV[2] or (#base == #ARGV[2] and base < ARGV[2]) then
    redis.call('HINCRBY', KEYS[1], 'balance', ARGV[1])
    redis.call('HINCRBY', KEYS[1], 'ver', 1)
  end
end
if KEYS[3] then
  if held and held ~= '0' and redis.call('EXISTS',KEYS[1]) == 1 then redis.call('HINCRBY',KEYS[1],'reserved','-' .. held) end
  redis.call('DEL',KEYS[3])
  redis.call('SET',KEYS[4],'1','PX',ARGV[3])
  redis.call('SREM',KEYS[5],KEYS[3])
  redis.call('ZREM',KEYS[6],ARGV[4])
  redis.call('ZREM',KEYS[7],ARGV[4])
end
redis.call('SET', KEYS[2], '1', 'PX', ARGV[3])
return 1
`)

func (r *BalanceRelay) Step(ctx context.Context) (int, error) {
	count := 0
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id, account_id, amount, version,coalesce(reservation_id,'') FROM v3_billing.balance_outbox ORDER BY id LIMIT 500 FOR UPDATE SKIP LOCKED`)
		if err != nil {
			return err
		}
		type delivery struct {
			id, account, amount, version int64
			reservation                  string
		}
		var pending []delivery
		for rows.Next() {
			var d delivery
			if err := rows.Scan(&d.id, &d.account, &d.amount, &d.version, &d.reservation); err != nil {
				rows.Close()
				return err
			}
			pending = append(pending, d)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, d := range pending {
			deliveryKey := redisx.KeyBalancePrefix + "post:" + strconv.FormatInt(d.id, 10)
			callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			keys := []string{billing.BalanceKey(d.account), deliveryKey}
			member := ""
			if d.reservation != "" {
				reservation, done, index, m := billing.PostingKeys(d.account, d.reservation)
				member = m
				keys = append(keys, reservation, done, index, redisx.KeyReservationOpen, redisx.KeyPostingOpen)
			}
			err := businessBalanceScript.Run(callCtx, r.rdb, keys,
				d.amount, d.version, (30 * 24 * time.Hour).Milliseconds(), member).Err()
			cancel()
			if err != nil {
				return fmt.Errorf("ledger: deliver balance %d: %w", d.id, err)
			}
			if _, err := tx.Exec(ctx, `DELETE FROM v3_billing.balance_outbox WHERE id = $1`, d.id); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	if err != nil {
		return count, err
	}
	_, err = billing.SweepPostingHolds(ctx, r.rdb, NewAccounts(r.pool), time.Now(), 100)
	return count, err
}

func (r *BalanceRelay) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		n, err := r.Step(ctx)
		if err != nil && ctx.Err() == nil {
			r.log.Error("ledger: balance relay failed", "err", err)
		}
		if n < 500 || err != nil {
			sleepCtx(ctx, 250*time.Millisecond)
		}
	}
	return nil
}
