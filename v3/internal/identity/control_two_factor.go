package identity

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

type TwoFactorSetup struct {
	Secret      string   `json:"secret"`
	QRCodeData  string   `json:"qr_code_data"`
	BackupCodes []string `json:"backup_codes"`
}

type TwoFactorStatus struct {
	Enabled              bool `json:"enabled"`
	Locked               bool `json:"locked"`
	BackupCodesRemaining int  `json:"backup_codes_remaining"`
}

func (c *Control) TwoFactorStatus(ctx context.Context, uid int64) (TwoFactorStatus, error) {
	var out TwoFactorStatus
	err := c.pool.QueryRow(ctx, `SELECT coalesce(t.enabled,false),coalesce(t.locked_until>$2,false),
	 (SELECT count(*) FROM v3_identity.two_factor_backup_codes b WHERE b.user_id=u.id AND b.used_at IS NULL AND t.enabled)
	 FROM v3_identity.users u LEFT JOIN v3_identity.two_factor t ON t.user_id=u.id WHERE u.id=$1 AND u.deleted_at IS NULL`, uid, c.cfg.Now()).
		Scan(&out.Enabled, &out.Locked, &out.BackupCodesRemaining)
	return out, controlDBError(err)
}

func (c *Control) SetupTwoFactor(ctx context.Context, u User) (TwoFactorSetup, error) {
	secret, err := newTwoFactorSecret()
	if err != nil {
		return TwoFactorSetup{}, err
	}
	ciphertext, err := c.encryptKey(secret)
	if err != nil {
		return TwoFactorSetup{}, err
	}
	codes, err := newBackupCodes()
	if err != nil {
		return TwoFactorSetup{}, err
	}
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return TwoFactorSetup{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Lock the user even when no factor row exists; simultaneous setups cannot interleave.
	var id int64
	if err = tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 AND status='active' AND deleted_at IS NULL FOR UPDATE`, u.ID).Scan(&id); err != nil {
		return TwoFactorSetup{}, controlDBError(err)
	}
	var enabled bool
	if err = tx.QueryRow(ctx, `SELECT coalesce((SELECT enabled FROM v3_identity.two_factor WHERE user_id=$1),false)`, u.ID).Scan(&enabled); err != nil {
		return TwoFactorSetup{}, err
	}
	if enabled {
		return TwoFactorSetup{}, ErrDuplicate
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_identity.two_factor(user_id,secret_ciphertext) VALUES($1,$2)
	 ON CONFLICT(user_id) DO UPDATE SET secret_ciphertext=excluded.secret_ciphertext,failed_attempts=0,locked_until=NULL,last_counter=-1`, u.ID, ciphertext)
	if err != nil {
		return TwoFactorSetup{}, err
	}
	if err = replaceBackupCodes(ctx, tx, u.ID, codes); err != nil {
		return TwoFactorSetup{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return TwoFactorSetup{}, err
	}
	return TwoFactorSetup{secret, twoFactorURI(secret, u.Username), codes}, nil
}

func replaceBackupCodes(ctx context.Context, tx pgx.Tx, uid int64, codes []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM v3_identity.two_factor_backup_codes WHERE user_id=$1`, uid); err != nil {
		return err
	}
	for _, code := range codes {
		hash, err := bcrypt.GenerateFromPassword([]byte(normalizeBackupCode(code)), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO v3_identity.two_factor_backup_codes(user_id,code_hash) VALUES($1,$2)`, uid, string(hash)); err != nil {
			return err
		}
	}
	return nil
}

// verifyTwoFactorTx serializes code use, including imported bcrypt backup hashes.
// The caller commits ErrCredentials to persist failed-attempt lockout, but rolls back other errors.
func (c *Control) verifyTwoFactorTx(ctx context.Context, tx pgx.Tx, uid int64, code string, enabled, allowBackup bool) error {
	var ciphertext []byte
	var actual bool
	var attempts int
	var locked *time.Time
	var last int64
	err := tx.QueryRow(ctx, `SELECT secret_ciphertext,enabled,failed_attempts,locked_until,last_counter FROM v3_identity.two_factor WHERE user_id=$1 FOR UPDATE`, uid).
		Scan(&ciphertext, &actual, &attempts, &locked, &last)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCredentials
	}
	if err != nil {
		return err
	}
	if actual != enabled || locked != nil && locked.After(c.cfg.Now()) {
		return ErrCredentials
	}
	if locked != nil {
		attempts = 0
	}
	secret, err := c.decryptKey(ciphertext)
	if err != nil {
		return err
	}
	counter, valid := totpCounter(secret, code, c.cfg.Now(), last)
	if !valid && allowBackup && normalizeBackupCode(code) != "" {
		valid, err = consumeBackupCode(ctx, tx, uid, normalizeBackupCode(code))
		if err != nil {
			return err
		}
		counter = last
	}
	if !valid {
		attempts++
		var until *time.Time
		if attempts >= 5 {
			value := c.cfg.Now().Add(5 * time.Minute)
			until = &value
		}
		_, err = tx.Exec(ctx, `UPDATE v3_identity.two_factor SET failed_attempts=$2,locked_until=$3 WHERE user_id=$1`, uid, attempts, until)
		if err != nil {
			return err
		}
		return ErrCredentials
	}
	_, err = tx.Exec(ctx, `UPDATE v3_identity.two_factor SET failed_attempts=0,locked_until=NULL,last_counter=$2,last_used_at=$3 WHERE user_id=$1`, uid, counter, c.cfg.Now())
	return err
}

