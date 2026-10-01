package ledger

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// DrainChecker prevents a subscription's unspent balance from expiring while
// reserved streams or Redis usage events are still outstanding. Redis must use
// persistence/noeviction, just as for the gateway's BalanceLoader contract.
type DrainChecker struct{ rdb *redisx.Client }

func NewDrainChecker(rdb *redisx.Client) *DrainChecker { return &DrainChecker{rdb: rdb} }

var freezeAccountScript = redis.NewScript(`
-- KEYS: subscription balance hash. ARGV: PG balance,PG version,pending outbox. closed forbids
-- new funding holds, but finalize can settle existing streams. An account never
-- loaded into Redis is initialized from PG, including its outbox base version.
if redis.call('HEXISTS',KEYS[1],'balance') == 0 then
  redis.call('HSET',KEYS[1],'balance',ARGV[1],'reserved','0','ver',ARGV[2],'base',ARGV[2])
end
redis.call('HSET',KEYS[1],'closed','1')
local reserved=redis.call('HGET',KEYS[1],'reserved') or '0'
local version=redis.call('HGET',KEYS[1],'ver') or '0'
local balance=redis.call('HGET',KEYS[1],'balance') or '0'
return reserved == '0' and version == ARGV[2] and balance == ARGV[1] and ARGV[3] == '0' and 1 or 0
`)

// FreezeAndDrained should be called in the transaction that owns the
// subscription state change, before posting subscription_expire. False means
// retry after the live request settles and its ledger event is consumed.
func (d *DrainChecker) FreezeAndDrained(ctx context.Context, tx pgx.Tx, accountID int64) (bool, error) {
	var balance, version int64
	if err := tx.QueryRow(ctx, `SELECT balance,version FROM v3_billing.accounts WHERE id=$1 FOR UPDATE`, accountID).Scan(&balance, &version); err != nil {
		return false, err
	}
	var pending bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_billing.balance_outbox WHERE account_id=$1)`, accountID).Scan(&pending); err != nil {
		return false, err
	}
	pendingFlag := "0"
	if pending {
		pendingFlag = "1"
	}
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	n, err := freezeAccountScript.Run(callCtx, d.rdb, []string{billing.BalanceKey(accountID)}, balance, version, pendingFlag).Int()
	if err != nil {
		return false, fmt.Errorf("ledger: freeze subscription account %d: %w", accountID, err)
	}
	return n == 1, nil
}
