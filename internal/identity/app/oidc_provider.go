package app

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sh2001sh/new-api/constant"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

const (
	OIDCAuthorizationCodeTTL = 5 * time.Minute
	OIDCAccessTokenTTL       = 15 * time.Minute
)

var (
	ErrOIDCDisabled         = errors.New("oidc provider is not configured")
	ErrOIDCInvalidClient    = errors.New("invalid_client")
	ErrOIDCInvalidGrant     = errors.New("invalid_grant")
	ErrOIDCInvalidToken     = errors.New("invalid_token")
	ErrOIDCInvalidRequest   = errors.New("invalid_request")
	ErrOIDCUnsupportedScope = errors.New("invalid_scope")
)

type OIDCProviderConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURI  string
	KeyID        string
	PrivateKey   *rsa.PrivateKey
}

type OIDCAuthorizationRequest struct {
	ClientID      string
	RedirectURI   string
	ResponseType  string
	Scope         string
	State         string
	Nonce         string
	CodeChallenge string
	ChallengeType string
}

type OIDCTokenResult struct {
	AccessToken string
	IDToken     string
	Scope       string
	ExpiresIn   int64
}

func LoadOIDCProviderConfig() (OIDCProviderConfig, error) {
	config := OIDCProviderConfig{
		Issuer:       strings.TrimRight(strings.TrimSpace(os.Getenv("OIDC_ISSUER")), "/"),
		ClientID:     strings.TrimSpace(os.Getenv("OIDC_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv("OIDC_CLIENT_SECRET")),
		RedirectURI:  strings.TrimSpace(os.Getenv("OIDC_REDIRECT_URI")),
		KeyID:        strings.TrimSpace(os.Getenv("OIDC_SIGNING_KEY_ID")),
	}
	if config.Issuer == "" || config.ClientID == "" || config.ClientSecret == "" || config.RedirectURI == "" {
		return OIDCProviderConfig{}, ErrOIDCDisabled
	}
	if len(config.ClientSecret) < 32 {
		return OIDCProviderConfig{}, fmt.Errorf("OIDC_CLIENT_SECRET must contain at least 32 characters")
	}
	if err := validateOIDCURL(config.Issuer); err != nil {
		return OIDCProviderConfig{}, fmt.Errorf("invalid OIDC_ISSUER: %w", err)
	}
	if err := validateOIDCURL(config.RedirectURI); err != nil {
		return OIDCProviderConfig{}, fmt.Errorf("invalid OIDC_REDIRECT_URI: %w", err)
	}
	if config.KeyID == "" {
		config.KeyID = "codego-oidc-1"
	}
	keyPEM, err := loadOIDCSigningKeyPEM()
	if err != nil {
		return OIDCProviderConfig{}, err
	}
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return OIDCProviderConfig{}, fmt.Errorf("OIDC signing key is not PEM encoded")
	}
	if key, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes); parseErr == nil {
		var ok bool
		config.PrivateKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			return OIDCProviderConfig{}, fmt.Errorf("OIDC signing key must be RSA")
		}
	} else {
		config.PrivateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return OIDCProviderConfig{}, fmt.Errorf("parse OIDC signing key: %w", err)
		}
	}
	if config.PrivateKey.N.BitLen() < 2048 {
		return OIDCProviderConfig{}, fmt.Errorf("OIDC signing key must be at least 2048 bits")
	}
	return config, nil
}

func loadOIDCSigningKeyPEM() ([]byte, error) {
	if encoded := strings.TrimSpace(os.Getenv("OIDC_SIGNING_PRIVATE_KEY_BASE64")); encoded != "" {
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("decode OIDC_SIGNING_PRIVATE_KEY_BASE64: %w", err)
		}
		return decoded, nil
	}
	path := strings.TrimSpace(os.Getenv("OIDC_SIGNING_PRIVATE_KEY_FILE"))
	if path == "" {
		return nil, ErrOIDCDisabled
	}
	keyPEM, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read OIDC signing key: %w", err)
	}
	return keyPEM, nil
}

func ValidateOIDCAuthorizationRequest(config OIDCProviderConfig, request OIDCAuthorizationRequest) error {
	if request.ClientID != config.ClientID || request.RedirectURI != config.RedirectURI {
		return ErrOIDCInvalidClient
	}
	if request.ResponseType != "code" || request.State == "" || len(request.State) > 512 ||
		len(request.Nonce) > 256 || request.ChallengeType != "S256" || !validPKCEValue(request.CodeChallenge, 43, 43) {
		return ErrOIDCInvalidRequest
	}
	if len(request.Scope) > 256 {
		return ErrOIDCUnsupportedScope
	}
	scopes := strings.Fields(request.Scope)
	if len(scopes) == 0 || !containsString(scopes, "openid") {
		return ErrOIDCUnsupportedScope
	}
	for _, scope := range scopes {
		if scope != "openid" && scope != "profile" && scope != "email" {
			return ErrOIDCUnsupportedScope
		}
	}
	return nil
}

func IssueOIDCAuthorizationCode(config OIDCProviderConfig, request OIDCAuthorizationRequest, userID int, now time.Time) (string, error) {
	if err := ValidateOIDCAuthorizationRequest(config, request); err != nil {
		return "", err
	}
	code, err := randomURLToken(32)
	if err != nil {
		return "", err
	}
	record := identityschema.OIDCAuthorizationCode{
		CodeHash: hashOIDCSecret(code), ClientID: request.ClientID, UserID: userID,
		RedirectURI: request.RedirectURI, Scope: strings.Join(strings.Fields(request.Scope), " "),
		Nonce: request.Nonce, CodeChallenge: request.CodeChallenge, ExpiresAt: now.Add(OIDCAuthorizationCodeTTL),
	}
	if err := platformdb.DB.Where("expires_at < ?", now).Delete(&identityschema.OIDCAuthorizationCode{}).Error; err != nil {
		return "", err
	}
	if err := platformdb.DB.Where("expires_at < ?", now).Delete(&identityschema.OIDCAccessToken{}).Error; err != nil {
		return "", err
	}
	if err := platformdb.DB.Create(&record).Error; err != nil {
		return "", err
	}
	return code, nil
}

