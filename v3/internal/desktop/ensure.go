package desktop

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/identity"
)

// A transaction lock and key creation use one connection, so concurrent
// ensure calls cannot exhaust a pool while the winner waits for another slot.
func (s *Service) ensureKey(ctx context.Context, uid int64, name, group string) (identity.KeyRecord, string, bool, error) {
	var k identity.KeyRecord
	var raw string
	created := false
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		lock := "desktop-key:" + strconv.FormatInt(uid, 10) + ":" + name
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,7632))`, lock); err != nil {
			return err
		}
		var authorized bool
		query := `SELECT EXISTS(SELECT 1 FROM v3_identity.allowed_groups($1) g WHERE g=$2)`
		args := []any{uid, group}
		retry := false
		switch group {
		case "":
			authorized = true
		case "auto":
			query = `SELECT EXISTS(SELECT 1 FROM v3_identity.auto_groups($1))`
			args = []any{uid}
			retry = true
		}
		if !authorized {
			if err := tx.QueryRow(ctx, query, args...).Scan(&authorized); err != nil {
				return err
			}
		}
		if !authorized {
			return ErrDenied
		}
		var sealed []byte
		err := tx.QueryRow(ctx, `SELECT id,key_ciphertext FROM v3_identity.api_keys WHERE user_id=$1 AND name=$2 AND deleted_at IS NULL ORDER BY id LIMIT 1`, uid, name).Scan(&k.ID, &sealed)
		if err == nil {
			k, err = scanOwnedKey(tx.QueryRow(ctx, keySelect+` WHERE k.user_id=$1 AND k.id=$2`, uid, k.ID))
			if err != nil {
				return err
			}
			if k.Status != "active" || k.ExpiresAt != nil && !k.ExpiresAt.After(s.cfg.Now()) {
				return ErrDenied
			}
			b, err := s.cfg.Crypto.Decrypt(sealed)
			raw = string(b)
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var hash [32]byte
		var prefix string
		var errGen error
		raw, hash, prefix, errGen = identity.GenerateKey()
		if errGen != nil {
			return errGen
		}
		sealed, err = s.cfg.Crypto.Encrypt([]byte(raw))
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `INSERT INTO v3_identity.api_keys(user_id,name,key_hash,key_prefix,key_ciphertext,group_name,cross_group_retry)
		 VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7) RETURNING id`, uid, name, hash[:], prefix, sealed, group, retry).Scan(&k.ID)
		if err != nil {
			return err
		}
		created = true
		k, err = scanOwnedKey(tx.QueryRow(ctx, keySelect+` WHERE k.id=$1`, k.ID))
		return err
	})
	if err != nil {
		return identity.KeyRecord{}, "", false, err
	}
	return k, raw, created, nil
}
