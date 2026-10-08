package channelmarket

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type PoolMember struct {
	GroupID  string `json:"group_id"`
	Priority int    `json:"priority"`
}
type RoutePool struct {
	ID                     string          `json:"id"`
	OwnerUserID            int64           `json:"owner_user_id"`
	Name                   string          `json:"name"`
	Strategy               string          `json:"strategy"`
	MaxAttempts            int             `json:"max_attempts"`
	FailureCooldownSeconds int             `json:"failure_cooldown_seconds"`
	MaxMultiplier          json.Number     `json:"max_multiplier"`
	Members                []PoolMember    `json:"members"`
	Config                 json.RawMessage `json:"config,omitempty"`
	GroupIDs               []string        `json:"group_ids,omitempty"`
	AutoBuild              json.RawMessage `json:"auto_build,omitempty"`
	TokenGroup             string          `json:"token_group,omitempty"`
}

func (s *Service) SavePool(ctx context.Context, user int64, p RoutePool) (RoutePool, error) {
	p, maximum, err := normalizePoolInput(user, p)
	if err != nil {
		return p, err
	}
	create := p.ID == ""
	if create {
		p.ID, err = newID()
		if err != nil {
			return p, err
		}
	}
	p.OwnerUserID = user
	p.TokenGroup = "pool_" + p.ID
	err = s.transaction(ctx, func(tx pgx.Tx) error {
		var previous json.RawMessage
		if !create {
			if e := tx.QueryRow(ctx, `SELECT config FROM v3_channelmarket.route_pools WHERE id=$1 AND owner_user_id=$2 FOR UPDATE`, p.ID, user).Scan(&previous); errors.Is(e, pgx.ErrNoRows) {
				return ErrNotFound
			} else if e != nil {
				return e
			}
		}
		if e := preserveAutoBuildMetadata(&p, previous, s.cfg.Now()); e != nil {
			return e
		}
		internal, catalogPool, e := upsertPoolRowTx(ctx, tx, user, p, maximum, create)
		if e != nil {
			return e
		}
		return syncPoolMembersTx(ctx, tx, user, p, maximum, internal, catalogPool, s.cfg.Now())
	})
	return p, err
}

// normalizePoolInput merges the legacy Config/GroupIDs fields into p's
// top-level fields and validates the result, returning the resolved
// max-multiplier factor alongside the normalized pool.
func normalizePoolInput(user int64, p RoutePool) (RoutePool, int64, error) {
	p = mergeLegacyPoolConfig(p)
	p.Name = strings.TrimSpace(p.Name)
	if user <= 0 || p.Name == "" || utf8.RuneCountInString(p.Name) > 64 || len(p.Members) > 100 {
		return p, 0, ErrInvalid
	}
	if p.Strategy == "" {
		p.Strategy = "priority"
	}
	if p.Strategy != "priority" && p.Strategy != "score" && p.Strategy != "cost" && p.Strategy != "weighted" && p.Strategy != "round_robin" && p.Strategy != "fill_first" {
		return p, 0, ErrInvalid
	}
	if p.MaxAttempts == 0 {
		p.MaxAttempts = 3
	}
	if p.MaxAttempts < 1 || p.MaxAttempts > 32 || p.FailureCooldownSeconds < 0 || p.FailureCooldownSeconds > 3600 {
		return p, 0, ErrInvalid
	}
	seen := make(map[string]bool, len(p.Members))
	for _, m := range p.Members {
		if m.GroupID == "" || len(m.GroupID) > 255 || m.Priority < 0 || m.Priority > 1000 || seen[m.GroupID] {
			return p, 0, ErrInvalid
		}
		seen[m.GroupID] = true
	}
	var maximum int64
	if p.MaxMultiplier == "" {
		p.MaxMultiplier = "0"
	}
	rational, ok := new(big.Rat).SetString(string(p.MaxMultiplier))
	if !ok || rational.Sign() < 0 || !json.Valid([]byte(p.MaxMultiplier)) {
		return p, 0, ErrInvalid
	}
	if rational.Sign() > 0 {
		var e error
		maximum, e = multiplier(p.MaxMultiplier)
		if e != nil {
			return p, 0, e
		}
	}
	p, err := normalizePoolAutoBuildConfig(p)
	if err != nil {
		return p, 0, err
	}
	return p, maximum, nil
}

