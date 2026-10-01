// Package community exposes a narrow public bridge for NodeBB. All permission
// decisions use current PostgreSQL state and the dedicated service credential.
package community

import (
	"context"
	"crypto/subtle"
	"errors"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrDisabled          = errors.New("community: disabled")
	ErrUnauthorized      = errors.New("community: unauthorized")
	ErrInvalidSubject    = errors.New("community: invalid subject")
	ErrInvalidQuery      = errors.New("community: invalid query")
	ErrInvalidPagination = errors.New("community: invalid pagination")
	ErrInvalidRating     = errors.New("community: invalid rating")
	ErrMemberNotFound    = errors.New("community: member not found")
	ErrInactive          = errors.New("community: member inactive")
	ErrChannelNotFound   = errors.New("community: channel not found")
	ErrSelfRating        = errors.New("community: self-rating prohibited")
	ErrUnavailable       = errors.New("community: unavailable")
)

type Config struct{ ServiceSecret string }

type Service struct {
	pool *pgxpool.Pool
	cfg  Config
}

func New(pool *pgxpool.Pool, cfg Config) *Service {
	cfg.ServiceSecret = strings.TrimSpace(cfg.ServiceSecret)
	return &Service{pool: pool, cfg: cfg}
}

func (s *Service) Authorize(candidate string) error {
	if len(s.cfg.ServiceSecret) < 32 {
		return ErrDisabled
	}
	if subtle.ConstantTimeCompare([]byte(candidate), []byte(s.cfg.ServiceSecret)) != 1 {
		return ErrUnauthorized
	}
	return nil
}

type Member struct {
	Subject              string  `json:"sub"`
	Active               bool    `json:"active"`
	Username             string  `json:"username,omitempty"`
	DisplayName          string  `json:"display_name,omitempty"`
	VerifiedChannelOwner bool    `json:"verified_channel_owner"`
	AverageScore         float64 `json:"average_score"`
	RatingCount          int64   `json:"rating_count"`
}

type Channel struct {
	ID                 string  `json:"id"`
	Slug               string  `json:"slug"`
	Name               string  `json:"name"`
	Provider           string  `json:"provider"`
	LifecycleStatus    string  `json:"lifecycle_status"`
	VerificationStatus string  `json:"verification_status"`
	AverageScore       float64 `json:"average_score"`
	RatingCount        int64   `json:"rating_count"`
	ViewerStars        int     `json:"viewer_stars"`
}

type ChannelList struct {
	Items    []Channel `json:"items"`
	Total    int64     `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
}

type Seller struct {
	Subject      string    `json:"sub"`
	Username     string    `json:"username"`
	DisplayName  string    `json:"display_name"`
	ChannelCount int64     `json:"channel_count"`
	Channels     []Channel `json:"channels"`
	AverageScore float64   `json:"average_score"`
	RatingCount  int64     `json:"rating_count"`
}

type SellerList struct {
	Items    []Seller `json:"items"`
	Total    int64    `json:"total"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
}

type RatingRequest struct {
	ViewerSubject string `json:"viewer_sub"`
	Stars         int    `json:"stars"`
}

type RatingSummary struct {
	AverageScore float64 `json:"average_score"`
	RatingCount  int64   `json:"rating_count"`
	ViewerStars  int     `json:"viewer_stars"`
}

type RatingResult struct {
	Channel RatingSummary `json:"channel"`
	Seller  RatingSummary `json:"seller"`
}

func normalizeSubject(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) != 6 {
		return "", ErrInvalidSubject
	}
	for _, c := range value {
		if !strings.ContainsRune("23456789ABCDEFGHJKLMNPQRSTUVWXYZ", c) {
			return "", ErrInvalidSubject
		}
	}
	return value, nil
}

var phonePattern = regexp.MustCompile(`^\+?[0-9][0-9\s().-]{5,}[0-9]$`)
var providerPattern = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)

func publicIdentity(subject, username, display string) (string, string) {
	username, display = strings.TrimSpace(username), strings.TrimSpace(display)
	if username == "" || strings.Contains(username, "@") || phonePattern.MatchString(username) {
		username = "member-" + subject
	}
	if display == "" || strings.Contains(display, "@") || phonePattern.MatchString(display) {
		display = username
	}
	return username, display
}

// Public metadata is published by catalog administration under settings.community.
// It contains only display identity and lifecycle, never URLs or credentials.
const eligibleChannels = `FROM v3_catalog.channels c
 JOIN v3_identity.users owner ON owner.id=c.owner_user_id
 WHERE c.scope='marketplace' AND c.status='enabled'
 AND owner.status='active' AND owner.deleted_at IS NULL AND COALESCE(owner.external_id,'')<>''
 AND c.settings->'community'->>'visibility'='public'
 AND c.settings->'community'->>'verification_status'='passed'
 AND c.settings->'community'->>'lifecycle_status' IN ('active','degraded')
 AND COALESCE(c.settings->'community'->>'id','')<>''`

type memberRecord struct {
	ID                                 int64
	Subject, Username, Display, Status string
}
type rowQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func loadMember(ctx context.Context, db rowQueryer, subject string) (memberRecord, error) {
	subject, err := normalizeSubject(subject)
	if err != nil {
		return memberRecord{}, err
	}
	var u memberRecord
	err = db.QueryRow(ctx, `SELECT id, external_id, username, display_name, status
 FROM v3_identity.users WHERE external_id=$1 AND deleted_at IS NULL`, subject).Scan(
		&u.ID, &u.Subject, &u.Username, &u.Display, &u.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, ErrMemberNotFound
	}
	return u, err
}

func (s *Service) GetMember(ctx context.Context, subject string) (Member, error) {
	if _, err := normalizeSubject(subject); err != nil {
		return Member{}, err
	}
	if s.pool == nil {
		return Member{}, ErrUnavailable
	}
	u, err := loadMember(ctx, s.pool, subject)
	if err != nil {
		return Member{}, err
	}
	m := Member{Subject: u.Subject, Active: u.Status == "active"}
	if !m.Active {
		return m, nil
	}
	m.Username, m.DisplayName = publicIdentity(u.Subject, u.Username, u.Display)
	err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 `+eligibleChannels+` AND c.owner_user_id=$1)`, u.ID).Scan(&m.VerifiedChannelOwner)
	if err != nil {
		return Member{}, err
	}
	summary, err := ownerSummary(ctx, s.pool, u.ID)
	m.AverageScore, m.RatingCount = summary.AverageScore, summary.RatingCount
	return m, err
}

func ownerSummary(ctx context.Context, db rowQueryer, ownerID int64) (RatingSummary, error) {
	var result RatingSummary
	err := db.QueryRow(ctx, `SELECT COALESCE(avg(r.stars)*2,0)::float8, count(r.user_id)
 FROM v3_community.channel_ratings r JOIN (SELECT c.settings->'community'->>'id' AS public_id `+
		eligibleChannels+` AND c.owner_user_id=$1) channels ON channels.public_id=r.channel_id`, ownerID).Scan(&result.AverageScore, &result.RatingCount)
	return result, err
}
