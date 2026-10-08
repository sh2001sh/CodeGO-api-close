//go:build pgintegration

package identity

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTwoFactorLoginBackupReplayAndRevocation(t *testing.T) {
	c, now, _ := securityControl(t)
	u := securityUser(t, c, "totp_user")
	before := securitySession(t, c, u)
	setup, err := c.SetupTwoFactor(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	var ciphertext []byte
	if err = c.pool.QueryRow(ctx, `SELECT secret_ciphertext FROM v3_identity.two_factor WHERE user_id=$1`, u.ID).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, []byte(setup.Secret)) {
		t.Fatal("TOTP secret stored in plaintext")
	}
	code, _ := totpCode(setup.Secret, now.Unix()/30)
	if err = c.EnableTwoFactor(ctx, u.ID, code); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Authenticate(ctx, before.AccessToken); !errors.Is(err, ErrCredentials) {
		t.Fatalf("pre-factor session valid: %v", err)
	}
	if _, err = c.Login(ctx, u.Username, "strong-password"); !errors.Is(err, ErrSecondFactorRequired) {
		t.Fatalf("password bypassed factor: %v", err)
	}
	other := securityUser(t, c, "switch_previous_user")
	previous := securitySession(t, c, other)
	if w := securityRequest(t, c, http.MethodPost, "/api/user/login", `{"username":"totp_user","password":"strong-password"}`, previous); w.Code != http.StatusOK {
		t.Fatalf("switch challenge: %d %s", w.Code, w.Body.String())
	}
	if _, err = c.Authenticate(ctx, previous.AccessToken); !errors.Is(err, ErrCredentials) {
		t.Fatalf("pending factor retained previous authenticated session: %v", err)
	}
	challenge := loginChallenge(t, c, u)
	if _, err = c.FinishTwoFactorLogin(ctx, challenge, code); !errors.Is(err, ErrCredentials) {
		t.Fatalf("enable code replay: %v", err)
	}
	*now = now.Add(30 * time.Second)
	code, _ = totpCode(setup.Secret, now.Unix()/30)
	s, err := c.FinishTwoFactorLogin(ctx, challenge, code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Authenticate(ctx, s.AccessToken); err != nil {
		t.Fatal(err)
	}
	if _, err = c.FinishTwoFactorLogin(ctx, challenge, setup.BackupCodes[0]); !errors.Is(err, ErrCredentials) {
		t.Fatalf("challenge replay: %v", err)
	}
	challenge = loginChallenge(t, c, u)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, err := c.FinishTwoFactorLogin(ctx, challenge, setup.BackupCodes[0]); results <- err })
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, ErrCredentials) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent backup/challenge winners=%d", winners)
	}
	challenge = loginChallenge(t, c, u)
	if _, err = c.FinishTwoFactorLogin(ctx, challenge, setup.BackupCodes[0]); !errors.Is(err, ErrCredentials) {
		t.Fatalf("backup replay: %v", err)
	}
	status, err := c.TwoFactorStatus(ctx, u.ID)
	if err != nil || !status.Enabled || status.BackupCodesRemaining != 3 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if err = c.DisableTwoFactor(ctx, u.ID, setup.BackupCodes[1]); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Authenticate(ctx, s.AccessToken); !errors.Is(err, ErrCredentials) {
		t.Fatalf("disabled factor kept old session: %v", err)
	}
	if _, err = c.Login(ctx, u.Username, "strong-password"); err != nil {
		t.Fatalf("factor remains after disable: %v", err)
	}
}

