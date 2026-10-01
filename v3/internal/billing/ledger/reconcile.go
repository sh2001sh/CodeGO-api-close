package ledger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// ReconcileResult summarizes one pass.
type ReconcileResult struct {
	Checked  int // accounts compared exactly (same version on both sides)
	InFlight int // Redis ahead: charges not posted yet; compared next pass
	Missing  int // no Redis hash: nothing to compare
	Drifted  int // same version, different balance
	Behind   int // Redis missed charges the ledger has (only after partial Redis data loss)
	Repaired int // drifted or behind accounts reset to the ledger
	Stuck    int // behind with Redis-only charges since reload: needs an operator
}

// NeedsAttention reports whether the pass found anything an operator must
// see. Drift and Redis falling behind should never happen in normal
// operation, so both alert even when repaired.
func (r ReconcileResult) NeedsAttention() bool { return r.Drifted > 0 || r.Behind > 0 }

// Reconciler compares Redis hot balances with the PostgreSQL ledger.
//
// Redis "ver" and accounts.version count the same charges in the same order:
// finalize applies and emits each charge atomically, and the worker posts
// them in stream order. So:
//
//   - ver == version: both sides reflect exactly the same charges, and the
//     balances must match to the micro-credit. A mismatch is drift, repaired
//     from the ledger.
//   - ver > version: charges are in flight; compare next pass.
//   - ver < version: Redis never saw some charges the ledger has. This only
//     happens after Redis lost data and a hash was reloaded while older
//     charges were still unposted. If Redis applied nothing since the reload
//     (ver == base) the ledger is complete for this account and the hash is
//     reset to it; otherwise resetting could drop Redis-only charges, so the
//     account is reported and retried.
type Reconciler struct {
	pool *pgxpool.Pool
	rdb  *redisx.Client
	log  *slog.Logger
	page int
}

// NewReconciler returns a Reconciler that reads accounts in pages of 1000.
func NewReconciler(pool *pgxpool.Pool, rdb *redisx.Client, log *slog.Logger) *Reconciler {
	if log == nil {
		log = slog.Default()
	}
	return &Reconciler{pool: pool, rdb: rdb, log: log, page: 1000}
}

// repairScript resets the hash to the ledger, only if no charge landed in
// Redis since the comparison (ver unchanged). Holds are untouched.
var repairScript = redis.NewScript(`
local v = redis.call('HGET', KEYS[1], 'ver')
if not v or v ~= ARGV[1] then return 0 end
redis.call('HSET', KEYS[1], 'balance', ARGV[2], 'ver', ARGV[3], 'base', ARGV[3])
return 1
`)

type accountRow struct {
	id, balance, version int64
	pending              bool
}

type hot struct {
	balance, ver, base int64
}

// Run makes one full pass over every account.
func (r *Reconciler) Run(ctx context.Context) (ReconcileResult, error) {
	var res ReconcileResult
	var after int64
	for {
		rows, err := r.pageAfter(ctx, after)
		if err != nil {
			return res, err
		}
		if len(rows) == 0 {
			return res, nil
		}
		if err := r.checkPage(ctx, rows, &res); err != nil {
			return res, err
		}
		after = rows[len(rows)-1].id
	}
}

// RunReserved reconstructs every account's held total from its live Redis
// reservations. It is separate from balance comparison: holds have no PG
// version and must be checked even while usage postings are in flight.
func (r *Reconciler) RunReserved(ctx context.Context) (int, error) {
	repaired := 0
	var after int64
	for {
		rows, err := r.pageAfter(ctx, after)
		if err != nil {
			return repaired, err
		}
		if len(rows) == 0 {
			return repaired, nil
		}
		for _, a := range rows {
			fixed, err := billing.RecomputeReserved(ctx, r.rdb, a.id)
			if err != nil {
				return repaired, err
			}
			if fixed {
				repaired++
				r.log.Warn("ledger: repaired reserved drift", "account", a.id)
			}
		}
		after = rows[len(rows)-1].id
	}
}

