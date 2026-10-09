package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/adminops"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/internal/identity"
	"github.com/sh2001sh/new-api/v3/internal/identity/oidc"
)

type config struct {
	Identity           identity.ControlConfig
	CommunitySecret    string
	InternalGatewayURL string
	SMTP               *commerce.SMTPWalletConfig
	OIDC               oidc.Config
	AdminTools         adminops.Config
	TrustedProxies     []netip.Prefix
	LedgerArchiveDSN   string
	LedgerArchive      *pgxpool.Pool
}

func configFromEnv() (config, error) {
	var cfg config
	var err error
	cfg.Identity.PublicURL = os.Getenv("V3_PUBLIC_URL")
	if cfg.Identity.PublicURL == "" {
		return cfg, errors.New("V3_PUBLIC_URL must be set to the public HTTP(S) origin")
	}
	cfg.Identity.SessionSecret, err = base64.StdEncoding.DecodeString(os.Getenv("V3_SESSION_SECRET"))
	if err != nil || len(cfg.Identity.SessionSecret) < 32 {
		return cfg, errors.New("V3_SESSION_SECRET must be base64 containing at least 32 bytes")
	}
	cfg.Identity.EncryptionKey, err = base64.StdEncoding.DecodeString(os.Getenv("V3_SECRET_KEY"))
	if err != nil || len(cfg.Identity.EncryptionKey) != 32 {
		return cfg, errors.New("V3_SECRET_KEY must be base64 containing exactly 32 bytes")
	}
	if raw := os.Getenv("V3_DISABLE_REGISTRATION"); raw != "" {
		cfg.Identity.DisableRegistration, err = strconv.ParseBool(raw)
		if err != nil {
			return cfg, errors.New("V3_DISABLE_REGISTRATION must be a boolean")
		}
	}
	if raw := os.Getenv("V3_OAUTH_PROVIDERS"); raw != "" {
		if json.Unmarshal([]byte(raw), &cfg.Identity.OAuth) != nil {
			return cfg, errors.New("V3_OAUTH_PROVIDERS must be a JSON object of OAuth provider settings")
		}
	}
	cfg.CommunitySecret = os.Getenv("CODEGO_COMMUNITY_API_SECRET")
	cfg.InternalGatewayURL = os.Getenv("V3_INTERNAL_GATEWAY_URL")
	cfg.LedgerArchiveDSN = strings.TrimSpace(os.Getenv("V3_LEDGER_ARCHIVE_PG_DSN"))
	cfg.AdminTools.LogDir = os.Getenv("V3_LOG_DIR")
	cfg.AdminTools.DiskCacheDir = os.Getenv("V3_TOOL_DISK_CACHE_DIR")
	if raw := strings.TrimSpace(os.Getenv("V3_TRUSTED_PROXY_CIDRS")); raw != "" {
		for _, value := range strings.Split(raw, ",") {
			prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
			if err != nil {
				return cfg, errors.New("V3_TRUSTED_PROXY_CIDRS must contain valid CIDRs")
			}
			cfg.TrustedProxies = append(cfg.TrustedProxies, prefix.Masked())
		}
	}
	smtp := commerce.SMTPWalletConfig{Address: os.Getenv("V3_SMTP_ADDR"), Username: os.Getenv("V3_SMTP_USERNAME"), Password: os.Getenv("V3_SMTP_PASSWORD"), From: os.Getenv("V3_SMTP_FROM")}
	tls := os.Getenv("V3_SMTP_IMPLICIT_TLS")
	if smtp.Address != "" || smtp.Username != "" || smtp.Password != "" || smtp.From != "" || tls != "" {
		if tls != "" {
			smtp.ImplicitTLS, err = strconv.ParseBool(tls)
			if err != nil {
				return cfg, errors.New("V3_SMTP_IMPLICIT_TLS must be a boolean")
			}
		}
		if _, err = commerce.NewSMTPWalletSender(smtp); err != nil {
			return cfg, errors.New("SMTP wallet recovery requires valid V3_SMTP_ADDR, V3_SMTP_FROM and paired username/password")
		}
		cfg.SMTP = &smtp
	}
	cfg.OIDC, err = oidc.LoadConfig()
	if err != nil && !errors.Is(err, oidc.ErrDisabled) {
		return cfg, err
	}
	return cfg, nil
}
