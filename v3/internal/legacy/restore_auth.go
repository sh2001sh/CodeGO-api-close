package legacy

import (
	"encoding/base32"
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func projectRestoredFactor(raw json.RawMessage) (restoredRecord, error) {
	var old struct {
		UserID         int64        `json:"user_id"`
		Secret         string       `json:"secret"`
		Enabled        bool         `json:"is_enabled"`
		FailedAttempts int64        `json:"failed_attempts"`
		LockedUntil    *historyTime `json:"locked_until"`
		LastUsedAt     *historyTime `json:"last_used_at"`
		CreatedAt      historyTime  `json:"created_at"`
	}
	r := restoredRecord{table: "v3_identity.two_factor", key: "user_id", secretColumn: "secret_ciphertext"}
	if err := securityRequiredFields(raw, "user_id", "secret", "is_enabled", "failed_attempts"); err != nil {
		return r, err
	}
	if json.Unmarshal(raw, &old) != nil {
		return r, fmt.Errorf("invalid source two-factor fields")
	}
	r.id = old.UserID
	if old.UserID <= 0 || old.FailedAttempts < 0 || old.FailedAttempts > 2147483647 || old.Secret == "" {
		return r, fmt.Errorf("invalid two-factor identifiers or counters")
	}
	counter := int64(-1)
	if old.LastUsedAt != nil && !old.LastUsedAt.IsZero() {
		if old.LastUsedAt.Unix() < 0 {
			return r, fmt.Errorf("two-factor last usage precedes epoch")
		}
		// v2 accepts a one-step future TOTP. Refuse all potentially accepted
		// counters at cutover, including that future step, to prevent replay.
		counter = old.LastUsedAt.Unix()/30 + 1
	}
	r.secret = old.Secret
	r.fields = map[string]any{"user_id": old.UserID, "enabled": old.Enabled, "failed_attempts": old.FailedAttempts, "locked_until": restoredOptionalTime(old.LockedUntil), "last_counter": counter, "last_used_at": restoredOptionalTime(old.LastUsedAt), "created_at": historyDate(old.CreatedAt)}
	return r, nil
}

func projectRestoredBackup(raw json.RawMessage) (restoredRecord, error) {
	var old struct {
		ID       int64        `json:"id"`
		UserID   int64        `json:"user_id"`
		CodeHash string       `json:"code_hash"`
		Used     bool         `json:"is_used"`
		UsedAt   *historyTime `json:"used_at"`
	}
	r := restoredRecord{table: "v3_identity.two_factor_backup_codes", key: "id"}
	if err := securityRequiredFields(raw, "id", "user_id", "code_hash", "is_used"); err != nil {
		return r, err
	}
	if json.Unmarshal(raw, &old) != nil {
		return r, fmt.Errorf("invalid backup-code fields")
	}
	r.id = old.ID
	if old.ID <= 0 || old.UserID <= 0 {
		return r, fmt.Errorf("invalid backup-code identifiers")
	}
	if _, err := bcrypt.Cost([]byte(old.CodeHash)); err != nil {
		return r, fmt.Errorf("backup-code hash is not bcrypt")
	}
	usedAt := restoredOptionalTime(old.UsedAt)
	if old.Used != (usedAt != nil) {
		return r, fmt.Errorf("backup-code consumption flag differs from usage timestamp")
	}
	r.fields = map[string]any{"id": old.ID, "user_id": old.UserID, "code_hash": old.CodeHash, "used_at": usedAt}
	return r, nil
}

func validateRestoredTOTP(secret string) error {
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimRight(secret, "=")))
	if err != nil || len(decoded) < 10 || len(decoded) > 64 {
		return fmt.Errorf("source TOTP secret is invalid")
	}
	return nil
}

func restoredOptionalTime(value *historyTime) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.Time
}
