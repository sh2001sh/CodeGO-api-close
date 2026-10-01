package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// A persisted intent keeps its original authorization when the operator later
// disables new conversions. Recovery must still finish or replay that intent.
func (s *Service) allowSubscriptionConversion(ctx context.Context, user, subscription int64, key string) error {
	var authorized bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_commerce.subscription_operations
	 WHERE operation_id=$1 AND actor_id=$2 AND subscription_id=$3 AND kind='conversion')`, key, user, subscription).Scan(&authorized); err != nil {
		return err
	}
	if authorized {
		return nil
	}
	if s.cfg.SubscriptionConversionDisabled {
		return ErrInvalid
	}
	var raw []byte
	var sensitive bool
	err := s.pool.QueryRow(ctx, `SELECT value,sensitive FROM v3_platform.settings WHERE key='SubscriptionClaudeConversionEnabled'`).Scan(&raw, &sensitive)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // The source default allows conversion when no option exists.
	}
	if err != nil {
		return err
	}
	if sensitive {
		return ErrInvalid
	}
	value := string(raw)
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return ErrInvalid
		}
	}
	enabled, err := strconv.ParseBool(value)
	if err != nil || !enabled {
		return ErrInvalid
	}
	return nil
}
