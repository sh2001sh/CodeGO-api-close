package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// AccountEmailSender is injected by the composition root; delivery failures never activate codes.
type AccountEmailSender interface {
	SendAccountEmail(context.Context, string, string, string) error
}

func (c *Control) requireEmailDelivery(ctx context.Context) error {
	if c.cfg.EmailSender == nil {
		return errors.New("identity: account email delivery is not configured")
	}
	if sender, ok := c.cfg.EmailSender.(interface {
		Available(context.Context) (bool, error)
	}); ok {
		available, err := sender.Available(ctx)
		if err != nil {
			return err
		}
		if !available {
			return errors.New("identity: account email delivery is not configured")
		}
	}
	return nil
}

func normalizedEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || strings.ContainsAny(email, "\r\n") || len(email) > 254 {
		return "", ErrInvalidInput
	}
	return email, nil
}

func (c *Control) emailProofHash(email, purpose, token string) []byte {
	mac := hmac.New(sha256.New, c.cfg.SessionSecret)
	_, _ = mac.Write([]byte("account-email\x00" + email + "\x00" + purpose + "\x00" + token))
	return mac.Sum(nil)
}

func (c *Control) SendEmailVerification(ctx context.Context, email string, uid *int64) error {
	email, err := normalizedEmail(email)
	if err != nil {
		return err
	}
	if err = c.requireEmailDelivery(ctx); err != nil {
		return err
	}
	if err = c.validateEmailPolicy(ctx, email, uid == nil); err != nil {
		return err
	}
	var taken bool
	err = c.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_identity.users WHERE lower(btrim(email))=$1 AND deleted_at IS NULL AND ($2::bigint IS NULL OR id<>$2))`, email, uid).Scan(&taken)
	if err != nil {
		return err
	}
	if taken {
		return ErrDuplicate
	}
	value, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return err
	}
	code := fmt.Sprintf("%06d", value.Int64())
	purpose := "registration"
	if uid != nil {
		purpose = "binding"
	}
	return c.sendEmailProof(ctx, email, purpose, uid, code, "CodeGo 邮箱验证", "邮箱验证码: "+code+"\n有效期为 10 分钟，请勿向他人透露。")
}

func (c *Control) SendPasswordReset(ctx context.Context, email string) error {
	email, err := normalizedEmail(email)
	if err != nil {
		return err
	}
	if err = c.requireEmailDelivery(ctx); err != nil {
		return err
	}
	var uid int64
	err = c.pool.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE lower(btrim(email))=$1 AND status='active' AND deleted_at IS NULL`, email).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	parsed, err := url.Parse(c.cfg.PublicURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return errors.New("identity: password reset requires a public URL")
	}
	token, err := randomToken()
	if err != nil {
		return err
	}
	link := strings.TrimRight(c.cfg.PublicURL, "/") + "/user/reset?" + url.Values{"email": {email}, "token": {token}}.Encode()
	err = c.sendEmailProof(ctx, email, "reset", &uid, token, "CodeGo 密码重置", "请打开以下链接重置登录密码:\n"+link+"\n有效期为 10 分钟，请勿向他人透露。")
	// An already-sent active link remains usable. Match unknown-account responses during cooldown.
	if errors.Is(err, ErrEmailRateLimit) {
		return nil
	}
	return err
}

func (c *Control) sendEmailProof(ctx context.Context, email, purpose string, uid *int64, token, subject, body string) error {
	hash := c.emailProofHash(email, purpose, token)
	tag, err := c.pool.Exec(ctx, `INSERT INTO v3_identity.email_challenges(email,purpose,user_id,token_hash,expires_at,created_at)
	 VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(email,purpose) DO UPDATE SET user_id=excluded.user_id,token_hash=excluded.token_hash,
	 expires_at=excluded.expires_at,attempts=0,state='pending',created_at=excluded.created_at
	 WHERE v3_identity.email_challenges.created_at<=$6-interval '60 seconds'`, email, purpose, uid, hash, c.cfg.Now().Add(10*time.Minute), c.cfg.Now())
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrEmailRateLimit
	}
	sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err = c.cfg.EmailSender.SendAccountEmail(sendCtx, email, subject, body); err != nil {
		_, cleanupErr := c.pool.Exec(ctx, `DELETE FROM v3_identity.email_challenges WHERE email=$1 AND purpose=$2 AND token_hash=$3`, email, purpose, hash)
		return errors.Join(err, cleanupErr)
	}
	tag, err = c.pool.Exec(ctx, `UPDATE v3_identity.email_challenges SET state='active' WHERE email=$1 AND purpose=$2 AND token_hash=$3 AND state='pending' AND expires_at>$4`, email, purpose, hash, c.cfg.Now())
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("identity: email challenge activation failed")
	}
	return nil
}

// Invalid attempts are committed by callers so six-digit email codes cannot be brute forced.
func (c *Control) consumeEmailProofTx(ctx context.Context, tx pgx.Tx, email, purpose, token string, uid *int64) error {
	if len(token) == 0 || len(token) > 128 {
		return ErrCredentials
	}
	var hash []byte
	var expires time.Time
	var attempts int
	var state string
	var owner *int64
	err := tx.QueryRow(ctx, `SELECT token_hash,expires_at,attempts,state,user_id FROM v3_identity.email_challenges WHERE email=$1 AND purpose=$2 FOR UPDATE`, email, purpose).
		Scan(&hash, &expires, &attempts, &state, &owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCredentials
	}
	if err != nil {
		return err
	}
	if state != "active" || !expires.After(c.cfg.Now()) || attempts >= 5 || (uid == nil) != (owner == nil) || (uid != nil && *uid != *owner) {
		return ErrCredentials
	}
	if !hmac.Equal(hash, c.emailProofHash(email, purpose, token)) {
		if _, err = tx.Exec(ctx, `UPDATE v3_identity.email_challenges SET attempts=attempts+1 WHERE email=$1 AND purpose=$2`, email, purpose); err != nil {
			return err
		}
		return ErrCredentials
	}
	_, err = tx.Exec(ctx, `DELETE FROM v3_identity.email_challenges WHERE email=$1 AND purpose=$2`, email, purpose)
	return err
}

func commitInvalidEmailProof(ctx context.Context, tx pgx.Tx, err error) error {
	if errors.Is(err, ErrCredentials) {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return commitErr
		}
	}
	return err
}
