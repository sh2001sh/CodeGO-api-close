package community

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/identity/oidc"
)

// The stable public community identifier remains the storage key so migrated
// reviews and their original creation dates survive moving the editor to market.
type MarketRating struct {
	GroupID           string        `json:"group_id"`
	ChannelID         string        `json:"channel_id"`
	Channel           RatingSummary `json:"channel"`
	Seller            RatingSummary `json:"seller"`
	CanRate           bool          `json:"can_rate"`
	EligibilityReason string        `json:"eligibility_reason"`
}

const realChannelUsage = `EXISTS(SELECT 1 FROM v3_audit.request_audits a
 WHERE a.user_id=$2 AND a.counted_in_success_rate AND a.request_type IN ('sync','stream','async','batch')
 AND (a.final_channel_id=c.id OR EXISTS(SELECT 1 FROM v3_audit.request_attempt_audits attempt
 WHERE attempt.request_id=a.request_id AND attempt.channel_id=c.id AND attempt.completed_at>=attempt.started_at)))
 OR EXISTS(SELECT 1 FROM v3_billing.usage_logs l WHERE l.user_id=$2 AND l.channel_id=c.id
 AND l.terminal IN ('completed','completed_no_usage','upstream_error_before_output','upstream_error_after_output','empty_stream','timeout'))`

func requireChannelUsage(ctx context.Context, db rowQueryer, channel string, viewer int64) error {
	var used bool
	err := db.QueryRow(ctx, `SELECT `+realChannelUsage+` FROM v3_catalog.channels c
 WHERE c.settings->'community'->>'id'=$1 AND c.scope='marketplace'`, channel, viewer).Scan(&used)
	if err != nil {
		return err
	}
	if !used {
		return ErrUsageRequired
	}
	return nil
}

// loadMarketGroup checks current marketplace visibility and blocks before
// translating an internal group ID into the stable rating identifier.
func (s *Service) loadMarketGroup(ctx context.Context, group string, viewer int64) (string, string, int64, error) {
	group = strings.TrimSpace(group)
	if viewer < 0 || group == "" || len(group) > 64 {
		return "", "", 0, ErrInvalidQuery
	}
	if s.pool == nil {
		return "", "", 0, ErrUnavailable
	}
	var id, canonical string
	var owner int64
	err := s.pool.QueryRow(ctx, `SELECT c.settings->'community'->>'id',g.id,c.owner_user_id FROM v3_channelmarket.groups g JOIN v3_catalog.channels c ON c.id=g.channel_id JOIN v3_identity.users owner ON owner.id=c.owner_user_id
 WHERE (g.id=$1 OR g.public_channel_id=$1) AND g.deleted_at IS NULL AND owner.status='active' AND owner.deleted_at IS NULL AND c.status='enabled' AND c.scope='marketplace'
 AND g.visibility='public' AND g.lifecycle_status IN ('active','degraded') AND g.verification_status='passed'
 AND NOT EXISTS(SELECT 1 FROM v3_channelmarket.channel_user_blocks b WHERE b.channel_id=c.id AND b.user_id=$2)`, group, viewer).Scan(&id, &canonical, &owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", 0, ErrChannelNotFound
	}
	return id, canonical, owner, err
}

func (s *Service) GetMarketRating(ctx context.Context, group string, viewer int64) (MarketRating, error) {
	id, canonical, owner, err := s.loadMarketGroup(ctx, group, viewer)
	if err != nil {
		return MarketRating{}, err
	}
	r := MarketRating{GroupID: canonical, ChannelID: id}
	err = s.pool.QueryRow(ctx, `SELECT COALESCE(avg(stars)*2,0)::float8,count(*),
 COALESCE(max(stars) FILTER(WHERE user_id=$2),0) FROM v3_community.channel_ratings WHERE channel_id=$1`, id, viewer).Scan(
		&r.Channel.AverageScore, &r.Channel.RatingCount, &r.Channel.ViewerStars)
	if err != nil {
		return r, err
	}
	r.Seller, err = ownerSummary(ctx, s.pool, owner)
	if err != nil {
		return r, err
	}
	if viewer == 0 {
		r.EligibilityReason = "login_required"
		return r, nil
	}
	if owner == viewer {
		r.EligibilityReason = "self_rating"
		return r, nil
	}
	err = requireChannelUsage(ctx, s.pool, id, viewer)
	if errors.Is(err, ErrUsageRequired) {
		r.EligibilityReason = "usage_required"
		return r, nil
	}
	r.CanRate = err == nil
	return r, err
}

func (s *Service) RateMarketGroup(ctx context.Context, group string, viewer int64, subject string, stars int) (MarketRating, error) {
	if viewer <= 0 || stars < 1 || stars > 5 {
		return MarketRating{}, ErrInvalidRating
	}
	id, canonical, owner, err := s.loadMarketGroup(ctx, group, viewer)
	if err != nil {
		return MarketRating{}, err
	}
	// Assign the owner's stable bridge identity before acquiring rating locks.
	// This covers owners who have never visited the community.
	if _, err = oidc.EnsureSubject(ctx, s.pool, owner); err != nil {
		return MarketRating{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MarketRating{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Market mutations lock the group before changing visibility or blocks.
	var locked string
	err = tx.QueryRow(ctx, `SELECT id FROM v3_channelmarket.groups WHERE id=$1 FOR UPDATE`, canonical).Scan(&locked)
	if err != nil {
		return MarketRating{}, err
	}
	viewerID, err := loadActiveMemberIDTx(ctx, tx, subject)
	if err != nil {
		return MarketRating{}, err
	}
	if viewerID != viewer {
		return MarketRating{}, ErrUnauthorized
	}
	expectedOwner := owner
	owner, err = lockRatableChannelOwnerTx(ctx, tx, id)
	if err != nil {
		return MarketRating{}, err
	}
	if owner != expectedOwner {
		return MarketRating{}, ErrChannelNotFound
	}
	var available bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_channelmarket.groups g WHERE g.id=$1 AND g.deleted_at IS NULL AND g.visibility='public' AND g.lifecycle_status IN ('active','degraded') AND g.verification_status='passed' AND NOT EXISTS(SELECT 1 FROM v3_channelmarket.channel_user_blocks b WHERE b.channel_id=g.channel_id AND b.user_id=$2))`, canonical, viewer).Scan(&available)
	if err != nil {
		return MarketRating{}, err
	}
	if !available {
		return MarketRating{}, ErrChannelNotFound
	}
	if owner == viewer {
		return MarketRating{}, ErrSelfRating
	}
	if err = requireChannelUsage(ctx, tx, id, viewer); err != nil {
		return MarketRating{}, err
	}
	if _, err = s.upsertChannelRating(ctx, tx, id, viewer, owner, stars); err != nil {
		return MarketRating{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return MarketRating{}, err
	}
	return s.GetMarketRating(ctx, group, viewer)
}
