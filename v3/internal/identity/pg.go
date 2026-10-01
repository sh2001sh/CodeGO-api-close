package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pgLoader reads one key profile. Disabled or deleted keys and users are
// returned as Revoked profiles (cacheable); unknown hashes return nil.
type pgLoader struct{ pool *pgxpool.Pool }

const profileSelect = `
SELECT k.id, k.user_id, coalesce(k.group_name, u.group_name),
       (k.status <> 'active' OR k.deleted_at IS NOT NULL OR u.status <> 'active' OR u.deleted_at IS NOT NULL),
       k.expires_at, k.allowed_models, k.allowed_cidrs::text[], k.budget_limited,
	       k.cross_group_retry, (k.max_marketplace_multiplier*1000000)::bigint, u.max_concurrency, u.requests_per_minute,
	       coalesce(b.id,0), ARRAY(SELECT v3_identity.allowed_groups(u.id)), ARRAY(SELECT v3_identity.auto_groups(u.id))
FROM v3_identity.api_keys k
JOIN v3_identity.users u ON u.id = k.user_id
LEFT JOIN v3_billing.accounts b ON b.owner_type='api_key' AND b.owner_id=k.id AND b.kind='key_budget'
`

const profileQuery = profileSelect + `WHERE k.key_hash = $1`

func (l pgLoader) load(ctx context.Context, hash [32]byte) (*KeyProfile, error) {
	return scanProfile(l.pool.QueryRow(ctx, profileQuery, hash[:]))
}

func scanProfile(row pgx.Row) (*KeyProfile, error) {
	var p KeyProfile
	var expires *time.Time
	var cidrs []string
	err := row.Scan(&p.KeyID, &p.UserID, &p.Group, &p.Revoked,
		&expires, &p.AllowedModels, &cidrs, &p.BudgetLimited, &p.CrossGroupRetry, &p.MaxMarketplaceMultiplierPPM,
		&p.MaxConcurrency, &p.RequestsPerMinute, &p.BudgetAccountID, &p.AllowedGroups, &p.AutoGroups)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("identity: load key: %w", err)
	}
	if expires != nil {
		p.ExpiresAt = *expires
	}
	if cidrs != nil {
		p.AllowedCIDRs = make([]netip.Prefix, 0, len(cidrs))
		for _, s := range cidrs {
			prefix, err := netip.ParsePrefix(s)
			if err != nil {
				return nil, fmt.Errorf("identity: key %d cidr %q: %w", p.KeyID, s, err)
			}
			p.AllowedCIDRs = append(p.AllowedCIDRs, prefix)
		}
	}
	return &p, nil
}

// toucher batches api_keys.last_used_at writes: each key is written at most
// once per window, from a background flusher, never on the request path.
type toucher struct {
	pool   *pgxpool.Pool
	window time.Duration
	now    func() time.Time
	log    *slog.Logger

	mu      sync.Mutex
	pending map[int64]struct{}
	written map[int64]time.Time
}

func newToucher(pool *pgxpool.Pool, window time.Duration, now func() time.Time, log *slog.Logger) *toucher {
	return &toucher{pool: pool, window: window, now: now, log: log,
		pending: make(map[int64]struct{}), written: make(map[int64]time.Time)}
}

func (t *toucher) touch(keyID int64) {
	t.mu.Lock()
	if last, ok := t.written[keyID]; !ok || t.now().Sub(last) >= t.window {
		t.pending[keyID] = struct{}{}
	}
	t.mu.Unlock()
}

// flush writes every pending key in one statement and forgets keys idle
// longer than the window, which bounds memory to recently active keys.
func (t *toucher) flush(ctx context.Context) error {
	now := t.now()
	t.mu.Lock()
	ids := make([]int64, 0, len(t.pending))
	for id := range t.pending {
		ids = append(ids, id)
		t.written[id] = now
	}
	clear(t.pending)
	for id, at := range t.written {
		if now.Sub(at) > 2*t.window {
			delete(t.written, id)
		}
	}
	t.mu.Unlock()
	if len(ids) == 0 {
		return nil
	}
	_, err := t.pool.Exec(ctx, `UPDATE v3_identity.api_keys SET last_used_at = now() WHERE id = ANY($1)`, ids)
	if err != nil {
		t.mu.Lock()
		for _, id := range ids { // retry on the next flush
			t.pending[id] = struct{}{}
			delete(t.written, id)
		}
		t.mu.Unlock()
		return fmt.Errorf("identity: touch %d keys: %w", len(ids), err)
	}
	return nil
}

func (t *toucher) run(ctx context.Context, every time.Duration) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			if err := t.flush(flushCtx); err != nil {
				t.log.Error("identity: final last_used flush failed", "err", err)
			}
			cancel()
			return
		case <-tick.C:
			if err := t.flush(ctx); err != nil {
				t.log.Error("identity: last_used flush failed", "err", err)
			}
		}
	}
}
