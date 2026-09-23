package app

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"github.com/sh2001sh/new-api/constant"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestOIDCAuthorizationCodeRoundTripAndReplayRejection(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:oidc-round-trip?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	previousDB := platformdb.DB
	platformdb.DB = db
	t.Cleanup(func() { platformdb.DB = previousDB })
	require.NoError(t, db.AutoMigrate(&identityschema.User{}, &identityschema.OIDCAuthorizationCode{}, &identityschema.OIDCAccessToken{}))

	user := identityschema.User{ExternalId: "ABC234", Username: "community-user", Password: "unused", Status: constant.UserStatusEnabled, Role: constant.RoleCommonUser, Email: "member@example.com"}
	require.NoError(t, db.Create(&user).Error)
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	config := OIDCProviderConfig{
		Issuer: "https://codegoai.com", ClientID: "nodebb-community", ClientSecret: "test-client-secret",
		RedirectURI: "https://community.codegoai.com/auth/codego/callback", KeyID: "test-key", PrivateKey: privateKey,
	}
	verifier := "a-verifier-long-enough-for-the-nodebb-pkce-client"
	digest := sha256.Sum256([]byte(verifier))
	request := OIDCAuthorizationRequest{
		ClientID: config.ClientID, RedirectURI: config.RedirectURI, ResponseType: "code",
		Scope: "openid profile email", State: "csrf-state", Nonce: "nonce-value",
		CodeChallenge: base64.RawURLEncoding.EncodeToString(digest[:]), ChallengeType: "S256",
	}
	now := time.Date(2026, 9, 18, 1, 2, 3, 0, time.UTC)
	code, err := IssueOIDCAuthorizationCode(config, request, user.Id, now)
	require.NoError(t, err)
	require.NotEmpty(t, code)

	result, err := ExchangeOIDCAuthorizationCode(config, code, config.ClientID, config.ClientSecret, config.RedirectURI, verifier, now.Add(time.Second))
	require.NoError(t, err)
	require.Equal(t, int64(OIDCAccessTokenTTL.Seconds()), result.ExpiresIn)

	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(result.IDToken, claims, func(token *jwt.Token) (any, error) {
		return &privateKey.PublicKey, nil
	}, jwt.WithAudience(config.ClientID), jwt.WithIssuer(config.Issuer), jwt.WithTimeFunc(func() time.Time { return now.Add(time.Second) }))
	require.NoError(t, err)
	require.True(t, parsed.Valid)
	require.Equal(t, "ABC234", claims["sub"])
	require.Equal(t, "nonce-value", claims["nonce"])

	loaded, scope, err := LoadOIDCUserByAccessToken(result.AccessToken, now.Add(2*time.Second))
	require.NoError(t, err)
	require.Equal(t, user.Id, loaded.Id)
	require.Equal(t, "openid profile email", scope)

	_, err = ExchangeOIDCAuthorizationCode(config, code, config.ClientID, config.ClientSecret, config.RedirectURI, verifier, now.Add(3*time.Second))
	require.ErrorIs(t, err, ErrOIDCInvalidGrant)

	_, _, err = LoadOIDCUserByAccessToken(result.AccessToken, now.Add(OIDCAccessTokenTTL+time.Second))
	require.ErrorIs(t, err, ErrOIDCInvalidToken)
	require.NoError(t, db.Model(&identityschema.User{}).Where("id = ?", user.Id).Update("status", constant.UserStatusDisabled).Error)
	_, _, err = LoadOIDCUserByAccessToken(result.AccessToken, now.Add(3*time.Second))
	require.ErrorIs(t, err, ErrOIDCInvalidToken)
}

func TestOIDCRejectsRedirectMismatchAndUnsupportedScope(t *testing.T) {
	config := OIDCProviderConfig{ClientID: "nodebb", RedirectURI: "https://community.codegoai.com/auth/codego/callback"}
	request := OIDCAuthorizationRequest{
		ClientID: "nodebb", RedirectURI: "https://evil.example/callback", ResponseType: "code",
		Scope: "openid", State: "state", CodeChallenge: strings.Repeat("a", 43), ChallengeType: "S256",
	}
	require.ErrorIs(t, ValidateOIDCAuthorizationRequest(config, request), ErrOIDCInvalidClient)

	request.RedirectURI = config.RedirectURI
	request.Scope = "openid admin"
	require.ErrorIs(t, ValidateOIDCAuthorizationRequest(config, request), ErrOIDCUnsupportedScope)
}

func TestOIDCPKCEVerificationRejectsWrongVerifier(t *testing.T) {
	digest := sha256.Sum256([]byte("correct-verifier"))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	require.True(t, verifyPKCE("correct-verifier", challenge))
	require.False(t, verifyPKCE("wrong-verifier", challenge))
}