func TestTwoFactorLockExpiryRegenerationAndAdminPermissions(t *testing.T) {
	c, now, _ := securityControl(t)
	u := securityUser(t, c, "locked_totp")
	setup, err := c.SetupTwoFactor(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := totpCode(setup.Secret, now.Unix()/30)
	if err = c.EnableTwoFactor(ctx, u.ID, code); err != nil {
		t.Fatal(err)
	}
	challenge := loginChallenge(t, c, u)
	for range 5 {
		if _, err = c.FinishTwoFactorLogin(ctx, challenge, "xxxx"); !errors.Is(err, ErrCredentials) {
			t.Fatal(err)
		}
	}
	status, err := c.TwoFactorStatus(ctx, u.ID)
	if err != nil || !status.Locked {
		t.Fatalf("lock status=%+v err=%v", status, err)
	}
	if _, err = c.FinishTwoFactorLogin(ctx, challenge, setup.BackupCodes[0]); !errors.Is(err, ErrCredentials) {
		t.Fatalf("locked backup accepted: %v", err)
	}
	*now = now.Add(5 * time.Minute)
	if _, err = c.FinishTwoFactorLogin(ctx, challenge, setup.BackupCodes[0]); !errors.Is(err, ErrCredentials) {
		t.Fatalf("expired challenge accepted: %v", err)
	}
	challenge = loginChallenge(t, c, u)
	s, err := c.FinishTwoFactorLogin(ctx, challenge, setup.BackupCodes[0])
	if err != nil {
		t.Fatal(err)
	}
	code, _ = totpCode(setup.Secret, now.Unix()/30)
	newCodes, err := c.RegenerateTwoFactorBackupCodes(ctx, u.ID, code)
	if err != nil || len(newCodes) != 4 {
		t.Fatalf("regenerate=%v err=%v", newCodes, err)
	}
	if err = c.DisableTwoFactor(ctx, u.ID, setup.BackupCodes[1]); !errors.Is(err, ErrCredentials) {
		t.Fatalf("old backup survived regeneration: %v", err)
	}
	if w := securityRequest(t, c, http.MethodGet, "/api/user/2fa/stats", "", s); w.Code != http.StatusForbidden {
		t.Fatalf("user read statistics: %d %s", w.Code, w.Body.String())
	}
	admin := securityUser(t, c, "factor_admin")
	mustExec(t, c.pool, `UPDATE v3_identity.users SET role='admin' WHERE id=$1`, admin.ID)
	admin.Role = "admin"
	root := securityUser(t, c, "factor_root")
	mustExec(t, c.pool, `UPDATE v3_identity.users SET role='root' WHERE id=$1`, root.ID)
	root.Role = "root"
	if err = c.AdminDisableTwoFactor(ctx, u, u.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("user admin reset: %v", err)
	}
	if err = c.AdminDisableTwoFactor(ctx, admin, root.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin reset root: %v", err)
	}
	if err = c.AdminDisableTwoFactor(ctx, root, root.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("root bypassed own factor proof: %v", err)
	}
	if w := securityRequest(t, c, http.MethodGet, "/api/user/2fa/stats", "", securitySession(t, c, root)); w.Code != http.StatusOK {
		t.Fatalf("root stats: %d %s", w.Code, w.Body.String())
	}
	if err = c.AdminDisableTwoFactor(ctx, admin, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = c.FinishTwoFactorLogin(ctx, challenge, newCodes[0]); !errors.Is(err, ErrCredentials) {
		t.Fatalf("admin reset left challenge valid: %v", err)
	}
}

func TestTwoFactorHTTPRequiresSessionAndSameOrigin(t *testing.T) {
	c, _, _ := securityControl(t)
	for _, path := range []string{"/api/user/2fa/status", "/api/user/2fa/stats"} {
		if w := securityRequest(t, c, http.MethodGet, path, "", Session{}); w.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous %s: %d", path, w.Code)
		}
	}
	u := securityUser(t, c, "csrf_totp")
	s := securitySession(t, c, u)
	for _, path := range []string{"/api/user/2fa/enable", "/api/user/2fa/disable", "/api/user/2fa/backup_codes"} {
		if w := securityRequest(t, c, http.MethodPost, path, `{"code":"000000","extra":true}`, s); w.Code != http.StatusBadRequest {
			t.Fatalf("malformed %s: %d", path, w.Code)
		}
	}
	r := httptest.NewRequest(http.MethodPost, "https://codego.test/api/user/2fa/setup", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://attacker.test")
	r.Header.Set("Authorization", "Bearer "+s.AccessToken)
	w := httptest.NewRecorder()
	c.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-site 2FA setup: %d %s", w.Code, w.Body.String())
	}
}
