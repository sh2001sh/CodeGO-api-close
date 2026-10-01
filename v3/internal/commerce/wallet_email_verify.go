package commerce

import (
	"context"
	"crypto/hmac"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *EmailWalletRecovery) VerifyTx(ctx context.Context, tx pgx.Tx, uid int64, code string) error {
	email, err := walletBoundEmail(ctx, tx, uid)
	if err != nil {
		return err
	}
	var recipient, purpose, state string
	var hash, salt []byte
	var failed int
	var expires time.Time
	var locked *time.Time
	err = tx.QueryRow(ctx, `SELECT recipient_email,purpose,state,code_hash,salt,failed_attempts,expires_at,locked_until FROM v3_commerce.wallet_recovery_codes WHERE user_id=$1 FOR UPDATE`, uid).Scan(&recipient, &purpose, &state, &hash, &salt, &failed, &expires, &locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrWalletEmailCodeInvalid
	}
	if err != nil {
		return err
	}
	now := s.cfg.Now()
	if locked != nil && locked.After(now) {
		return ErrWalletEmailCodeLocked
	}
	if state != "active" || purpose != walletEmailPurpose {
		return ErrWalletEmailCodeInvalid
	}
	if recipient != email || !expires.After(now) {
		if _, err = tx.Exec(ctx, `UPDATE v3_commerce.wallet_recovery_codes SET state='expired' WHERE user_id=$1 AND state='active'`, uid); err != nil {
			return err
		}
		return ErrWalletEmailCodeInvalid
	}
	valid := len(code) == 6
	for _, c := range code {
		valid = valid && c >= '0' && c <= '9'
	}
	if !valid || !hmac.Equal(hash, s.codeHash(uid, email, code, salt)) {
		failed++
		state = "active"
		if failed >= walletMaxFailures {
			value := now.Add(30 * time.Minute)
			locked = &value
			state = "locked"
		}
		_, err = tx.Exec(ctx, `UPDATE v3_commerce.wallet_recovery_codes SET failed_attempts=$2,locked_until=$3,state=$4 WHERE user_id=$1`, uid, failed, locked, state)
		if err != nil {
			return err
		}
		if locked != nil {
			return ErrWalletEmailCodeLocked
		}
		return ErrWalletEmailCodeInvalid
	}
	tag, err := tx.Exec(ctx, `UPDATE v3_commerce.wallet_recovery_codes SET state='consumed',consumed_at=$2 WHERE user_id=$1 AND state='active'`, uid, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrWalletEmailCodeInvalid
	}
	return nil
}
