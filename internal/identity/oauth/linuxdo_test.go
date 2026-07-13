package oauth

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLinuxDOExchangeTokenUsesForwardedHTTPSCallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var received url.Values
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read token request: %v", err)
		}
		received, err = url.ParseQuery(string(body))
		if err != nil {
			t.Fatalf("parse token request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"token"}`))
	}))
	defer tokenServer.Close()

	const endpointEnv = "LINUX_DO_TOKEN_ENDPOINT"
	previousEndpoint, hadPreviousEndpoint := os.LookupEnv(endpointEnv)
	if err := os.Setenv(endpointEnv, tokenServer.URL); err != nil {
		t.Fatalf("set token endpoint: %v", err)
	}
	t.Cleanup(func() {
		if hadPreviousEndpoint {
			_ = os.Setenv(endpointEnv, previousEndpoint)
			return
		}
		_ = os.Unsetenv(endpointEnv)
	})

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodGet, "http://shu26.cfd/api/oauth/linuxdo", nil)
	request.Host = "shu26.cfd"
	request.Header.Set("X-Forwarded-Proto", "https")
	context.Request = request

	if _, err := (&LinuxDOProvider{}).ExchangeToken(context.Request.Context(), "oauth-code", context); err != nil {
		t.Fatalf("exchange token: %v", err)
	}
	if got, want := received.Get("redirect_uri"), "https://shu26.cfd/api/oauth/linuxdo"; got != want {
		t.Fatalf("redirect_uri = %q, want %q", got, want)
	}
}

func TestLinuxDORedirectURIFallsBackToRequestTLS(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "https://shu26.cfd/api/oauth/linuxdo", nil)
	request.Host = "shu26.cfd"
	request.TLS = &tls.ConnectionState{}

	if got, want := linuxDORedirectURI(request), "https://shu26.cfd/api/oauth/linuxdo"; got != want {
		t.Fatalf("redirect URI = %q, want %q", got, want)
	}
}