func consumeBackupCode(ctx context.Context, tx pgx.Tx, uid int64, code string) (bool, error) {
	rows, err := tx.Query(ctx, `SELECT id,code_hash FROM v3_identity.two_factor_backup_codes WHERE user_id=$1 AND used_at IS NULL ORDER BY id`, uid)
	if err != nil {
		return false, err
	}
	var matched int64
	for rows.Next() {
		var id int64
		var hash string
		if err = rows.Scan(&id, &hash); err != nil {
			rows.Close()
			return false, err
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(code)) == nil {
			matched = id
			break
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil || matched == 0 {
		return false, err
	}
	_, err = tx.Exec(ctx, `UPDATE v3_identity.two_factor_backup_codes SET used_at=now() WHERE id=$1 AND used_at IS NULL`, matched)
	return err == nil, err
}

func (c *Control) twoFactorOperation(ctx context.Context, uid int64, code string, enable, backup bool, apply func(pgx.Tx) error) error {
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = c.verifyTwoFactorTx(ctx, tx, uid, code, enable, backup)
	if err != nil && !errors.Is(err, ErrCredentials) {
		return err
	}
	if err == nil {
		err = apply(tx)
		if err != nil {
			return err
		}
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		return commitErr
	}
	return err
}

func (c *Control) EnableTwoFactor(ctx context.Context, uid int64, code string) error {
	return c.twoFactorOperation(ctx, uid, code, false, false, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE v3_identity.two_factor SET enabled=true WHERE user_id=$1`, uid)
		if err != nil {
			return err
		}
		// Enabling the second factor invalidates sessions authenticated before setup.
		_, err = tx.Exec(ctx, `UPDATE v3_identity.sessions SET revoked_at=$2 WHERE user_id=$1 AND revoked_at IS NULL`, uid, c.cfg.Now())
		return err
	})
}

func (c *Control) DisableTwoFactor(ctx context.Context, uid int64, code string) error {
	return c.twoFactorOperation(ctx, uid, code, true, true, func(tx pgx.Tx) error { return c.removeTwoFactorTx(ctx, tx, uid) })
}

func (c *Control) RegenerateTwoFactorBackupCodes(ctx context.Context, uid int64, code string) ([]string, error) {
	codes, err := newBackupCodes()
	if err != nil {
		return nil, err
	}
	err = c.twoFactorOperation(ctx, uid, code, true, false, func(tx pgx.Tx) error { return replaceBackupCodes(ctx, tx, uid, codes) })
	if err != nil {
		return nil, err
	}
	return codes, nil
}

func (c *Control) removeTwoFactorTx(ctx context.Context, tx pgx.Tx, uid int64) error {
	tag, err := tx.Exec(ctx, `DELETE FROM v3_identity.two_factor WHERE user_id=$1`, uid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM v3_identity.login_challenges WHERE user_id=$1`, uid); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE v3_identity.sessions SET revoked_at=$2 WHERE user_id=$1 AND revoked_at IS NULL`, uid, c.cfg.Now())
	return err
}

func (c *Control) AdminDisableTwoFactor(ctx context.Context, actor User, target int64) error {
	if !actor.IsAdmin() || actor.ID == target {
		return ErrForbidden
	}
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var role string
	err = tx.QueryRow(ctx, `SELECT role FROM v3_identity.users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, target).Scan(&role)
	if err != nil {
		return controlDBError(err)
	}
	if actor.Role != "root" && role != "user" {
		return ErrForbidden
	}
	if err = c.removeTwoFactorTx(ctx, tx, target); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
