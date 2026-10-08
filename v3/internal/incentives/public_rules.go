package incentives

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

func publicRules(c Settings) map[string]json.RawMessage {
	raw, _ := json.Marshal(c)
	m := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &m)
	for _, key := range []string{"enabled", "timezone", "draw_hour", "draw_minute", "cost_per_usd", "monthly_budget_usd"} {
		delete(m, key)
	}
	return m
}

// AffiliateCode allocates once under the user's row lock; concurrent requests
// and later reads keep the same invitation attribution.
func (s *Service) AffiliateCode(ctx context.Context, user int64) (string, error) {
	if user <= 0 {
		return "", ErrInvalid
	}
	var existing *string
	if err := s.pool.QueryRow(ctx, `SELECT aff_code FROM v3_identity.users WHERE id=$1 AND status='active' AND deleted_at IS NULL`, user).Scan(&existing); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	if existing != nil && *existing != "" {
		return *existing, nil
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	var code string
	err := s.pool.QueryRow(ctx, `UPDATE v3_identity.users SET aff_code=CASE WHEN aff_code IS NULL OR aff_code='' THEN $2 ELSE aff_code END WHERE id=$1 AND status='active' AND deleted_at IS NULL RETURNING aff_code`, user, hex.EncodeToString(b[:])).Scan(&code)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return code, err
}
