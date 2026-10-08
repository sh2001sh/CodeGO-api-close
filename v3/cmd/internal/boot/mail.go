package boot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/internal/identity"
	"github.com/sh2001sh/new-api/v3/internal/settings"
)

var ErrStoredMailUnavailable = errors.New("mail: SMTP delivery is not configured")

type StoredEmailSender struct {
	pool  *pgxpool.Pool
	store *settings.Store
}

var _ commerce.WalletEmailSender = (*StoredEmailSender)(nil)
var _ identity.AccountEmailSender = (*StoredEmailSender)(nil)

// NewStoredEmailSender leaves absent mail settings unavailable without preventing
// control startup. Every send observes current settings, including encrypted tokens.
func NewStoredEmailSender(pool *pgxpool.Pool, dec catalog.Decrypter) *StoredEmailSender {
	return &StoredEmailSender{pool: pool, store: settings.New(pool, dec)}
}

func (s *StoredEmailSender) Available(ctx context.Context) (bool, error) {
	_, err := s.load(ctx)
	if errors.Is(err, ErrStoredMailUnavailable) {
		return false, nil
	}
	return err == nil, err
}

func (s *StoredEmailSender) SendAccountEmail(ctx context.Context, email, subject, body string) error {
	sender, err := s.sender(ctx)
	if err != nil {
		return err
	}
	return sender.SendAccountEmail(ctx, email, subject, body)
}

func (s *StoredEmailSender) SendWalletRecovery(ctx context.Context, email, code string) error {
	sender, err := s.sender(ctx)
	if err != nil {
		return err
	}
	return sender.SendWalletRecovery(ctx, email, code)
}

func (s *StoredEmailSender) sender(ctx context.Context) (*commerce.SMTPWalletSender, error) {
	cfg, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	sender, err := commerce.NewSMTPWalletSender(cfg)
	if err != nil {
		return nil, errors.New("mail: invalid stored SMTP configuration")
	}
	return sender, nil
}

func (s *StoredEmailSender) load(ctx context.Context) (commerce.SMTPWalletConfig, error) {
	if err := ctx.Err(); err != nil {
		return commerce.SMTPWalletConfig{}, err
	}
	if s.pool == nil {
		return commerce.SMTPWalletConfig{}, ErrStoredMailUnavailable
	}
	values := map[string]json.RawMessage{}
	for _, key := range []string{"SMTPServer", "SMTPPort", "SMTPAccount", "SMTPToken", "SMTPFrom", "SMTPSSLEnabled", "SMTPForceAuthLogin", "EmailLoginAuthServerList"} {
		value, err := s.store.Get(ctx, key)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return commerce.SMTPWalletConfig{}, ctx.Err()
			}
			return commerce.SMTPWalletConfig{}, errors.New("mail: cannot read stored SMTP configuration")
		}
		values[key] = value
	}
	cfg, err := storedSMTPConfig(values)
	if err != nil {
		return commerce.SMTPWalletConfig{}, err
	}
	if cfg.Password != "" {
		var sensitive bool
		if err = s.pool.QueryRow(ctx, `SELECT sensitive FROM v3_platform.settings WHERE key='SMTPToken'`).Scan(&sensitive); err != nil || !sensitive {
			return commerce.SMTPWalletConfig{}, errors.New("mail: SMTP token must be encrypted")
		}
	}
	return cfg, nil
}

func storedSMTPConfig(values map[string]json.RawMessage) (commerce.SMTPWalletConfig, error) {
	var cfg commerce.SMTPWalletConfig
	texts := map[string]string{}
	for _, key := range []string{"SMTPServer", "SMTPAccount", "SMTPToken", "SMTPFrom"} {
		if raw := values[key]; len(raw) != 0 {
			var text string
			if err := json.Unmarshal(raw, &text); err != nil || strings.TrimSpace(string(raw)) == "null" {
				return cfg, fmt.Errorf("mail: invalid %s setting", key)
			}
			texts[key] = text
		}
	}
	server := strings.TrimSpace(texts["SMTPServer"])
	cfg.Username = strings.TrimSpace(texts["SMTPAccount"])
	cfg.Password = texts["SMTPToken"]
	cfg.From = strings.TrimSpace(texts["SMTPFrom"])
	if server == "" && cfg.Username == "" && cfg.Password == "" && cfg.From == "" {
		return cfg, ErrStoredMailUnavailable
	}
	if !validSMTPHost(server) {
		return cfg, errors.New("mail: invalid SMTPServer setting")
	}
	port, err := smtpPort(values["SMTPPort"])
	if err != nil {
		return cfg, err
	}
	ssl, err := smtpBoolean(values["SMTPSSLEnabled"])
	if err != nil {
		return cfg, errors.New("mail: invalid SMTPSSLEnabled setting")
	}
	force, err := smtpBoolean(values["SMTPForceAuthLogin"])
	if err != nil {
		return cfg, errors.New("mail: invalid SMTPForceAuthLogin setting")
	}
	servers, err := smtpLoginServers(values["EmailLoginAuthServerList"])
	if err != nil {
		return cfg, err
	}
	cfg.Address = net.JoinHostPort(server, strconv.Itoa(port))
	cfg.ImplicitTLS = ssl || port == 465
	if cfg.From == "" {
		cfg.From = cfg.Username
	}
	lower := strings.ToLower(server + " " + cfg.Username)
	cfg.LoginAuth = force || strings.Contains(lower, "outlook") || strings.Contains(lower, "onmicrosoft")
	for _, host := range servers {
		if strings.EqualFold(host, server) {
			cfg.LoginAuth = true
		}
	}
	if _, err := commerce.NewSMTPWalletSender(cfg); err != nil {
		return commerce.SMTPWalletConfig{}, errors.New("mail: invalid stored SMTP configuration")
	}
	return cfg, nil
}
