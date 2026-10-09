// Package channelmarket owns user supplied channels, private group access and
// owner income. Shared channel secrets stay in the catalog credential table.
package channelmarket

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing"
)

var (
	ErrInvalid     = errors.New("channelmarket: invalid input")
	ErrNotFound    = errors.New("channelmarket: resource not found or inaccessible")
	ErrConflict    = errors.New("channelmarket: state conflict")
	ErrUnavailable = errors.New("channelmarket: dependency unavailable")
)

type Poster interface {
	PostTx(context.Context, pgx.Tx, billing.Entry) (billing.PostResult, error)
}

type Encrypter interface{ Encrypt([]byte) ([]byte, error) }
type Config struct {
	Now             func() time.Time
	Hold            time.Duration
	Probe           func(context.Context, ProbeRequest) (ModelTest, error)
	ListModels      func(context.Context, FetchModelsRequest) ([]string, error)
	BatchRelay      func(context.Context, BatchRelayRequest) (BatchReceipt, error)
	IssueKey        func(context.Context, int64, string) (BoundToken, error)
	GiftBoxes       func(context.Context, int64, int64, string, int) ([]int64, error)
	WelfareTransfer func(context.Context, int64, string, int64, string, string) error
	// SecurityAuditHandler serves retained global audit records at legacy aliases.
	SecurityAuditHandler http.Handler
}
type Actor struct {
	UserID int64
	Admin  bool
}
type Service struct {
	pool   *pgxpool.Pool
	enc    Encrypter
	poster Poster
	cfg    Config
	log    *slog.Logger
}

func New(pool *pgxpool.Pool, enc Encrypter, poster Poster, cfg Config, log *slog.Logger) *Service {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Hold <= 0 {
		cfg.Hold = 24 * time.Hour
	}
	if cfg.Probe == nil {
		cfg.Probe = probe
		if cfg.ListModels == nil {
			cfg.ListModels = FetchModels
		}
	}
	if log == nil {
		log = slog.Default()
	}
	return &Service{pool: pool, enc: enc, poster: poster, cfg: cfg, log: log}
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (s *Service) transaction(ctx context.Context, fn func(pgx.Tx) error) error {
	if s.pool == nil {
		return ErrUnavailable
	}
	return pgx.BeginFunc(ctx, s.pool, fn)
}

func owned(ctx context.Context, tx pgx.Tx, a Actor, channel int64) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT g.id FROM v3_channelmarket.groups g JOIN v3_catalog.channels c ON c.id=g.channel_id WHERE g.channel_id=$1 AND g.deleted_at IS NULL AND g.lifecycle_status<>'deleted' AND ($2 OR g.owner_user_id=$3) FOR UPDATE OF g`, channel, a.Admin, a.UserID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

func accessible(ctx context.Context, tx pgx.Tx, user int64, group string) error {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_channelmarket.groups g JOIN v3_catalog.channels c ON c.id=g.channel_id
	WHERE g.id=$1 AND g.deleted_at IS NULL AND g.lifecycle_status='active' AND c.status='enabled'
	AND (g.visibility='public' OR g.owner_user_id=$2 OR EXISTS(SELECT 1 FROM v3_channelmarket.group_access a WHERE a.group_id=g.id AND a.user_id=$2))
	AND NOT EXISTS(SELECT 1 FROM v3_channelmarket.channel_user_blocks b WHERE b.channel_id=c.id AND b.user_id=$2))`, group, user).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}
