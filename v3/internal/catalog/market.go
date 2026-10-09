package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/exactfactor"
)

// Snapshot is immutable after compilation. Catalog includes it in its atomic
// publication so authorization and prices never require a hot-path PG read.
type MarketSnapshot struct {
	Groups   map[string]MarketGroupPolicy
	Channels map[int64]MarketChannelPolicy
	Pools    map[string]MarketPoolPolicy
}
type MarketGroupPolicy struct {
	ID                     string
	ChannelID, OwnerUserID int64
	Visibility, Status     string
	Allowed                map[int64]bool
	Score                  float64
	HasScore               bool
}
type MarketChannelPolicy struct {
	CreditPolicy               string
	GroupName                  string
	OwnerUserID, MultiplierPPM int64
	MultiplierPPMExact         string
	Blocked                    map[int64]bool
	UserMultipliers            map[int64]int64
	UserMultipliersExact       map[int64]string
	Windows                    []MarketMultiplierWindow
	Prices                     json.RawMessage
	ModelPrices                map[string]Price
}
type MarketMultiplierWindow struct {
	StartsAt, EndsAt   time.Time
	MultiplierPPM      int64
	MultiplierPPMExact string
}
type MarketPoolPolicy struct {
	OwnerUserID            int64
	GroupIDs               []string
	MaxMultiplierPPM       int64
	MaxAttempts            int
	FailureCooldownSeconds int
	Strategy               string
	Members                []MarketPoolMember
}

type MarketPoolMember struct {
	GroupID          string
	CatalogGroupName string
	Priority         int
}

func (g MarketGroupPolicy) Allows(user int64) bool {
	return g.Status == "active" && (g.Visibility == "public" || user == g.OwnerUserID || g.Allowed[user])
}

// Factor is retained for integral PPM compatibility. Runtime pricing and
// authorization must use FactorExact to preserve fractional legacy overrides.
func (c MarketChannelPolicy) Factor(user int64, now time.Time) int64 {
	if value, ok := c.UserMultipliers[user]; ok {
		return value
	}
	factor := c.MultiplierPPM
	for _, w := range c.Windows {
		if !now.Before(w.StartsAt) && now.Before(w.EndsAt) && w.MultiplierPPM < factor {
			factor = w.MultiplierPPM
		}
	}
	return factor
}

// FactorExact returns the effective scaled PPM without quantizing legacy
// fractional overrides. User overrides take precedence over all time windows.
// An empty result means an invalid policy; routing must refuse that policy.
func (c MarketChannelPolicy) FactorExact(user int64, now time.Time) string {
	if value, ok := c.UserMultipliersExact[user]; ok {
		return value
	}
	if value, ok := c.UserMultipliers[user]; ok {
		return exactfactor.FromInt64(value)
	}
	factor, err := exactfactor.Resolve(c.MultiplierPPM, c.MultiplierPPMExact)
	if err != nil {
		return ""
	}
	for _, w := range c.Windows {
		if now.Before(w.StartsAt) || !now.Before(w.EndsAt) {
			continue
		}
		value, err := exactfactor.Resolve(w.MultiplierPPM, w.MultiplierPPMExact)
		if err != nil {
			return ""
		}
		cmp, err := exactfactor.Compare(value, factor)
		if err != nil {
			return ""
		}
		if cmp < 0 {
			factor = value
		}
	}
	return factor
}