// mergeLegacyPoolConfig copies the legacy GroupIDs/Config input fields onto
// p's canonical top-level fields, leaving p unchanged where the legacy input
// is absent.
func mergeLegacyPoolConfig(p RoutePool) RoutePool {
	if p.Members == nil && p.GroupIDs != nil {
		for priority, id := range p.GroupIDs {
			p.Members = append(p.Members, PoolMember{GroupID: id, Priority: priority})
		}
	}
	// A round-tripped pool already has canonical routing fields. Legacy JSON
	// must not override a user's edits with stale copies of those fields.
	if len(p.Config) == 0 || p.Strategy != "" || p.MaxAttempts != 0 {
		return p
	}
	var config struct {
		Strategy    string      `json:"strategy"`
		MaxAttempts int         `json:"max_attempts"`
		Cooldown    int         `json:"failure_cooldown_seconds"`
		Maximum     json.Number `json:"max_multiplier"`
	}
	if json.Unmarshal(p.Config, &config) != nil {
		return p
	}
	if config.Strategy != "" {
		p.Strategy = config.Strategy
	}
	if config.MaxAttempts != 0 {
		p.MaxAttempts = config.MaxAttempts
	}
	p.FailureCooldownSeconds = config.Cooldown
	if config.Maximum != "" {
		p.MaxMultiplier = config.Maximum
	}
	return p
}

// normalizePoolAutoBuildConfig validates p.Config as a JSON object (defaulting
// to {} when empty), validates any auto_build payload, and merges it into
// the config object.
func normalizePoolAutoBuildConfig(p RoutePool) (RoutePool, error) {
	if len(p.Config) == 0 {
		p.Config = json.RawMessage(`{}`)
	}
	var object map[string]json.RawMessage
	if e := json.Unmarshal(p.Config, &object); e != nil || object == nil {
		return p, ErrInvalid
	}
	raw := object["auto_build"]
	// Validate the nested representation even when the canonical field is present.
	if len(raw) > 0 {
		if _, e := parseAutoBuild(raw, false); e != nil {
			return p, e
		}
	}
	if len(p.AutoBuild) > 0 {
		raw = p.AutoBuild
	}
	if len(raw) > 0 {
		build, e := parseAutoBuild(raw, false)
		if e != nil {
			return p, e
		}
		object["auto_build"], e = json.Marshal(build)
		if e != nil {
			return p, e
		}
		p.AutoBuild = object["auto_build"]
	}
	delete(object, "last_built_at")
	// Legacy routing input is consumed once; callers edit the top-level fields.
	for _, key := range []string{"strategy", "max_attempts", "failure_cooldown_seconds", "max_multiplier"} {
		delete(object, key)
	}
	var e error
	p.Config, e = json.Marshal(object)
	if e != nil {
		return p, e
	}
	return p, nil
}

