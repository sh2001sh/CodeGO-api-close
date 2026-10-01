package commerce

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

type walletCredential struct {
	hash     string
	failures int
	locked   *time.Time
}

// ConfigureWalletPassword checks the account credential for first setup and
// the existing payment credential for changes. Failed checks commit their
// counters, while any database failure rolls the entire change back.
func (s *Service) ConfigureWalletPassword(ctx context.Context, uid int64, in WalletPasswordInput) error {
	if uid <= 0 || !validWalletPassword(in.NewPaymentPassword) || in.NewPaymentPassword != in.ConfirmPassword || len(in.CurrentPassword) > 72 || len(in.OldPaymentPassword) > 72 {
		return ErrInvalid
	}
	var authErr error
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var accountHash string
		err := tx.QueryRow(ctx, `SELECT coalesce(password_hash,'') FROM v3_identity.users WHERE id=$1 AND status='active' AND deleted_at IS NULL FOR UPDATE`, uid).Scan(&accountHash)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO v3_commerce.wallet_transfer_security(user_id,created_at,updated_at) VALUES($1,$2,$2) ON CONFLICT(user_id) DO NOTHING`, uid, s.cfg.Now()); err != nil {
			return err
		}
		credential, err := walletCredentialTx(ctx, tx, uid)
		if err != nil {
			return err
		}
		if err = s.verifyWalletPasswordChangeTx(ctx, tx, uid, credential, accountHash, in); err != nil {
			if isWalletAuthError(err) {
				authErr = err
				return nil
			}
			return err
		}
		encoded, err := bcrypt.GenerateFromPassword([]byte(in.NewPaymentPassword), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE v3_commerce.wallet_transfer_security SET password_hash=$2,failed_attempts=0,locked_until=NULL,updated_at=$3 WHERE user_id=$1`, uid, string(encoded), s.cfg.Now())
		return err
	})
	if err != nil {
		return err
	}
	return authErr
}

func walletCredentialTx(ctx context.Context, tx pgx.Tx, uid int64) (walletCredential, error) {
	var c walletCredential
	err := tx.QueryRow(ctx, `SELECT coalesce(password_hash,''),failed_attempts,locked_until FROM v3_commerce.wallet_transfer_security WHERE user_id=$1 FOR UPDATE`, uid).Scan(&c.hash, &c.failures, &c.locked)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrWalletPasswordNotSet
	}
	return c, err
}

func isWalletAuthError(err error) bool {
	return errors.Is(err, ErrWalletPasswordNotSet) || errors.Is(err, ErrWalletPasswordWrong) || errors.Is(err, ErrWalletPasswordLocked) || errors.Is(err, ErrWalletAccountPassword) || errors.Is(err, ErrWalletEmailCodeInvalid) || errors.Is(err, ErrWalletEmailCodeLocked)
}

func (s *Service) verifyWalletPasswordChangeTx(ctx context.Context, tx pgx.Tx, uid int64, c walletCredential, accountHash string, in WalletPasswordInput) error {
	if c.hash == "" {
		return s.verifyWalletCredentialTx(ctx, tx, uid, c, accountHash, in.CurrentPassword, ErrWalletAccountPassword)
	}
	switch strings.TrimSpace(in.VerificationMethod) {
	case "payment_password":
		return s.verifyWalletCredentialTx(ctx, tx, uid, c, c.hash, in.OldPaymentPassword, ErrWalletPasswordWrong)
	case "email":
		if s.cfg.WalletRecovery == nil {
			return ErrWalletEmailUnavailable
		}
		return s.cfg.WalletRecovery.VerifyTx(ctx, tx, uid, in.EmailCode)
	default:
		return ErrInvalid
	}
}

func (s *Service) verifyWalletCredentialTx(ctx context.Context, tx pgx.Tx, uid int64, c walletCredential, hash, password string, wrong error) error {
	now := s.cfg.Now()
	if c.locked != nil && c.locked.After(now) {
		return ErrWalletPasswordLocked
	}
	if hash == "" || len(password) <= 72 && bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil {
		if c.failures == 0 && c.locked == nil {
			return nil
		}
		_, err := tx.Exec(ctx, `UPDATE v3_commerce.wallet_transfer_security SET failed_attempts=0,locked_until=NULL,updated_at=$2 WHERE user_id=$1`, uid, now)
		return err
	}
	failed := c.failures + 1
	var locked *time.Time
	if failed >= walletMaxFailures {
		value := now.Add(30 * time.Minute)
		locked = &value
		failed = 0
	}
	_, err := tx.Exec(ctx, `UPDATE v3_commerce.wallet_transfer_security SET failed_attempts=$2,locked_until=$3,updated_at=$4 WHERE user_id=$1`, uid, failed, locked, now)
	if err != nil {
		return err
	}
	if locked != nil {
		return ErrWalletPasswordLocked
	}
	return wrong
}
