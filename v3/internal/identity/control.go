package identity

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalidInput         = errors.New("identity: invalid input")
	ErrCredentials          = errors.New("identity: invalid credentials")
	ErrForbidden            = errors.New("identity: forbidden")
	ErrNotFound             = errors.New("identity: not found")
	ErrDuplicate            = errors.New("identity: already exists")
	ErrSecondFactorRequired = errors.New("identity: second factor required")
	ErrEmailRateLimit       = errors.New("identity: email request rate limited")
)

type ControlConfig struct {
	SessionSecret            []byte
	EncryptionKey            []byte
	PublicURL                string
	DisableRegistration      bool
	AccessTTL                time.Duration
	RefreshTTL               time.Duration
	Now                      func() time.Time
	OAuth                    map[string]OAuthProvider
	HTTPClient               *http.Client
	BudgetPoster             KeyBudgetPoster
	EmailSender              AccountEmailSender
	RequireEmailVerification bool
}

type Control struct {
	pool    *pgxpool.Pool
	cfg     ControlConfig
	cipher  cipher.AEAD
	log     *slog.Logger
	limiter *attemptLimiter
}

func NewControl(pool *pgxpool.Pool, cfg ControlConfig, log *slog.Logger) (*Control, error) {
	if len(cfg.SessionSecret) < 32 || len(cfg.EncryptionKey) != 32 {
		return nil, errors.New("identity: session secret must be at least 32 bytes and encryption key exactly 32 bytes")
	}
	if cfg.AccessTTL <= 0 {
		cfg.AccessTTL = 15 * time.Minute
	}
	if cfg.RefreshTTL <= 0 {
		cfg.RefreshTTL = 30 * 24 * time.Hour
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	if log == nil {
		log = slog.Default()
	}
	block, err := aes.NewCipher(cfg.EncryptionKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Control{pool: pool, cfg: cfg, cipher: aead, log: log, limiter: newAttemptLimiter(cfg.Now)}, nil
}

type User struct {
	ID                    int64  `json:"id"`
	Username              string `json:"username"`
	DisplayName           string `json:"display_name"`
	Email                 string `json:"email"`
	Role                  string `json:"role"`
	Status                string `json:"status"`
	Group                 string `json:"group"`
	AffiliateMicroCredits int64  `json:"affiliate_micro_credits"`
}

func (u User) IsAdmin() bool { return u.Role == "admin" || u.Role == "root" }
