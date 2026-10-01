// Package catalog compiles v3_catalog rows into the immutable Snapshot
// defined in snapshot.go, publishes new versions over Redis and keeps a
// gateway-side Store in sync (plan §3).
package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Querier is the subset of pgx a Compile call needs. *pgxpool.Pool and
// pgx.Tx both satisfy it.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Begin(ctx context.Context) (pgx.Tx, error)
}

// compiledParts holds every piece read from the database during Compile,
// before routes are built and the final Snapshot is assembled.
type compiledParts struct {
	groups               map[string]Group
	channels             map[int64]*Channel
	channelGroups        channelGroupSet
	channelModels        channelModelSet
	pools                map[string]map[string]pool
	prices               map[string]Price
	settings             map[string]json.RawMessage
	subscriptionPolicies map[string]SubscriptionPolicy
	profiles             map[int64]AccountProfile
	market               MarketSnapshot
	metadata             MetadataSnapshot
	officialPools        map[string]OfficialPool
}

// loadCompiledParts reads every table Compile needs within tx, applying
// channel/group memberships onto the loaded channels. It performs no
// transaction control of its own.
func loadCompiledParts(ctx context.Context, tx pgx.Tx, dec Decrypter) (compiledParts, error) {
	var p compiledParts
	var err error
	if p.groups, err = loadGroups(ctx, tx); err != nil {
		return compiledParts{}, err
	}
	if p.channels, err = loadChannels(ctx, tx, dec); err != nil {
		return compiledParts{}, err
	}
	if p.channelGroups, err = loadChannelGroups(ctx, tx); err != nil {
		return compiledParts{}, err
	}
	for id, memberships := range p.channelGroups {
		if channel := p.channels[id]; channel != nil {
			for group := range memberships {
				channel.Groups = append(channel.Groups, group)
			}
		}
	}
	if p.channelModels, err = loadChannelModels(ctx, tx); err != nil {
		return compiledParts{}, err
	}
	if p.pools, err = loadRoutePools(ctx, tx); err != nil {
		return compiledParts{}, err
	}
	if p.prices, err = loadPrices(ctx, tx); err != nil {
		return compiledParts{}, err
	}
	if p.settings, err = loadSettings(ctx, tx); err != nil {
		return compiledParts{}, err
	}
	if p.subscriptionPolicies, err = compileSubscriptionPolicies(p.settings["SubscriptionGroupPolicy"]); err != nil {
		return compiledParts{}, err
	}
	if p.profiles, err = loadAccountProfiles(ctx, tx); err != nil {
		return compiledParts{}, err
	}
	if p.market, err = ReadMarketSnapshot(ctx, tx); err != nil {
		return compiledParts{}, fmt.Errorf("catalog: market policies: %w", err)
	}
	if p.metadata, err = ReadMetadata(ctx, tx); err != nil {
		return compiledParts{}, err
	}
	if p.officialPools, err = loadOfficialPools(ctx, tx, p.channels, p.channelGroups, p.channelModels); err != nil {
		return compiledParts{}, err
	}
	return p, nil
}

// Compile loads every enabled channel, credential, group, route pool and
// price and builds one immutable Snapshot. It runs inside a single
// REPEATABLE READ read-only transaction so all tables are read at the same
// point in time.
func Compile(ctx context.Context, q Querier, dec Decrypter) (*Snapshot, error) {
	tx, err := q.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("catalog: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY"); err != nil {
		return nil, fmt.Errorf("catalog: set isolation: %w", err)
	}

	parts, err := loadCompiledParts(ctx, tx, dec)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("catalog: commit: %w", err)
	}

	routes := buildRoutes(parts.channels, parts.channelGroups, parts.channelModels, parts.pools)
	return &Snapshot{
		Built:                time.Now(),
		Groups:               parts.groups,
		Channels:             parts.channels,
		Routes:               routes,
		Prices:               parts.prices,
		Settings:             parts.settings,
		AccountProfiles:      parts.profiles,
		Market:               parts.market,
		SubscriptionPolicies: parts.subscriptionPolicies,
		Metadata:             parts.metadata,
		OfficialPools:        parts.officialPools,
	}, nil
}

func loadSettings(ctx context.Context, tx pgx.Tx) (map[string]json.RawMessage, error) {
	rows, err := tx.Query(ctx, `SELECT key,value FROM v3_platform.settings WHERE NOT sensitive`)
	if err != nil {
		return nil, fmt.Errorf("catalog: query settings: %w", err)
	}
	defer rows.Close()
	settings := make(map[string]json.RawMessage)
	for rows.Next() {
		var key string
		var value json.RawMessage
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("catalog: scan setting: %w", err)
		}
		settings[key] = value
	}
	return settings, rows.Err()
}

func loadGroups(ctx context.Context, tx pgx.Tx) (map[string]Group, error) {
	rows, err := tx.Query(ctx, `SELECT name, multiplier FROM v3_catalog.groups`)
	if err != nil {
		return nil, fmt.Errorf("catalog: query groups: %w", err)
	}
	defer rows.Close()

	groups := make(map[string]Group)
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.Name, &g.Multiplier); err != nil {
			return nil, fmt.Errorf("catalog: scan group: %w", err)
		}
		groups[g.Name] = g
	}
	return groups, rows.Err()
}

func loadPrices(ctx context.Context, tx pgx.Tx) (map[string]Price, error) {
	rows, err := tx.Query(ctx, `
		SELECT model, mode, input_per_mtok, output_per_mtok, cache_read_per_mtok,
		       cache_write_per_mtok, per_request, rules
		FROM v3_catalog.model_prices`)
	if err != nil {
		return nil, fmt.Errorf("catalog: query model_prices: %w", err)
	}
	defer rows.Close()

	prices := make(map[string]Price)
	for rows.Next() {
		var p Price
		var rules []byte
		if err := rows.Scan(&p.Model, &p.Mode, &p.InputPerMTok, &p.OutputPerMTok,
			&p.CacheReadPerMTok, &p.CacheWritePerMTok, &p.PerRequest, &rules); err != nil {
			return nil, fmt.Errorf("catalog: scan model_price: %w", err)
		}
		if p.Rules, err = unmarshalAnyMap(rules); err != nil {
			return nil, fmt.Errorf("catalog: price %s rules: %w", p.Model, err)
		}
		prices[p.Model] = p
	}
	return prices, rows.Err()
}

func unmarshalStringMap(raw []byte) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	m := make(map[string]string)
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, nil
	}
	return m, nil
}

func unmarshalAnyMap(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	m := make(map[string]any)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&m); err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, nil
	}
	return m, nil
}