func ExchangeOIDCAuthorizationCode(config OIDCProviderConfig, code, clientID, clientSecret, redirectURI, verifier string, now time.Time) (OIDCTokenResult, error) {
	if !validOIDCClient(config, clientID, clientSecret) {
		return OIDCTokenResult{}, ErrOIDCInvalidClient
	}
	if code == "" || redirectURI == "" || !validPKCEValue(verifier, 43, 128) {
		return OIDCTokenResult{}, ErrOIDCInvalidGrant
	}
	var result OIDCTokenResult
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		var grant identityschema.OIDCAuthorizationCode
		if err := tx.Where("code_hash = ?", hashOIDCSecret(code)).First(&grant).Error; err != nil {
			return ErrOIDCInvalidGrant
		}
		if grant.UsedAt != nil || !grant.ExpiresAt.After(now) || grant.ClientID != clientID || grant.RedirectURI != redirectURI || !verifyPKCE(verifier, grant.CodeChallenge) {
			return ErrOIDCInvalidGrant
		}
		usedAt := now
		update := tx.Model(&identityschema.OIDCAuthorizationCode{}).
			Where("code_hash = ? AND used_at IS NULL", grant.CodeHash).Update("used_at", usedAt)
		if update.Error != nil || update.RowsAffected != 1 {
			return ErrOIDCInvalidGrant
		}
		var user identityschema.User
		if err := tx.Omit("password").First(&user, "id = ? AND status = ?", grant.UserID, constant.UserStatusEnabled).Error; err != nil {
			return ErrOIDCInvalidGrant
		}
		accessToken, err := randomURLToken(32)
		if err != nil {
			return err
		}
		access := identityschema.OIDCAccessToken{
			TokenHash: hashOIDCSecret(accessToken), ClientID: clientID, UserID: user.Id,
			Scope: grant.Scope, ExpiresAt: now.Add(OIDCAccessTokenTTL),
		}
		if err := tx.Create(&access).Error; err != nil {
			return err
		}
		idToken, err := signOIDCIDToken(config, &user, grant.Nonce, now)
		if err != nil {
			return err
		}
		result = OIDCTokenResult{AccessToken: accessToken, IDToken: idToken, Scope: grant.Scope, ExpiresIn: int64(OIDCAccessTokenTTL.Seconds())}
		return nil
	})
	return result, err
}

func LoadOIDCUserByAccessToken(token string, now time.Time) (*identityschema.User, string, error) {
	if token == "" {
		return nil, "", ErrOIDCInvalidToken
	}
	var access identityschema.OIDCAccessToken
	if err := platformdb.DB.Where("token_hash = ?", hashOIDCSecret(token)).First(&access).Error; err != nil {
		return nil, "", ErrOIDCInvalidToken
	}
	if access.RevokedAt != nil || !access.ExpiresAt.After(now) {
		return nil, "", ErrOIDCInvalidToken
	}
	var user identityschema.User
	if err := platformdb.DB.Omit("password").First(&user, "id = ? AND status = ?", access.UserID, constant.UserStatusEnabled).Error; err != nil {
		return nil, "", ErrOIDCInvalidToken
	}
	return &user, access.Scope, nil
}

func OIDCPublicJWK(config OIDCProviderConfig) map[string]string {
	publicKey := config.PrivateKey.PublicKey
	return map[string]string{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": config.KeyID,
		"n": base64.RawURLEncoding.EncodeToString(publicKey.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(intBytes(publicKey.E)),
	}
}

func signOIDCIDToken(config OIDCProviderConfig, user *identityschema.User, nonce string, now time.Time) (string, error) {
	claims := jwt.MapClaims{
		"iss": config.Issuer, "sub": user.ExternalId, "aud": config.ClientID,
		"iat": now.Unix(), "exp": now.Add(OIDCAccessTokenTTL).Unix(),
		"preferred_username": user.Username, "name": firstNonEmpty(user.DisplayName, user.Username),
	}
	if nonce != "" {
		claims["nonce"] = nonce
	}
	if user.Email != "" {
		claims["email"] = user.Email
		claims["email_verified"] = false
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = config.KeyID
	return token.SignedString(config.PrivateKey)
}

func validOIDCClient(config OIDCProviderConfig, clientID, secret string) bool {
	return clientID == config.ClientID && subtle.ConstantTimeCompare([]byte(secret), []byte(config.ClientSecret)) == 1
}

func verifyPKCE(verifier, challenge string) bool {
	digest := sha256.Sum256([]byte(verifier))
	actual := base64.RawURLEncoding.EncodeToString(digest[:])
	return subtle.ConstantTimeCompare([]byte(actual), []byte(challenge)) == 1
}

func validPKCEValue(value string, minLength, maxLength int) bool {
	if len(value) < minLength || len(value) > maxLength {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '-' || character == '.' || character == '_' || character == '~' {
			continue
		}
		return false
	}
	return true
}

func hashOIDCSecret(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func randomURLToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func intBytes(value int) []byte {
	if value == 0 {
		return []byte{0}
	}
	buffer := make([]byte, 0, 4)
	for value > 0 {
		buffer = append([]byte{byte(value)}, buffer...)
		value >>= 8
	}
	return buffer
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func validateOIDCURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("must be an absolute URL without credentials or a fragment")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme == "http" && (parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1") {
		return nil
	}
	return errors.New("must use HTTPS")
}