func (r *Reconciler) pageAfter(ctx context.Context, after int64) ([]accountRow, error) {
	rows, err := r.pool.Query(ctx, `SELECT a.id,a.balance,a.version,
	    EXISTS(SELECT 1 FROM v3_billing.balance_outbox o WHERE o.account_id=a.id)
	    FROM v3_billing.accounts a WHERE a.id > $1 ORDER BY a.id LIMIT $2`, after, r.page)
	if err != nil {
		return nil, fmt.Errorf("ledger: reconcile page: %w", err)
	}
	defer rows.Close()
	var out []accountRow
	for rows.Next() {
		var a accountRow
		if err := rows.Scan(&a.id, &a.balance, &a.version, &a.pending); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Reconciler) checkPage(ctx context.Context, rows []accountRow, res *ReconcileResult) error {
	pipe := r.rdb.Pipeline()
	cmds := make([]*redis.SliceCmd, len(rows))
	for i, a := range rows {
		cmds[i] = pipe.HMGet(ctx, billing.BalanceKey(a.id), "balance", "ver", "base")
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("ledger: reconcile redis read: %w", err)
	}
	for i, a := range rows {
		h, ok := parseHot(cmds[i].Val())
		switch {
		case !ok:
			res.Missing++
		case a.pending:
			// A PG business posting and a Redis gateway charge can have the
			// same version while representing different operations. Until
			// business delivery finishes, equality cannot establish drift.
			res.InFlight++
		case h.ver > a.version:
			res.InFlight++
		case h.ver == a.version:
			res.Checked++
			if h.balance != a.balance {
				res.Drifted++
				if err := r.repair(ctx, a, h, "drift", res); err != nil {
					return err
				}
			}
		default: // Redis behind the ledger
			res.Behind++
			if h.ver != h.base {
				res.Stuck++
				r.log.Error("ledger: redis missed ledger charges and has charges of its own since reload; not repaired",
					"account", a.id, "redis_ver", h.ver, "redis_base", h.base, "ledger_version", a.version)
				continue
			}
			if err := r.repair(ctx, a, h, "behind", res); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Reconciler) repair(ctx context.Context, a accountRow, h hot, kind string, res *ReconcileResult) error {
	n := 0
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// Freeze PG posting while rechecking the snapshot. A business debit
		// may have committed after pageAfter without changing Redis ver.
		var latest accountRow
		if err := tx.QueryRow(ctx, `SELECT balance,version FROM v3_billing.accounts WHERE id=$1 FOR SHARE`, a.id).
			Scan(&latest.balance, &latest.version); err != nil {
			return err
		}
		if latest.balance != a.balance || latest.version != a.version {
			return nil
		}
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_billing.balance_outbox WHERE account_id=$1)`, a.id).Scan(&latest.pending); err != nil {
			return err
		}
		if latest.pending {
			return nil
		}
		var err error
		n, err = repairScript.Run(ctx, r.rdb, []string{billing.BalanceKey(a.id)}, h.ver, a.balance, a.version).Int()
		return err
	})
	if err != nil {
		return fmt.Errorf("ledger: repair account %d: %w", a.id, err)
	}
	r.log.Error("ledger: redis balance disagreed with ledger", "kind", kind, "account", a.id,
		"redis_balance", h.balance, "redis_ver", h.ver, "ledger_balance", a.balance, "ledger_version", a.version,
		"diff", h.balance-a.balance, "repaired", n == 1)
	if n == 1 {
		res.Repaired++
	}
	return nil
}

// parseHot reads balance, ver and base. Hashes loaded before 'base' existed
// have none; treating base as ver keeps them repairable when behind.
func parseHot(vals []any) (hot, bool) {
	if len(vals) != 3 || vals[0] == nil || vals[1] == nil {
		return hot{}, false
	}
	var h hot
	var err1, err2 error
	h.balance, err1 = strconv.ParseInt(fmt.Sprint(vals[0]), 10, 64)
	h.ver, err2 = strconv.ParseInt(fmt.Sprint(vals[1]), 10, 64)
	h.base = h.ver
	if vals[2] != nil {
		if b, err := strconv.ParseInt(fmt.Sprint(vals[2]), 10, 64); err == nil {
			h.base = b
		}
	}
	return h, err1 == nil && err2 == nil
}
