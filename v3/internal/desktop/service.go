// Package desktop serves the retained desktop client protocol with separate,
// scoped device grants. A desktop bearer never grants browser/admin privileges.
package desktop

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/audit"
	"github.com/sh2001sh/new-api/v3/internal/identity"
)

var (
	ErrInvalid         = errors.New("desktop: invalid input")
	ErrDenied          = errors.New("desktop: authorization denied")
	ErrMissing         = errors.New("desktop: record not found")
	ErrUnauthenticated = fmt.Errorf("%w: no active device credential", ErrDenied)
)

type Crypto interface {
	Encrypt([]byte) ([]byte, error)
	Decrypt([]byte) ([]byte, error)
}

type Config struct {
	PublicURL       string
	Crypto          Crypto
	Now             func() time.Time
	ReleaseManifest func(context.Context) (any, error)
}

type Service struct {
	pool    *pgxpool.Pool
	id      *identity.Control
	cfg     Config
	audit   *audit.Service
	limiter limiter
}

func New(pool *pgxpool.Pool, id *identity.Control, cfg Config) *Service {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	cfg.PublicURL = strings.TrimRight(cfg.PublicURL, "/")
	return &Service{pool: pool, id: id, cfg: cfg, audit: audit.New(pool, audit.Config{})}
}

var defaultScopes = []string{"account:read", "logs:read", "tokens:read", "tokens:write", "config:read", "config:write", "telemetry:write"}

type Device struct {
	ID         int64    `json:"id"`
	UserID     int64    `json:"-"`
	DeviceName string   `json:"device_name"`
	Platform   string   `json:"platform"`
	AppVersion string   `json:"app_version"`
	Scopes     []string `json:"scopes"`
	Status     string   `json:"status"`
	CreatedAt  int64    `json:"created_at"`
	LastUsedAt int64    `json:"last_used_at"`
	ExpiresAt  int64    `json:"expires_at"`
	RevokedAt  int64    `json:"revoked_at"`
}

func token(prefix string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}
func digest(raw string) []byte { h := sha256.Sum256([]byte(raw)); return h[:] }
func dbError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMissing
	}
	return err
}

func (s *Service) Authenticate(r *http.Request, scope string) (Device, error) {
	raw := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(raw) >= 7 && strings.EqualFold(raw[:7], "bearer ") {
		raw = strings.TrimSpace(raw[7:])
	}
	if !strings.HasPrefix(raw, "desktop_") {
		return Device{}, ErrUnauthenticated
	}
	var d Device
	err := s.pool.QueryRow(r.Context(), `UPDATE v3_identity.desktop_devices d SET last_used_at=$2
	 FROM v3_identity.users u WHERE d.token_hash=$1 AND d.user_id=u.id AND u.status='active' AND u.deleted_at IS NULL
	 AND d.revoked_at IS NULL AND d.expires_at>$2
	 RETURNING d.id,d.user_id,d.device_name,d.platform,d.app_version,d.scopes,
	 extract(epoch FROM d.created_at)::bigint,extract(epoch FROM d.last_used_at)::bigint,extract(epoch FROM d.expires_at)::bigint`, digest(raw), s.cfg.Now()).
		Scan(&d.ID, &d.UserID, &d.DeviceName, &d.Platform, &d.AppVersion, &d.Scopes, &d.CreatedAt, &d.LastUsedAt, &d.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Device{}, ErrUnauthenticated
	}
	if err == nil && !slices.Contains(d.Scopes, scope) {
		return Device{}, ErrDenied
	}
	d.Status = "active"
	return d, err
}

func (s *Service) Devices(ctx context.Context, uid int64) ([]Device, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,user_id,device_name,platform,app_version,scopes,
	 CASE WHEN revoked_at IS NOT NULL THEN 'revoked' WHEN expires_at<=$2 THEN 'expired' ELSE 'active' END,
	 extract(epoch FROM created_at)::bigint,extract(epoch FROM last_used_at)::bigint,extract(epoch FROM expires_at)::bigint,
	 coalesce(extract(epoch FROM revoked_at)::bigint,0) FROM v3_identity.desktop_devices WHERE user_id=$1 ORDER BY id DESC`, uid, s.cfg.Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.UserID, &d.DeviceName, &d.Platform, &d.AppVersion, &d.Scopes, &d.Status, &d.CreatedAt, &d.LastUsedAt, &d.ExpiresAt, &d.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Service) Revoke(ctx context.Context, uid, id int64) error {
	if id <= 0 {
		return ErrInvalid
	}
	tag, err := s.pool.Exec(ctx, `UPDATE v3_identity.desktop_devices SET revoked_at=coalesce(revoked_at,$3) WHERE user_id=$1 AND id=$2`, uid, id, s.cfg.Now())
	if err == nil && tag.RowsAffected() == 0 {
		return ErrMissing
	}
	return err
}
