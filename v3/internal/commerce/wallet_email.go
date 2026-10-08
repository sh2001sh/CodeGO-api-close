package commerce

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrWalletEmailRequired    = errors.New("请先绑定邮箱")
	ErrWalletEmailUnverified  = errors.New("请先验证绑定邮箱")
	ErrWalletEmailCodeInvalid = errors.New("邮箱验证码错误或已过期")
	ErrWalletEmailCodeLocked  = errors.New("邮箱验证码已临时锁定")
	ErrWalletEmailRateLimited = errors.New("验证码发送过于频繁")
	ErrWalletEmailDelivery    = errors.New("验证码邮件发送失败")
)

// WalletRecovery.VerifyTx consumes a verified code in the password-changing
// transaction. Invalid/locked code errors may update attempts; callers must
// commit those rejected attempts without changing the payment credential.
type WalletRecovery interface {
	Send(context.Context, int64) (string, error)
	VerifyTx(context.Context, pgx.Tx, int64, string) error
}

type WalletEmailSender interface {
	SendWalletRecovery(context.Context, string, string) error
}

type WalletRecoveryConfig struct {
	// Key is a server secret (at least 32 bytes), not stored in PG. HMAC protects
	// low-entropy six-digit codes from offline guessing after a DB-only leak.
	Key      []byte
	Now      func() time.Time
	TTL      time.Duration
	Cooldown time.Duration
}

type EmailWalletRecovery struct {
	pool   *pgxpool.Pool
	sender WalletEmailSender
	cfg    WalletRecoveryConfig
}

func NewWalletRecovery(pool *pgxpool.Pool, sender WalletEmailSender, cfg WalletRecoveryConfig) (*EmailWalletRecovery, error) {
	if pool == nil || sender == nil || len(cfg.Key) < 32 {
		return nil, ErrInvalid
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.TTL == 0 {
		cfg.TTL = 10 * time.Minute
	}
	if cfg.Cooldown == 0 {
		cfg.Cooldown = time.Minute
	}
	if cfg.TTL < time.Second || cfg.TTL > time.Hour || cfg.Cooldown < time.Second || cfg.Cooldown > cfg.TTL {
		return nil, ErrInvalid
	}
	cfg.Key = append([]byte(nil), cfg.Key...)
	return &EmailWalletRecovery{pool: pool, sender: sender, cfg: cfg}, nil
}

const walletEmailPurpose = "wallet_transfer_password"

func (s *EmailWalletRecovery) Available(ctx context.Context) (bool, error) {
	if sender, ok := s.sender.(interface {
		Available(context.Context) (bool, error)
	}); ok {
		return sender.Available(ctx)
	}
	return true, nil
}

func (s *EmailWalletRecovery) codeHash(uid int64, email, code string, salt []byte) []byte {
	h := hmac.New(sha256.New, s.cfg.Key)
	_, _ = h.Write([]byte(walletEmailPurpose))
	var id [8]byte
	binary.BigEndian.PutUint64(id[:], uint64(uid))
	_, _ = h.Write(id[:])
	_, _ = h.Write(salt)
	_, _ = h.Write([]byte(email))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(code))
	return h.Sum(nil)
}

func walletBoundEmail(ctx context.Context, tx pgx.Tx, uid int64) (string, error) {
	var email string
	var verified bool
	err := tx.QueryRow(ctx, `SELECT coalesce(email,''),email_verified FROM v3_identity.users WHERE id=$1 AND status='active' AND deleted_at IS NULL FOR UPDATE`, uid).Scan(&email, &verified)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(email) == "" {
		return "", ErrWalletEmailRequired
	}
	if !verified {
		return "", ErrWalletEmailUnverified
	}
	if _, err = mail.ParseAddress(email); err != nil || strings.ContainsAny(email, "\r\n") {
		return "", ErrWalletEmailRequired
	}
	return email, nil
}

// reserveWalletRecoveryCodeTx checks the rate limit/lock state and upserts
// the new code's hash, returning the user's verified bound email it was
// addressed to.
func (s *EmailWalletRecovery) reserveWalletRecoveryCodeTx(ctx context.Context, uid int64, code string, salt []byte) (string, error) {
	var email string
	now := s.cfg.Now()
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		email, err = walletBoundEmail(ctx, tx, uid)
		if err != nil {
			return err
		}
		var sendAfter time.Time
		var locked *time.Time
		err = tx.QueryRow(ctx, `SELECT send_after,locked_until FROM v3_commerce.wallet_recovery_codes WHERE user_id=$1 FOR UPDATE`, uid).Scan(&sendAfter, &locked)
		if err == nil {
			if locked != nil && locked.After(now) {
				return ErrWalletEmailCodeLocked
			}
			if sendAfter.After(now) {
				return ErrWalletEmailRateLimited
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO v3_commerce.wallet_recovery_codes(user_id,purpose,recipient_email,code_hash,salt,state,created_at,expires_at,send_after) VALUES($1,$2,$3,$4,$5,'pending',$6,$7,$8)
		 ON CONFLICT(user_id) DO UPDATE SET purpose=EXCLUDED.purpose,recipient_email=EXCLUDED.recipient_email,code_hash=EXCLUDED.code_hash,salt=EXCLUDED.salt,state='pending',created_at=EXCLUDED.created_at,expires_at=EXCLUDED.expires_at,send_after=EXCLUDED.send_after,failed_attempts=0,locked_until=NULL,consumed_at=NULL`, uid, walletEmailPurpose, email, s.codeHash(uid, email, code, salt), salt, now, now.Add(s.cfg.TTL), now.Add(s.cfg.Cooldown))
		return err
	})
	return email, err
}

// Send reserves the cooldown before delivery. A failed delivery leaves an
// unusable code and the cooldown; a successful delivery activates only its own
// generation and only while the user's verified bound address is unchanged.
func (s *EmailWalletRecovery) Send(ctx context.Context, uid int64) (string, error) {
	if uid <= 0 {
		return "", ErrInvalid
	}
	available, err := s.Available(ctx)
	if err != nil {
		return "", err
	}
	if !available {
		return "", ErrWalletEmailUnavailable
	}
	random, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	code := fmt.Sprintf("%06d", random.Int64())
	salt := make([]byte, 32)
	if _, err = rand.Read(salt); err != nil {
		return "", err
	}
	email, err := s.reserveWalletRecoveryCodeTx(ctx, uid, code, salt)
	if err != nil {
		return "", err
	}
	address, err := mail.ParseAddress(email)
	if err != nil {
		return "", err
	}
	if err = s.sender.SendWalletRecovery(ctx, address.Address, code); err != nil {
		// The request may be canceled; finish the durable failure transition with
		// a bounded cleanup context so a pending code cannot become usable later.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_, updateErr := s.pool.Exec(cleanup, `UPDATE v3_commerce.wallet_recovery_codes SET state='failed' WHERE user_id=$1 AND salt=$2 AND state='pending'`, uid, salt)
		return "", errors.Join(ErrWalletEmailDelivery, err, updateErr)
	}
	tag, err := s.pool.Exec(ctx, `UPDATE v3_commerce.wallet_recovery_codes c SET state='active' FROM v3_identity.users u WHERE c.user_id=$1 AND c.salt=$2 AND c.state='pending' AND c.expires_at>$3 AND u.id=c.user_id AND u.email=c.recipient_email AND u.email_verified AND u.status='active' AND u.deleted_at IS NULL`, uid, salt, s.cfg.Now())
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() != 1 {
		return "", ErrWalletEmailCodeInvalid
	}
	return maskWalletEmail(address.Address), nil
}