// upsertPoolRowTx creates or updates the route_pools row for p and ensures
// a matching catalog.route_pools row exists, returning the pool's internal
// group name and catalog pool id for member sync.
func upsertPoolRowTx(ctx context.Context, tx pgx.Tx, user int64, p RoutePool, maximum int64, create bool) (string, int64, error) {
	internal := "pool_" + p.ID
	if create {
		if _, e := tx.Exec(ctx, `INSERT INTO v3_catalog.groups(name) VALUES($1)`, internal); e != nil {
			return "", 0, e
		}
		if _, e := tx.Exec(ctx, `INSERT INTO v3_channelmarket.route_pools(id,owner_user_id,name,internal_group_name,strategy,max_attempts,failure_cooldown_seconds,max_multiplier_ppm,config) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, p.ID, user, p.Name, internal, p.Strategy, p.MaxAttempts, p.FailureCooldownSeconds, maximum, p.Config); e != nil {
			return "", 0, e
		}
	} else {
		if e := tx.QueryRow(ctx, `SELECT internal_group_name FROM v3_channelmarket.route_pools WHERE id=$1 AND owner_user_id=$2 FOR UPDATE`, p.ID, user).Scan(&internal); errors.Is(e, pgx.ErrNoRows) {
			return "", 0, ErrNotFound
		} else if e != nil {
			return "", 0, e
		}
		if _, e := tx.Exec(ctx, `UPDATE v3_channelmarket.route_pools SET name=$3,strategy=$4,max_attempts=$5,failure_cooldown_seconds=$6,max_multiplier_ppm=$7,config=$8 WHERE id=$1 AND owner_user_id=$2`, p.ID, user, p.Name, p.Strategy, p.MaxAttempts, p.FailureCooldownSeconds, maximum, p.Config); e != nil {
			return "", 0, e
		}
	}
	strategy := p.Strategy
	if strategy == "priority" || strategy == "score" || strategy == "cost" {
		strategy = "fill_first"
	}
	var catalogPool int64
	if e := tx.QueryRow(ctx, `INSERT INTO v3_catalog.route_pools(group_name,model,strategy) VALUES($1,'*',$2) ON CONFLICT(group_name,model) WHERE name='' DO UPDATE SET strategy=EXCLUDED.strategy RETURNING id`, internal, strategy).Scan(&catalogPool); e != nil {
		return "", 0, e
	}
	return internal, catalogPool, nil
}

// syncPoolMembersTx replaces the pool's member rows (both the channelmarket
// and catalog-facing tables) to match p.Members exactly.
func syncPoolMembersTx(ctx context.Context, tx pgx.Tx, user int64, p RoutePool, maximum int64, internal string, catalogPool int64, now time.Time) error {
	if _, e := tx.Exec(ctx, `DELETE FROM v3_channelmarket.route_pool_members WHERE pool_id=$1`, p.ID); e != nil {
		return e
	}
	if _, e := tx.Exec(ctx, `DELETE FROM v3_catalog.route_pool_members WHERE pool_id=$1`, catalogPool); e != nil {
		return e
	}
	seen := map[string]bool{}
	for _, m := range p.Members {
		if seen[m.GroupID] {
			return ErrInvalid
		}
		seen[m.GroupID] = true
		if strings.HasPrefix(m.GroupID, "official:") {
			if e := addOfficialPoolMemberTx(ctx, tx, user, p.ID, catalogPool, maximum, m); e != nil {
				return e
			}
			continue
		}
		if e := addOwnedPoolMemberTx(ctx, tx, user, p.ID, catalogPool, maximum, m, now); e != nil {
			return e
		}
	}
	return nil
}

// addOfficialPoolMemberTx wires an "official:<group>" member into both the
// channelmarket pool and the catalog pool, after confirming the owner is
// allowed to use that official group.
func addOfficialPoolMemberTx(ctx context.Context, tx pgx.Tx, user int64, poolID string, catalogPool, maximum int64, m PoolMember) error {
	group := strings.TrimPrefix(m.GroupID, "official:")
	var allowed bool
	if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_catalog.groups g WHERE g.name=$2 AND ($3=0 OR round(g.multiplier*1000000)::bigint<=$3)
	AND EXISTS(SELECT 1 FROM v3_identity.allowed_groups($1) a WHERE a=$2)
	AND EXISTS(SELECT 1 FROM v3_catalog.channels c JOIN v3_catalog.channel_groups cg ON cg.channel_id=c.id WHERE c.scope='official' AND c.status='enabled' AND cg.group_name=$2 AND EXISTS(SELECT 1 FROM v3_catalog.channel_models cm WHERE cm.channel_id=c.id) AND EXISTS(SELECT 1 FROM v3_catalog.channel_credentials k WHERE k.channel_id=c.id AND k.status='enabled')))`, user, group, maximum).Scan(&allowed); e != nil {
		return e
	}
	if !allowed {
		return ErrNotFound
	}
	if _, e := tx.Exec(ctx, `INSERT INTO v3_channelmarket.route_pool_members(pool_id,group_id,catalog_group_name,priority) VALUES($1,$2,$3,$4)`, poolID, m.GroupID, group, m.Priority); e != nil {
		return e
	}
	_, e := tx.Exec(ctx, `INSERT INTO v3_catalog.route_pool_members(pool_id,channel_id,priority) SELECT $1,c.id,$3 FROM v3_catalog.channels c JOIN v3_catalog.channel_groups cg ON cg.channel_id=c.id WHERE c.scope='official' AND c.status='enabled' AND cg.group_name=$2 AND EXISTS(SELECT 1 FROM v3_catalog.channel_models cm WHERE cm.channel_id=c.id) AND EXISTS(SELECT 1 FROM v3_catalog.channel_credentials k WHERE k.channel_id=c.id AND k.status='enabled') ON CONFLICT(pool_id,channel_id) DO UPDATE SET priority=greatest(v3_catalog.route_pool_members.priority,EXCLUDED.priority)`, catalogPool, group, -m.Priority)
	return e
}

// addOwnedPoolMemberTx wires a user-owned channel group member into both the
// channelmarket pool and the catalog pool, enforcing the max-multiplier cap.
func addOwnedPoolMemberTx(ctx context.Context, tx pgx.Tx, user int64, poolID string, catalogPool, maximum int64, m PoolMember, now time.Time) error {
	if e := accessible(ctx, tx, user, m.GroupID); e != nil {
		return e
	}
	var channel, factor int64
	if e := tx.QueryRow(ctx, `SELECT g.channel_id,coalesce(u.multiplier_ppm,least(g.multiplier_ppm,coalesce((SELECT min(multiplier_ppm) FROM v3_channelmarket.time_range_multipliers w WHERE w.channel_id=g.channel_id AND w.starts_at<=$3 AND w.ends_at>$3),g.multiplier_ppm))) FROM v3_channelmarket.groups g JOIN v3_catalog.channels c ON c.id=g.channel_id LEFT JOIN v3_channelmarket.user_multipliers u ON u.channel_id=g.channel_id AND u.user_id=$2 WHERE g.id=$1 AND c.status='enabled' AND EXISTS(SELECT 1 FROM v3_catalog.channel_models cm WHERE cm.channel_id=g.channel_id) AND EXISTS(SELECT 1 FROM v3_catalog.channel_credentials k WHERE k.channel_id=c.id AND k.status='enabled')`, m.GroupID, user, now).Scan(&channel, &factor); errors.Is(e, pgx.ErrNoRows) {
		return ErrNotFound
	} else if e != nil {
		return e
	}
	if maximum > 0 && factor > maximum {
		return ErrInvalid
	}
	if _, e := tx.Exec(ctx, `INSERT INTO v3_channelmarket.route_pool_members(pool_id,group_id,priority) VALUES($1,$2,$3)`, poolID, m.GroupID, m.Priority); e != nil {
		return e
	}
	_, e := tx.Exec(ctx, `INSERT INTO v3_catalog.route_pool_members(pool_id,channel_id,priority) VALUES($1,$2,$3)`, catalogPool, channel, -m.Priority)
	return e
}

func (s *Service) Pools(ctx context.Context, user int64) ([]RoutePool, error) {
	if s.pool == nil {
		return nil, ErrUnavailable
	}
	rows, err := s.pool.Query(ctx, `SELECT id,owner_user_id,name,strategy,max_attempts,failure_cooldown_seconds,max_multiplier_ppm,config FROM v3_channelmarket.route_pools WHERE owner_user_id=$1 ORDER BY name`, user)
	if err != nil {
		return nil, err
	}
	var pools []RoutePool
	for rows.Next() {
		var p RoutePool
		var maximum int64
		if err = rows.Scan(&p.ID, &p.OwnerUserID, &p.Name, &p.Strategy, &p.MaxAttempts, &p.FailureCooldownSeconds, &maximum, &p.Config); err != nil {
			rows.Close()
			return nil, err
		}
		p.MaxMultiplier = json.Number(formatFactor(maximum))
		p.TokenGroup = "pool_" + p.ID
		var config map[string]json.RawMessage
		if err = json.Unmarshal(p.Config, &config); err != nil {
			rows.Close()
			return nil, err
		}
		p.AutoBuild = config["auto_build"]
		pools = append(pools, p)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for i := range pools {
		rows, err = s.pool.Query(ctx, `SELECT group_id,priority FROM v3_channelmarket.route_pool_members WHERE pool_id=$1 ORDER BY priority ASC,group_id`, pools[i].ID)
		if err != nil {
			return nil, err
		}
		pools[i].Members = []PoolMember{}
		for rows.Next() {
			var m PoolMember
			if err = rows.Scan(&m.GroupID, &m.Priority); err != nil {
				rows.Close()
				return nil, err
			}
			pools[i].Members = append(pools[i].Members, m)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return nil, err
		}
	}
	if pools == nil {
		pools = []RoutePool{}
	}
	return pools, nil
}

func (s *Service) DeletePool(ctx context.Context, user int64, id string) error {
	return s.transaction(ctx, func(tx pgx.Tx) error {
		var internal string
		if e := tx.QueryRow(ctx, `SELECT internal_group_name FROM v3_channelmarket.route_pools WHERE id=$1 AND owner_user_id=$2 FOR UPDATE`, id, user).Scan(&internal); errors.Is(e, pgx.ErrNoRows) {
			return ErrNotFound
		} else if e != nil {
			return e
		}
		var bound bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_identity.api_keys WHERE group_name=$1 AND deleted_at IS NULL)`, internal).Scan(&bound); e != nil {
			return e
		}
		if bound {
			return ErrConflict
		}
		if _, e := tx.Exec(ctx, `DELETE FROM v3_catalog.route_pools WHERE group_name=$1`, internal); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, `DELETE FROM v3_channelmarket.route_pools WHERE id=$1 AND owner_user_id=$2`, id, user); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `DELETE FROM v3_catalog.groups WHERE name=$1`, internal)
		return e
	})
}
func (s *Service) BindPoolToken(ctx context.Context, user int64, pool string, key int64) error {
	return s.transaction(ctx, func(tx pgx.Tx) error {
		tag, e := tx.Exec(ctx, `UPDATE v3_identity.api_keys SET group_name=p.internal_group_name FROM v3_channelmarket.route_pools p WHERE p.id=$1 AND p.owner_user_id=$2 AND v3_identity.api_keys.id=$3 AND v3_identity.api_keys.user_id=$2 AND v3_identity.api_keys.status='active' AND v3_identity.api_keys.deleted_at IS NULL`, pool, user, key)
		if e == nil && tag.RowsAffected() != 1 {
			return ErrNotFound
		}
		return e
	})
}
