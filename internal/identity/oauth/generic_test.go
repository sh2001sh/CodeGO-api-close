package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
)

func TestGoogleOAuthCodeExchangeAndIdentity(t *testing.T) {
	const sub = "109876543210987654321"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			if r.Method != http.MethodPost {
				t.Error("token exchange must use POST")
			}
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			for key, want := range map[string]string{
				"grant_type": "authorization_code", "code": "one-use-code",
				"client_id": "test.apps.googleusercontent.com", "client_secret": "test-only-secret",
				"redirect_uri": "https://site.example/oauth/google",
			} {
				if r.PostForm.Get(key) != want {
					t.Errorf("incorrect token parameter %s", key)
				}
			}
			fmt.Fprint(w, `{"access_token":"test-access-token","token_type":"Bearer","expires_in":3600}`)
		case "/userinfo":
			if r.Header.Get("Authorization") != "Bearer test-access-token" {
				t.Error("missing bearer authentication")
			}
			fmt.Fprintf(w, `{"sub":%q,"name":"Google User","email":"person@example.com","email_verified":true}`, sub)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	previous := platformconfig.ServerAddress
	platformconfig.ServerAddress = "https://site.example/"
	t.Cleanup(func() { platformconfig.ServerAddress = previous })
	provider := NewGenericOAuthProvider(&identitydomain.CustomOAuthProvider{
		Name: "Google", Slug: "google", ClientId: "test.apps.googleusercontent.com", ClientSecret: "test-only-secret",
		TokenEndpoint: server.URL + "/token", UserInfoEndpoint: server.URL + "/userinfo", AuthStyle: AuthStyleInParams,
		UserIdField: "sub", DisplayNameField: "name", EmailField: "email",
		AccessPolicy: `{"conditions":[{"field":"email_verified","op":"eq","value":true}]}`,
	})
	token, err := provider.ExchangeToken(context.Background(), "one-use-code", nil)
	if err != nil {
		t.Fatal(err)
	}
	user, err := provider.GetUserInfo(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if user.ProviderUserID != sub || user.Email != "person@example.com" || user.DisplayName != "Google User" {
		t.Fatalf("unexpected identity: %#v", user)
	}
}

func TestGoogleOAuthRejectsUntrustedResponses(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		token      bool
	}{
		{"unverified email", `{"sub":"123","email_verified":false}`, 200, false},
		{"missing verification", `{"sub":"123"}`, 200, false},
		{"missing subject", `{"email_verified":true}`, 200, false},
		{"null subject", `{"sub":null,"email_verified":true}`, 200, false},
		{"object subject", `{"sub":{},"email_verified":true}`, 200, false},
		{"blank subject", `{"sub":" ","email_verified":true}`, 200, false},
		{"unauthorized userinfo", `{"sub":"123","email_verified":true}`, 401, false},
		{"token failure with token", `{"access_token":"should-not-be-accepted"}`, 400, true},
		{"oauth token error", `{"error":"invalid_grant"}`, 200, true},
		{"missing token", `{}`, 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			provider := NewGenericOAuthProvider(&identitydomain.CustomOAuthProvider{
				Name: "Google", Slug: "google", TokenEndpoint: server.URL, UserInfoEndpoint: server.URL, UserIdField: "sub",
				AccessPolicy: `{"conditions":[{"field":"email_verified","op":"eq","value":true}]}`,
			})
			var err error
			if tc.token {
				_, err = provider.ExchangeToken(context.Background(), "test-code", nil)
			} else {
				_, err = provider.GetUserInfo(context.Background(), &OAuthToken{AccessToken: "test-token"})
			}
			if err == nil {
				t.Fatal("untrusted response was accepted")
			}
		})
	}
}
