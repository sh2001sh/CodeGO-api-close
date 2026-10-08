//go:build pgintegration

package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type accountMailCapture struct {
	mu       sync.Mutex
	messages []string
	fail     bool
}

func (s *accountMailCapture) SendAccountEmail(_ context.Context, email, subject, body string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("test SMTP unavailable")
	}
	s.messages = append(s.messages, email+"\n"+subject+"\n"+body)
	return nil
}
func (s *accountMailCapture) last() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.messages[len(s.messages)-1]
}
func (s *accountMailCapture) code() string {
	line := strings.Split(s.last(), "邮箱验证码: ")[1]
	return strings.Split(line, "\n")[0]
}

func securityControl(t *testing.T) (*Control, *time.Time, *accountMailCapture) {
	t.Helper()
	pool, _ := testDeps(t)
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	sender := &accountMailCapture{}
	c, err := NewControl(pool, ControlConfig{SessionSecret: bytes.Repeat([]byte{1}, 32), EncryptionKey: bytes.Repeat([]byte{2}, 32), PublicURL: "https://codego.test", Now: func() time.Time { return now }, EmailSender: sender}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	return c, &now, sender
}
func securityUser(t *testing.T, c *Control, name string) User {
	t.Helper()
	u, err := c.Register(ctx, RegisterInput{Username: name, Password: "strong-password"})
	if err != nil {
		t.Fatal(err)
	}
	return u
}
func securitySession(t *testing.T, c *Control, u User) Session {
	t.Helper()
	s, err := c.NewSession(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func securityRequest(t *testing.T, c *Control, method, path, body string, s Session) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "https://codego.test"+path, strings.NewReader(body))
	r.Header.Set("X-CodeGo-API-Version", "3")
	if s.AccessToken != "" {
		r.Header.Set("Authorization", "Bearer "+s.AccessToken)
	}
	w := httptest.NewRecorder()
	c.Handler().ServeHTTP(w, r)
	return w
}
func loginChallenge(t *testing.T, c *Control, u User) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": u.Username, "password": "strong-password"})
	w := securityRequest(t, c, http.MethodPost, "/api/user/login", string(body), Session{})
	if w.Code != http.StatusOK {
		t.Fatalf("login challenge status=%d body=%s", w.Code, w.Body.String())
	}
	for _, cookie := range w.Result().Cookies() {
		if (cookie.Name == "codego_session" || cookie.Name == "codego_refresh") && cookie.Value != "" {
			t.Fatal("first factor issued authenticated cookie")
		}
	}
	var out struct {
		Data struct {
			Required bool   `json:"require_2fa"`
			Token    string `json:"challenge_token"`
			Access   string `json:"access_token"`
		}
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || !out.Data.Required || len(out.Data.Token) != 43 || out.Data.Access != "" {
		t.Fatalf("invalid challenge=%s", w.Body.String())
	}
	return out.Data.Token
}
