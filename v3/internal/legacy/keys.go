package legacy

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"

	"github.com/jackc/pgx/v5"
)

type sourceKey struct {
	ID              int64       `json:"id"`
	UserID          int64       `json:"user_id"`
	Key             string      `json:"key"`
	Name            string      `json:"name"`
	Status          int         `json:"status"`
	CreatedTime     int64       `json:"created_time"`
	AccessedTime    int64       `json:"accessed_time"`
	ExpiredTime     int64       `json:"expired_time"`
	RemainUnits     int64       `json:"remain_quota"`
	Unlimited       bool        `json:"unlimited_quota"`
	ModelsEnabled   bool        `json:"model_limits_enabled"`
	Models          string      `json:"model_limits"`
	AllowIPs        *string     `json:"allow_ips"`
	Group           string      `json:"group"`
	CrossGroupRetry bool        `json:"cross_group_retry"`
	Multiplier      json.Number `json:"marketplace_multiplier_limit"`
	DeletedAt       *string     `json:"deleted_at"`
}

func decodeKey(row json.RawMessage) (sourceKey, string, []string, error) {
	var k sourceKey
	if err := json.Unmarshal(row, &k); err != nil {
		return k, "", nil, err
	}
	if k.ID <= 0 || k.UserID <= 0 {
		return k, "", nil, fmt.Errorf("legacy: API key ID and owner must be positive")
	}
	key := strings.TrimSpace(k.Key)
	if key == "" {
		return k, "", nil, fmt.Errorf("legacy: API key %d has empty secret", k.ID)
	}
	if !strings.HasPrefix(key, "sk-") {
		key = "sk-" + key
	}
	var cidrs []string
	if k.AllowIPs != nil {
		for _, raw := range list(*k.AllowIPs) {
			prefix, err := netip.ParsePrefix(raw)
			if err != nil {
				address, addrErr := netip.ParseAddr(raw)
				if addrErr != nil {
					return k, "", nil, fmt.Errorf("legacy: API key %d has invalid allowed address", k.ID)
				}
				prefix = netip.PrefixFrom(address, address.BitLen())
			}
			cidrs = append(cidrs, prefix.Masked().String())
		}
	}
	if !k.Unlimited {
		if _, err := OpeningBalance(k.RemainUnits); err != nil {
			return k, "", nil, fmt.Errorf("legacy: API key %d budget: %w", k.ID, err)
		}
	}
	return k, key, cidrs, nil
}

func (m *Importer) importKeys(ctx context.Context, tx pgx.Tx, rows []json.RawMessage) error {
	for _, row := range rows {
		k, key, cidrs, err := decodeKey(row)
		if err != nil {
			return err
		}
		ciphertext, err := m.crypto.Encrypt([]byte(key))
		if err != nil {
			return err
		}
		hash := sha256.Sum256([]byte(key))
		prefix := key
		if len(prefix) > 8 {
			prefix = prefix[:8]
		}
		status := "active"
		if k.Status != 1 {
			status = "disabled"
		}
		var models []string
		if k.ModelsEnabled {
			models = list(k.Models)
			if models == nil {
				models = []string{}
			}
		}
		multiplier := k.Multiplier.String()
		if multiplier == "" {
			multiplier = "0"
		}
		_, err = tx.Exec(ctx, `INSERT INTO v3_identity.api_keys
			(id,user_id,name,key_hash,key_prefix,key_ciphertext,status,group_name,cross_group_retry,allowed_models,allowed_cidrs,budget_limited,max_marketplace_multiplier,
			expires_at,last_used_at,created_at,deleted_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9,$10,$11::cidr[],$12,$13::numeric,
			CASE WHEN $14::bigint>0 THEN to_timestamp($14) END,CASE WHEN $15::bigint>0 THEN to_timestamp($15) END,
			CASE WHEN $16::bigint>0 THEN to_timestamp($16) ELSE now() END,$17::timestamptz) ON CONFLICT(id) DO NOTHING`,
			k.ID, k.UserID, k.Name, hash[:], prefix, ciphertext, status, k.Group, k.CrossGroupRetry, models, cidrs, !k.Unlimited, multiplier, k.ExpiredTime, k.AccessedTime, k.CreatedTime, k.DeletedAt)
		if err != nil {
			return fmt.Errorf("legacy: import API key %d: %w", k.ID, err)
		}
		var actualHash []byte
		var actualUser int64
		if err = tx.QueryRow(ctx, `SELECT key_hash,user_id FROM v3_identity.api_keys WHERE id=$1`, k.ID).Scan(&actualHash, &actualUser); err != nil {
			return err
		}
		if string(actualHash) != string(hash[:]) || actualUser != k.UserID {
			return fmt.Errorf("legacy: target API key ID %d differs from the source identity", k.ID)
		}
		if !k.Unlimited {
			amount, _ := OpeningBalance(k.RemainUnits)
			if err = opening(ctx, tx, "api_key", k.ID, "key_budget", int64(amount)); err != nil {
				return err
			}
		}
	}
	return nil
}
