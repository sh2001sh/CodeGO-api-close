package oidc

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const codeTTL = 5 * time.Minute
const tokenTTL = 15 * time.Minute

var errGrant = errors.New("invalid_grant")
var errToken = errors.New("invalid_token")

type Server struct {
	pool         *pgxpool.Pool
	cfg          Config
	authenticate func(*http.Request) (int64, error)
	log          *slog.Logger
}

func New(pool *pgxpool.Pool, cfg Config, authenticate func(*http.Request) (int64, error), log *slog.Logger) (*Server, error) {
	cfg, err := normalize(cfg)
	if err != nil {
		return nil, err
	}
	if pool == nil || authenticate == nil {
		return nil, errors.New("oidc: database pool and authentication resolver are required")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Server{pool: pool, cfg: cfg, authenticate: authenticate, log: log}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", s.discovery)
	mux.HandleFunc("GET /api/oidc/jwks", s.jwks)
	mux.HandleFunc("GET /api/oidc/authorize", s.authorize)
	mux.HandleFunc("POST /api/oidc/token", s.token)
	mux.HandleFunc("GET /api/oidc/userinfo", s.userinfo)
	mux.HandleFunc("POST /api/oidc/userinfo", s.userinfo)
	return mux
}

func (s *Server) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                s.cfg.Issuer,
		"authorization_endpoint":                s.cfg.Issuer + "/api/oidc/authorize",
		"token_endpoint":                        s.cfg.Issuer + "/api/oidc/token",
		"userinfo_endpoint":                     s.cfg.Issuer + "/api/oidc/userinfo",
		"jwks_uri":                              s.cfg.Issuer + "/api/oidc/jwks",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "profile", "email"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
		"code_challenge_methods_supported":      []string{"S256"},
		"claims_supported":                      []string{"iss", "sub", "aud", "iat", "exp", "nonce", "preferred_username", "name", "email", "email_verified"},
	})
}

func (s *Server) jwks(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=3600")
	key := s.cfg.PrivateKey.PublicKey
	writeJSON(w, http.StatusOK, map[string]any{"keys": []any{map[string]string{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": s.cfg.KeyID,
		"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value) // A disconnected response has no recovery path.
}

func oauthError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, status, map[string]string{"error": code})
}

func randomToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func digest(raw string) []byte {
	h := sha256.Sum256([]byte(raw))
	return h[:]
}

func validVerifier(v string) bool {
	if len(v) < 43 || len(v) > 128 {
		return false
	}
	for _, c := range v {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~", c) {
			continue
		}
		return false
	}
	return true
}

func validChallenge(v string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(v)
	return err == nil && len(raw) == 32 && base64.RawURLEncoding.EncodeToString(raw) == v
}

func pkceMatches(verifier, challenge string) bool {
	if !validVerifier(verifier) {
		return false
	}
	actual := base64.RawURLEncoding.EncodeToString(digest(verifier))
	return subtle.ConstantTimeCompare([]byte(actual), []byte(challenge)) == 1
}

func hasScope(scope, target string) bool {
	for _, field := range strings.Fields(scope) {
		if field == target {
			return true
		}
	}
	return false
}
