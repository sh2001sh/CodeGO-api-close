package http

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
)

func OIDCDiscovery(c *gin.Context) {
	config, err := identityapp.LoadOIDCProviderConfig()
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"issuer":                                config.Issuer,
		"authorization_endpoint":                config.Issuer + "/api/oidc/authorize",
		"token_endpoint":                        config.Issuer + "/api/oidc/token",
		"userinfo_endpoint":                     config.Issuer + "/api/oidc/userinfo",
		"jwks_uri":                              config.Issuer + "/api/oidc/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "profile", "email"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
		"code_challenge_methods_supported":      []string{"S256"},
		"claims_supported": []string{
			"iss", "sub", "aud", "iat", "exp", "nonce",
			"preferred_username", "name", "email", "email_verified",
		},
	})
}

func OIDCJWKS(c *gin.Context) {
	config, err := identityapp.LoadOIDCProviderConfig()
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "public, max-age=3600")
	c.JSON(http.StatusOK, gin.H{"keys": []any{identityapp.OIDCPublicJWK(config)}})
}

func OIDCAuthorize(c *gin.Context) {
	config, err := identityapp.LoadOIDCProviderConfig()
	if err != nil {
		c.String(http.StatusServiceUnavailable, "OIDC provider is not configured")
		return
	}
	request := identityapp.OIDCAuthorizationRequest{
		ClientID: c.Query("client_id"), RedirectURI: c.Query("redirect_uri"),
		ResponseType: c.Query("response_type"), Scope: c.Query("scope"), State: c.Query("state"),
		Nonce: c.Query("nonce"), CodeChallenge: c.Query("code_challenge"), ChallengeType: c.Query("code_challenge_method"),
	}
	if err := identityapp.ValidateOIDCAuthorizationRequest(config, request); err != nil {
		writeOIDCAuthorizationError(c, config, request, err)
		return
	}
	session := sessions.Default(c)
	userID, ok := session.Get("id").(int)
	if !ok || userID <= 0 {
		loginURL := "/sign-in?redirect=" + url.QueryEscape(c.Request.URL.RequestURI())
		c.Redirect(http.StatusFound, loginURL)
		return
	}
	user, err := identityapp.LoadUserByID(userID, false)
	if err != nil || user.Status != constant.UserStatusEnabled || user.ExternalId == "" {
		session.Clear()
		_ = session.Save()
		c.Redirect(http.StatusFound, "/sign-in?redirect="+url.QueryEscape(c.Request.URL.RequestURI()))
		return
	}
	code, err := identityapp.IssueOIDCAuthorizationCode(config, request, userID, time.Now().UTC())
	if err != nil {
		writeOIDCAuthorizationError(c, config, request, err)
		return
	}
	callback, _ := url.Parse(config.RedirectURI)
	query := callback.Query()
	query.Set("code", code)
	query.Set("state", request.State)
	callback.RawQuery = query.Encode()
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusFound, callback.String())
}

func OIDCToken(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	config, err := identityapp.LoadOIDCProviderConfig()
	if err != nil {
		writeOIDCTokenError(c, http.StatusServiceUnavailable, "temporarily_unavailable", "OIDC provider is not configured")
		return
	}
	clientID, clientSecret, hasBasic := c.Request.BasicAuth()
	if !hasBasic {
		clientID, clientSecret = c.PostForm("client_id"), c.PostForm("client_secret")
	}
	if c.PostForm("grant_type") != "authorization_code" {
		writeOIDCTokenError(c, http.StatusBadRequest, "unsupported_grant_type", "only authorization_code is supported")
		return
	}
	result, err := identityapp.ExchangeOIDCAuthorizationCode(
		config, c.PostForm("code"), clientID, clientSecret,
		c.PostForm("redirect_uri"), c.PostForm("code_verifier"), time.Now().UTC(),
	)
	if err != nil {
		if errors.Is(err, identityapp.ErrOIDCInvalidClient) {
			c.Header("WWW-Authenticate", `Basic realm="codego-oidc"`)
			writeOIDCTokenError(c, http.StatusUnauthorized, "invalid_client", "client authentication failed")
			return
		}
		writeOIDCTokenError(c, http.StatusBadRequest, "invalid_grant", "authorization code is invalid, expired, or already used")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"access_token": result.AccessToken, "token_type": "Bearer", "expires_in": result.ExpiresIn,
		"id_token": result.IDToken, "scope": result.Scope,
	})
}

func OIDCUserInfo(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	token := strings.TrimSpace(c.GetHeader("Authorization"))
	if len(token) < 8 || !strings.EqualFold(token[:7], "Bearer ") {
		writeOIDCUserInfoError(c)
		return
	}
	user, scope, err := identityapp.LoadOIDCUserByAccessToken(strings.TrimSpace(token[7:]), time.Now().UTC())
	if err != nil {
		writeOIDCUserInfoError(c)
		return
	}
	claims := gin.H{"sub": user.ExternalId}
	if hasOIDCScope(scope, "profile") {
		claims["preferred_username"] = user.Username
		claims["name"] = user.DisplayName
		if claims["name"] == "" {
			claims["name"] = user.Username
		}
	}
	if hasOIDCScope(scope, "email") && user.Email != "" {
		claims["email"] = user.Email
		claims["email_verified"] = false
	}
	c.JSON(http.StatusOK, claims)
}

func writeOIDCAuthorizationError(c *gin.Context, config identityapp.OIDCProviderConfig, request identityapp.OIDCAuthorizationRequest, err error) {
	if request.ClientID != config.ClientID || request.RedirectURI != config.RedirectURI {
		c.String(http.StatusBadRequest, "invalid OIDC client or redirect_uri")
		return
	}
	errorCode := "invalid_request"
	if errors.Is(err, identityapp.ErrOIDCUnsupportedScope) {
		errorCode = "invalid_scope"
	}
	callback, _ := url.Parse(config.RedirectURI)
	query := callback.Query()
	query.Set("error", errorCode)
	if request.State != "" && len(request.State) <= 512 {
		query.Set("state", request.State)
	}
	callback.RawQuery = query.Encode()
	c.Redirect(http.StatusFound, callback.String())
}

func writeOIDCTokenError(c *gin.Context, status int, code, description string) {
	c.JSON(status, gin.H{"error": code, "error_description": description})
}

func writeOIDCUserInfoError(c *gin.Context) {
	c.Header("WWW-Authenticate", `Bearer error="invalid_token"`)
	c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
}

func hasOIDCScope(scope, expected string) bool {
	for _, value := range strings.Fields(scope) {
		if value == expected {
			return true
		}
	}
	return false
}
