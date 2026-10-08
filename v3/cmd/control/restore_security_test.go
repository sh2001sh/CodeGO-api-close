//go:build pgintegration

package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/identity"
)

func restoreTOTP(t *testing.T, secret string, now time.Time) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(now.Unix()/30))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 15
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[offset:offset+4])&0x7fffffff)%1000000)
}

func TestRestoreComposedTwoFactorChallengeAndReplay(t *testing.T) {
	s := restoreStack(t, nil)
	u := s.register(t, "restored_factor")
	token := u.AccessToken
	s.call(t, "GET", "/api/user/2fa/status", "", "", 401)
	s.call(t, "GET", "/api/user/2fa/stats", token, "", 403)
	setup := restoreData[identity.TwoFactorSetup](t, s.call(t, "POST", "/api/user/2fa/setup", token, `{}`, 200))
	if len(setup.BackupCodes) != 4 {
		t.Fatalf("legacy backup code count=%d", len(setup.BackupCodes))
	}
	s.call(t, "POST", "/api/user/2fa/enable", token, fmt.Sprintf(`{"code":%q}`, restoreTOTP(t, setup.Secret, *s.now)), 200)
	s.call(t, "GET", "/api/user/self", token, "", 401)
	challenge := func() string {
		w := s.call(t, "POST", "/api/user/login", "", `{"username":"restored_factor","password":"strong-password"}`, 200)
		data := restoreData[struct {
			Required bool   `json:"require_2fa"`
			Token    string `json:"challenge_token"`
			Access   string `json:"access_token"`
		}](t, w)
		if !data.Required || data.Token == "" || data.Access != "" {
			t.Fatalf("first factor released authentication: %+v", data)
		}
		for _, cookie := range w.Result().Cookies() {
			if (cookie.Name == "codego_session" || cookie.Name == "codego_refresh") && cookie.Value != "" {
				t.Fatal("first factor issued authenticated cookie")
			}
		}
		return data.Token
	}
	ch := challenge()
	s.call(t, "POST", "/api/user/login/2fa", "", fmt.Sprintf(`{"challenge_token":%q,"code":"invalid"}`, ch), 401)
	body := fmt.Sprintf(`{"challenge_token":%q,"code":%q}`, ch, setup.BackupCodes[0])
	second := restoreData[identity.Session](t, s.call(t, "POST", "/api/user/login/2fa", "", body, 200))
	s.call(t, "POST", "/api/user/login/2fa", "", body, 401)
	status := restoreData[identity.TwoFactorStatus](t, s.call(t, "GET", "/api/user/2fa/status", second.AccessToken, "", 200))
	if !status.Enabled || status.BackupCodesRemaining != 3 {
		t.Fatalf("backup replay protection: %+v", status)
	}
	ch = challenge()
	s.call(t, "POST", "/api/user/login/2fa", "", fmt.Sprintf(`{"challenge_token":%q,"code":%q}`, ch, setup.BackupCodes[0]), 401)
	*s.now = s.now.Add(5 * time.Minute)
	s.call(t, "POST", "/api/user/login/2fa", "", fmt.Sprintf(`{"challenge_token":%q,"code":%q}`, ch, setup.BackupCodes[1]), 401)
}

type restoreMailbox struct{ body string }

func (m *restoreMailbox) SendAccountEmail(_ context.Context, _, _, body string) error {
	m.body = body
	return nil
}

func TestRestoreComposedEmailProofAndPasswordReset(t *testing.T) {
	mail := &restoreMailbox{}
	s := restoreStack(t, func(cfg *config) { cfg.Identity.EmailSender = mail })
	u := s.register(t, "restored_mail")
	s.call(t, "GET", "/api/verification?email=restore@example.test", u.AccessToken, "", 200)
	parts := strings.Split(mail.body, "邮箱验证码: ")
	if len(parts) != 2 {
		t.Fatal("verification message did not contain a proof")
	}
	code := strings.Split(parts[1], "\n")[0]
	body := fmt.Sprintf(`{"email":"restore@example.test","code":%q}`, code)
	s.call(t, "POST", "/api/user/email/verify", u.AccessToken, body, 200)
	s.call(t, "POST", "/api/oauth/email/bind", u.AccessToken, body, 401)
	s.call(t, "GET", "/api/reset_password?email=restore@example.test", "", "", 200)
	var proof string
	for _, line := range strings.Split(mail.body, "\n") {
		if strings.HasPrefix(line, "http://control.test/user/reset?") {
			link, err := url.Parse(line)
			if err != nil {
				t.Fatal(err)
			}
			proof = link.Query().Get("token")
		}
	}
	if proof == "" {
		t.Fatal("password reset did not deliver a proof")
	}
	reset := fmt.Sprintf(`{"email":"restore@example.test","token":%q,"password":"new-strong-password"}`, proof)
	s.call(t, "POST", "/api/user/reset", "", reset, 200)
	s.call(t, "POST", "/api/user/reset", "", reset, 401)
	s.call(t, "GET", "/api/user/self", u.AccessToken, "", 401)
	s.call(t, "POST", "/api/user/login", "", `{"username":"restored_mail","password":"strong-password"}`, 401)
	restored := restoreData[identity.Session](t, s.call(t, "POST", "/api/user/login", "", `{"username":"restored_mail","password":"new-strong-password"}`, 200))
	if restored.User.ID != u.User.ID || restored.AccessToken == "" {
		t.Fatal("password reset did not retain the original account")
	}
}

func TestRestoreComposedAccountMailUnavailableIsExplicit(t *testing.T) {
	s := restoreStack(t, nil)
	s.call(t, "GET", "/api/verification?email=restore@example.test", "", "", 503)
	s.call(t, "GET", "/api/reset_password?email=restore@example.test", "", "", 503)
}
