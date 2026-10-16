package http

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/i18n"
	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	"github.com/sh2001sh/new-api/internal/identity/oauth"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
)

func TestGoogleOAuthLoginAndExplicitBinding(t *testing.T) {
	if err := i18n.Init(); err != nil {
		t.Fatal(err)
	}
	for _, bind := range []bool{false, true} {
		t.Run(fmt.Sprintf("bind=%t", bind), func(t *testing.T) {
			db := setupDesktopHTTPTestDB(t)
			previousRegister := platformconfig.RegisterEnabled
			platformconfig.RegisterEnabled = true
			t.Cleanup(func() { platformconfig.RegisterEnabled = previousRegister })
			const subject = "109876543210987654321"
			existing := &identityschema.User{Username: "existing", Email: "same@example.com", DisplayName: "Existing", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
			if err := db.Create(existing).Error; err != nil {
				t.Fatal(err)
			}
			requests := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/token" {
					fmt.Fprint(w, `{"access_token":"test-access-token","token_type":"Bearer"}`)
					return
				}
				fmt.Fprintf(w, `{"sub":%q,"name":"Google User","email":"same@example.com","email_verified":true}`, subject)
			}))
			defer upstream.Close()
			config := &identitydomain.CustomOAuthProvider{Name: "Google", Slug: "google", Enabled: true, TokenEndpoint: upstream.URL + "/token", UserInfoEndpoint: upstream.URL + "/userinfo", UserIdField: "sub", EmailField: "email", DisplayNameField: "name", AccessPolicy: `{"conditions":[{"field":"email_verified","op":"eq","value":true}]}`}
			if err := db.Create(config).Error; err != nil {
				t.Fatal(err)
			}
			oauth.Register("google", oauth.NewGenericOAuthProvider(config))
			t.Cleanup(func() { oauth.Unregister("google") })
			engine := gin.New()
			engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("google-oauth-test-secret"))))
			engine.GET("/prepare", func(c *gin.Context) {
				session := sessions.Default(c)
				session.Set("oauth_state", "google-state")
				if bind {
					session.Set("id", existing.Id)
					session.Set("username", existing.Username)
				}
				if err := session.Save(); err != nil {
					t.Fatal(err)
				}
				c.Status(http.StatusOK)
			})
			engine.GET("/oauth/:provider", HandleOAuth)
			callback := func(state string) *httptest.ResponseRecorder {
				prepare := httptest.NewRecorder()
				engine.ServeHTTP(prepare, httptest.NewRequest(http.MethodGet, "/prepare", nil))
				req := httptest.NewRequest(http.MethodGet, "/oauth/google?code=test-code&state="+state, nil)
				req.Header.Set("Cookie", prepare.Header().Get("Set-Cookie"))
				rec := httptest.NewRecorder()
				engine.ServeHTTP(rec, req)
				return rec
			}
			if response := decodeAPIResponse(t, callback("wrong-state")); response.Success || requests != 0 {
				t.Fatal("invalid state reached provider or authenticated")
			}
			if response := decodeAPIResponse(t, callback("google-state")); !response.Success {
				t.Fatalf("Google callback failed: %#v", response)
			}
			var binding identitydomain.UserOAuthBinding
			if err := db.Where("provider_id = ? AND provider_user_id = ?", config.Id, subject).First(&binding).Error; err != nil {
				t.Fatal(err)
			}
			if bind && binding.UserId != existing.Id {
				t.Fatal("explicit binding changed local numeric user ID")
			}
			if !bind && binding.UserId == existing.Id {
				t.Fatal("email match incorrectly merged accounts")
			}
			if !bind {
				if response := decodeAPIResponse(t, callback("google-state")); !response.Success {
					t.Fatalf("repeat login failed: %#v", response)
				}
				var users int64
				if err := db.Model(&identityschema.User{}).Count(&users).Error; err != nil || users != 2 {
					t.Fatalf("repeat login created extra account: count=%d err=%v", users, err)
				}
			}
		})
	}
}
