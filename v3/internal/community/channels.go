package community

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

type ChannelQuery struct {
	Page, PageSize               int
	Keyword, Sort, ViewerSubject string
}

func (q ChannelQuery) validate() (ChannelQuery, error) {
	q.Keyword, q.Sort = strings.TrimSpace(q.Keyword), strings.TrimSpace(q.Sort)
	if q.Page == 0 {
		q.Page = 1
	}
	if q.PageSize == 0 {
		q.PageSize = 20
	}
	if q.Sort == "" {
		q.Sort = "rating"
	}
	if q.Page < 1 || q.Page > 10000 || q.PageSize < 1 || q.PageSize > 50 || len([]rune(q.Keyword)) > 64 {
		return q, ErrInvalidQuery
	}
	switch q.Sort {
	case "rating", "updated", "name":
	default:
		return q, ErrInvalidQuery
	}
	if q.ViewerSubject != "" {
		var err error
		q.ViewerSubject, err = normalizeSubject(q.ViewerSubject)
		if err != nil {
			return q, err
		}
	}
	return q, nil
}

func keywordPattern(value string) string {
	value = strings.ToLower(value)
	value = strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(value)
	return "%" + value + "%"
}

const channelFields = `c.settings->'community'->>'id',
 COALESCE(c.settings->'community'->>'slug',''), COALESCE(c.settings->'community'->>'name',c.name),
 c.provider,c.settings->'community'->>'lifecycle_status',c.settings->'community'->>'verification_status',
 COALESCE((SELECT avg(stars)*2 FROM v3_community.channel_ratings WHERE channel_id=c.settings->'community'->>'id'),0)::float8,
 (SELECT count(*) FROM v3_community.channel_ratings WHERE channel_id=c.settings->'community'->>'id'),
 COALESCE((SELECT stars FROM v3_community.channel_ratings WHERE channel_id=c.settings->'community'->>'id' AND user_id=$2),0)`

const channelFilter = ` AND c.owner_user_id=$1 AND ($3::text='' OR
 LOWER(COALESCE(c.settings->'community'->>'name',c.name)) LIKE $4 ESCAPE '!'
 OR LOWER(COALESCE(c.settings->'community'->>'slug','')) LIKE $4 ESCAPE '!')`