// readMarketGroupsAndChannels loads market groups and their derived channel
// policies, returning the group-id -> internal-group-name lookup needed by
// subsequent queries.
func readMarketGroupsAndChannels(ctx context.Context, tx pgx.Tx, s *MarketSnapshot) (map[string]string, error) {
	rows, err := tx.Query(ctx, `SELECT id,channel_id,owner_user_id,internal_group_name,visibility,lifecycle_status,multiplier_ppm::text,model_prices,credit_pool_policy FROM v3_channelmarket.groups WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	groupNames := map[string]string{}
	for rows.Next() {
		var id, name, visibility, status, creditPolicy string
		var channel, owner int64
		var factor string
		var prices json.RawMessage
		if err = rows.Scan(&id, &channel, &owner, &name, &visibility, &status, &factor, &prices, &creditPolicy); err != nil {
			rows.Close()
			return nil, err
		}
		if _, err = exactfactor.ParsePPM(factor); err != nil {
			rows.Close()
			return nil, fmt.Errorf("market group %s multiplier: %w", id, err)
		}
		integerFactor, _ := exactfactor.Int64(factor)
		groupNames[id] = name
		s.Groups[name] = MarketGroupPolicy{ID: id, ChannelID: channel, OwnerUserID: owner, Visibility: visibility, Status: status, Allowed: map[int64]bool{}}
		modelPrices, e := ParseMarketPrices(prices)
		if e != nil {
			rows.Close()
			return nil, e
		}
		s.Channels[channel] = MarketChannelPolicy{GroupName: name, CreditPolicy: creditPolicy, OwnerUserID: owner, MultiplierPPM: integerFactor, MultiplierPPMExact: factor, Prices: prices, ModelPrices: modelPrices, Blocked: map[int64]bool{}, UserMultipliers: map[int64]int64{}, UserMultipliersExact: map[int64]string{}}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return groupNames, nil
}

// readMarketGroupAccess loads per-user group allowlists keyed by group id,
// applying them onto the already-populated s.Groups by internal name.
func readMarketGroupAccess(ctx context.Context, tx pgx.Tx, s *MarketSnapshot, groupNames map[string]string) error {
	rows, err := tx.Query(ctx, `SELECT group_id,user_id FROM v3_channelmarket.group_access`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var user int64
		if err = rows.Scan(&id, &user); err != nil {
			rows.Close()
			return err
		}
		if name, ok := groupNames[id]; ok {
			s.Groups[name].Allowed[user] = true
		}
	}
	rows.Close()
	return rows.Err()
}

// readMarketChannelBlocks loads per-user channel blocks onto s.Channels.
func readMarketChannelBlocks(ctx context.Context, tx pgx.Tx, s *MarketSnapshot) error {
	rows, err := tx.Query(ctx, `SELECT channel_id,user_id FROM v3_channelmarket.channel_user_blocks`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var channel, user int64
		if err = rows.Scan(&channel, &user); err != nil {
			rows.Close()
			return err
		}
		if c, ok := s.Channels[channel]; ok {
			c.Blocked[user] = true
		}
	}
	rows.Close()
	return rows.Err()
}

// readMarketUserMultiplierOverrides loads per-user multiplier overrides onto
// s.Channels.
func readMarketUserMultiplierOverrides(ctx context.Context, tx pgx.Tx, s *MarketSnapshot) error {
	rows, err := tx.Query(ctx, `SELECT channel_id,user_id,multiplier_ppm::text FROM v3_channelmarket.user_multipliers`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var channel, user int64
		var factor string
		if err = rows.Scan(&channel, &user, &factor); err != nil {
			rows.Close()
			return err
		}
		if c, ok := s.Channels[channel]; ok {
			if _, err = exactfactor.ParsePPM(factor); err != nil {
				rows.Close()
				return fmt.Errorf("market channel %d user %d multiplier: %w", channel, user, err)
			}
			if c.UserMultipliersExact == nil {
				c.UserMultipliersExact = map[int64]string{}
			}
			c.UserMultipliersExact[user] = factor
			if integer, fits := exactfactor.Int64(factor); fits {
				if c.UserMultipliers == nil {
					c.UserMultipliers = map[int64]int64{}
				}
				c.UserMultipliers[user] = integer
			}
			s.Channels[channel] = c
		}
	}
	rows.Close()
	return rows.Err()
}

// readMarketTimeWindows loads time-ranged multiplier windows onto s.Channels,
// ordered by start time then id.
func readMarketTimeWindows(ctx context.Context, tx pgx.Tx, s *MarketSnapshot) error {
	rows, err := tx.Query(ctx, `SELECT channel_id,starts_at,ends_at,multiplier_ppm::text FROM v3_channelmarket.time_range_multipliers ORDER BY starts_at,id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var channel int64
		var window MarketMultiplierWindow
		if err = rows.Scan(&channel, &window.StartsAt, &window.EndsAt, &window.MultiplierPPMExact); err != nil {
			rows.Close()
			return err
		}
		if c, ok := s.Channels[channel]; ok {
			if _, err = exactfactor.ParsePPM(window.MultiplierPPMExact); err != nil {
				rows.Close()
				return fmt.Errorf("market channel %d window multiplier: %w", channel, err)
			}
			window.MultiplierPPM, _ = exactfactor.Int64(window.MultiplierPPMExact)
			c.Windows = append(c.Windows, window)
			s.Channels[channel] = c
		}
	}
	rows.Close()
	return rows.Err()
}

func ReadMarketSnapshot(ctx context.Context, tx pgx.Tx) (MarketSnapshot, error) {
	s := MarketSnapshot{Groups: map[string]MarketGroupPolicy{}, Channels: map[int64]MarketChannelPolicy{}, Pools: map[string]MarketPoolPolicy{}}
	groupNames, err := readMarketGroupsAndChannels(ctx, tx, &s)
	if err != nil {
		return s, err
	}
	if err = readMarketGroupAccess(ctx, tx, &s, groupNames); err != nil {
		return s, err
	}
	if err = readMarketChannelBlocks(ctx, tx, &s); err != nil {
		return s, err
	}
	if err = readMarketUserMultiplierOverrides(ctx, tx, &s); err != nil {
		return s, err
	}
	if err = readMarketTimeWindows(ctx, tx, &s); err != nil {
		return s, err
	}
	if err = readMarketRankings(ctx, tx, s.Groups, groupNames); err != nil {
		return s, err
	}
	err = readMarketPools(ctx, tx, s.Pools)
	return s, err
}