func (s *Service) ListChannels(ctx context.Context, subject string, query ChannelQuery) (ChannelList, error) {
	q, err := query.validate()
	if err != nil {
		return ChannelList{}, err
	}
	if _, err := normalizeSubject(subject); err != nil {
		return ChannelList{}, err
	}
	if s.pool == nil {
		return ChannelList{}, ErrUnavailable
	}
	u, err := loadMember(ctx, s.pool, subject)
	if err != nil {
		return ChannelList{}, err
	}
	if u.Status != "active" {
		return ChannelList{}, ErrInactive
	}
	viewerID, err := s.resolveChannelViewer(ctx, q)
	if err != nil {
		return ChannelList{}, err
	}
	result := ChannelList{Items: []Channel{}, Page: q.Page, PageSize: q.PageSize}
	// Reuse all four typed parameters so an empty search has identical scope.
	args := []any{u.ID, viewerID, q.Keyword, keywordPattern(q.Keyword)}
	err = s.pool.QueryRow(ctx, `SELECT count(*) `+eligibleChannels+`
 AND c.owner_user_id=$1 AND $2::bigint>=0 AND ($3::text='' OR
 LOWER(COALESCE(c.settings->'community'->>'name',c.name)) LIKE $4 ESCAPE '!'
 OR LOWER(COALESCE(c.settings->'community'->>'slug','')) LIKE $4 ESCAPE '!')`, args...).Scan(&result.Total)
	if err != nil {
		return ChannelList{}, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+channelFields+` `+eligibleChannels+channelFilter+
		` ORDER BY `+channelListOrder(q.Sort)+` LIMIT $5 OFFSET $6`, append(args, q.PageSize, (q.Page-1)*q.PageSize)...)
	if err != nil {
		return ChannelList{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var c Channel
		if err := rows.Scan(&c.ID, &c.Slug, &c.Name, &c.Provider, &c.LifecycleStatus, &c.VerificationStatus,
			&c.AverageScore, &c.RatingCount, &c.ViewerStars); err != nil {
			return ChannelList{}, err
		}
		result.Items = append(result.Items, c)
	}
	return result, rows.Err()
}

// resolveChannelViewer loads and validates the optional viewer subject whose
// own rating should be included in results, returning 0 if none was given.
func (s *Service) resolveChannelViewer(ctx context.Context, q ChannelQuery) (int64, error) {
	if q.ViewerSubject == "" {
		return 0, nil
	}
	viewer, err := loadMember(ctx, s.pool, q.ViewerSubject)
	if err != nil {
		return 0, err
	}
	if viewer.Status != "active" {
		return 0, ErrInactive
	}
	return viewer.ID, nil
}

// channelListOrder maps a validated sort name to its ORDER BY clause.
func channelListOrder(sort string) string {
	switch sort {
	case "updated":
		return "c.updated_at DESC,c.id"
	case "name":
		return "3,c.id"
	default:
		return "7 DESC,8 DESC,c.updated_at DESC,c.id"
	}
}

func (s *Service) RateChannel(ctx context.Context, id string, request RatingRequest) (RatingResult, error) {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 64 || request.Stars < 1 || request.Stars > 5 {
		return RatingResult{}, ErrInvalidRating
	}
	subject, err := normalizeSubject(request.ViewerSubject)
	if err != nil {
		return RatingResult{}, err
	}
	if s.pool == nil {
		return RatingResult{}, ErrUnavailable
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return RatingResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	viewerID, err := loadActiveMemberIDTx(ctx, tx, subject)
	if err != nil {
		return RatingResult{}, err
	}
	ownerID, err := lockRatableChannelOwnerTx(ctx, tx, id)
	if err != nil {
		return RatingResult{}, err
	}
	if ownerID == viewerID {
		return RatingResult{}, ErrSelfRating
	}
	result, err := s.upsertChannelRating(ctx, tx, id, viewerID, ownerID, request.Stars)
	if err != nil {
		return RatingResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RatingResult{}, err
	}
	return result, nil
}

// loadActiveMemberIDTx resolves subject to an active member's user ID,
// holding a shared lock through the rest of the rating transaction.
func loadActiveMemberIDTx(ctx context.Context, tx pgx.Tx, subject string) (int64, error) {
	var id int64
	var status string
	err := tx.QueryRow(ctx, `SELECT id,status FROM v3_identity.users
 WHERE external_id=$1 AND deleted_at IS NULL FOR SHARE`, subject).Scan(&id, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrMemberNotFound
	}
	if err != nil {
		return 0, err
	}
	if status != "active" {
		return 0, ErrInactive
	}
	return id, nil
}

// lockRatableChannelOwnerTx locks the channel and its owner row so ownership
// and eligibility cannot change while the rating is applied.
func lockRatableChannelOwnerTx(ctx context.Context, tx pgx.Tx, id string) (int64, error) {
	var ownerID int64
	// Hold ownership and eligibility stable through the upsert, so revocation
	// cannot race the authorization decision and leave an unauthorized rating.
	err := tx.QueryRow(ctx, `SELECT c.owner_user_id `+eligibleChannels+
		` AND c.settings->'community'->>'id'=$1 FOR UPDATE OF c FOR SHARE OF owner`, id).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrChannelNotFound
	}
	if err != nil {
		return 0, err
	}
	return ownerID, nil
}

// upsertChannelRating records the viewer's rating and returns the channel's
// updated rating summary and owner summary.
func (s *Service) upsertChannelRating(ctx context.Context, tx pgx.Tx, id string, viewerID, ownerID int64, stars int) (RatingResult, error) {
	_, err := tx.Exec(ctx, `INSERT INTO v3_community.channel_ratings(channel_id,user_id,stars)
 VALUES ($1,$2,$3) ON CONFLICT (channel_id,user_id) DO UPDATE SET stars=EXCLUDED.stars`, id, viewerID, stars)
	if err != nil {
		return RatingResult{}, err
	}
	var result RatingResult
	err = tx.QueryRow(ctx, `SELECT COALESCE(avg(stars)*2,0)::float8,count(*),
 COALESCE(max(stars) FILTER (WHERE user_id=$2),0) FROM v3_community.channel_ratings WHERE channel_id=$1`, id, viewerID).Scan(
		&result.Channel.AverageScore, &result.Channel.RatingCount, &result.Channel.ViewerStars)
	if err != nil {
		return RatingResult{}, err
	}
	result.Seller, err = ownerSummary(ctx, tx, ownerID)
	if err != nil {
		return RatingResult{}, err
	}
	return result, nil
}
