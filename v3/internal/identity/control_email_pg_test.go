//go:build pgintegration

package identity

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func resetMailToken(t *testing.T, s *accountMailCapture) string {
	t.Helper()
	body := s.last()
	start := strings.Index(body, "https://codego.test/user/reset?")
	if start < 0 {
		t.Fatalf("missing reset link: %s", body)
	}
	line := strings.Split(body[start:], "\n")[0]
	link, err := url.Parse(line)
	if err != nil {
		t.Fatal(err)
	}
	return link.Query().Get("token")
}

func TestRegistrationEmailProofRequiresDeliveryExpiryAndSingleUse(t *testing.T) {
	c, now, sender := securityControl(t)
	c.cfg.RequireEmailVerification = true
	in := RegisterInput{Username: "verified_registration", Email: "alice@example.test", Password: "strong-password"}
	if _, err := c.Register(ctx, in); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("registered without code: %v", err)
	}
	if err := c.SendEmailVerification(ctx, in.Email, nil); err != nil {
		t.Fatal(err)
	}
	in.VerificationCode = sender.code()
	if err := c.SendEmailVerification(ctx, in.Email, nil); !errors.Is(err, ErrEmailRateLimit) {
		t.Fatalf("email cooldown bypassed: %v", err)
	}
	u, err := c.Register(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	var verified bool
	if err = c.pool.QueryRow(ctx, `SELECT email_verified FROM v3_identity.users WHERE id=$1`, u.ID).Scan(&verified); err != nil || !verified {
		t.Fatalf("verified=%v err=%v", verified, err)
	}
	in.Username = "replayed_registration"
	if _, err = c.Register(ctx, in); !errors.Is(err, ErrCredentials) {
		t.Fatalf("registration code replay accepted: %v", err)
	}
	in.Email = "expiry@example.test"
	in.Username = "expired_registration"
	if err = c.SendEmailVerification(ctx, in.Email, nil); err != nil {
		t.Fatal(err)
	}
	in.VerificationCode = sender.code()
	*now = now.Add(10 * time.Minute)
	if _, err = c.Register(ctx, in); !errors.Is(err, ErrCredentials) {
		t.Fatalf("expired registration code accepted: %v", err)
	}
	sender.fail = true
	if err = c.SendEmailVerification(ctx, "offline@example.test", nil); err == nil {
		t.Fatal("email delivery failure hidden")
	}
	var count int
	if err = c.pool.QueryRow(ctx, `SELECT count(*) FROM v3_identity.email_challenges WHERE email='offline@example.test'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed delivery retained proof count=%d err=%v", count, err)
	}
	c.cfg.EmailSender = nil
	if w := securityRequest(t, c, http.MethodGet, "/api/verification?email=offline@example.test", "", Session{}); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing SMTP status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestBindingEmailOwnershipAttemptLockoutAndReplay(t *testing.T) {
	c, now, sender := securityControl(t)
	alice := securityUser(t, c, "binding_alice")
	bob := securityUser(t, c, "binding_bob")
	email := "new@example.test"
	if err := c.SendEmailVerification(ctx, email, &alice.ID); err != nil {
		t.Fatal(err)
	}
	code := sender.code()
	if _, err := c.VerifyEmail(ctx, bob.ID, email, code); !errors.Is(err, ErrCredentials) {
		t.Fatalf("other user consumed binding code: %v", err)
	}
	for range 5 {
		if _, err := c.VerifyEmail(ctx, alice.ID, email, "bad"); !errors.Is(err, ErrCredentials) {
			t.Fatal(err)
		}
	}
	if _, err := c.VerifyEmail(ctx, alice.ID, email, code); !errors.Is(err, ErrCredentials) {
		t.Fatalf("brute-force locked proof accepted: %v", err)
	}
	*now = now.Add(time.Minute)
	if err := c.SendEmailVerification(ctx, email, &alice.ID); err != nil {
		t.Fatal(err)
	}
	code = sender.code()
	u, err := c.VerifyEmail(ctx, alice.ID, email, code)
	if err != nil || u.Email != email {
		t.Fatalf("bound=%+v err=%v", u, err)
	}
	if _, err = c.VerifyEmail(ctx, alice.ID, email, code); !errors.Is(err, ErrCredentials) {
		t.Fatalf("binding replay accepted: %v", err)
	}
	if err = c.SendEmailVerification(ctx, email, &bob.ID); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("other owner email verification sent: %v", err)
	}
	if _, err = c.UpdateProfile(ctx, alice.ID, ProfileInput{Email: "changed@example.test"}); err != nil {
		t.Fatal(err)
	}
	var verified bool
	if err = c.pool.QueryRow(ctx, `SELECT email_verified FROM v3_identity.users WHERE id=$1`, alice.ID).Scan(&verified); err != nil || verified {
		t.Fatalf("changed email kept verified=true: %v", err)
	}
}

func TestPasswordResetConcurrentSingleUseRevokesSessionsAndRetainsFactor(t *testing.T) {
	c, now, sender := securityControl(t)
	u := securityUser(t, c, "recover_user")
	if err := c.SendEmailVerification(ctx, "recover@example.test", &u.ID); err != nil {
		t.Fatal(err)
	}
	u, err := c.VerifyEmail(ctx, u.ID, "recover@example.test", sender.code())
	if err != nil {
		t.Fatal(err)
	}
	setup, err := c.SetupTwoFactor(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := totpCode(setup.Secret, now.Unix()/30)
	if err = c.EnableTwoFactor(ctx, u.ID, code); err != nil {
		t.Fatal(err)
	}
	s := securitySession(t, c, u)
	challenge := loginChallenge(t, c, u)
	if err = c.SendPasswordReset(ctx, u.Email); err != nil {
		t.Fatal(err)
	}
	token := resetMailToken(t, sender)
	before := len(sender.messages)
	if err = c.SendPasswordReset(ctx, u.Email); err != nil || len(sender.messages) != before {
		t.Fatalf("known-account resend cooldown exposed/rotated reset token: %v", err)
	}
	if err = c.SendPasswordReset(ctx, "unknown@example.test"); err != nil || len(sender.messages) != before {
		t.Fatalf("unknown account leaked/sent: %v", err)
	}
	if _, err = c.ResetPassword(ctx, u.Email, "incorrect", "replacement-password"); !errors.Is(err, ErrCredentials) {
		t.Fatalf("wrong reset token accepted: %v", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, err := c.ResetPassword(ctx, u.Email, token, "replacement-password"); results <- err })
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
		t.Fatalf("reset concurrent winners=%d", winners)
	}
	if _, err = c.Authenticate(ctx, s.AccessToken); !errors.Is(err, ErrCredentials) {
		t.Fatalf("reset left old session valid: %v", err)
	}
	if _, err = c.Refresh(ctx, s.RefreshToken); !errors.Is(err, ErrCredentials) {
		t.Fatalf("reset left old refresh valid: %v", err)
	}
	if _, err = c.FinishTwoFactorLogin(ctx, challenge, setup.BackupCodes[0]); !errors.Is(err, ErrCredentials) {
		t.Fatalf("reset left pending challenge valid: %v", err)
	}
	if _, err = c.Login(ctx, u.Username, "replacement-password"); !errors.Is(err, ErrSecondFactorRequired) {
		t.Fatalf("reset disabled 2FA: %v", err)
	}
	*now = now.Add(time.Minute)
	if err = c.SendPasswordReset(ctx, u.Email); err != nil {
		t.Fatal(err)
	}
	token = resetMailToken(t, sender)
	password, err := c.ResetPassword(ctx, u.Email, token, "")
	if err != nil || len(password) != 12 {
		t.Fatalf("legacy temporary password=%q err=%v", password, err)
	}
	if _, err = c.Login(ctx, u.Username, password); !errors.Is(err, ErrSecondFactorRequired) {
		t.Fatalf("temporary password failed: %v", err)
	}
	*now = now.Add(time.Minute)
	if err = c.SendPasswordReset(ctx, u.Email); err != nil {
		t.Fatal(err)
	}
	token = resetMailToken(t, sender)
	*now = now.Add(10 * time.Minute)
	if _, err = c.ResetPassword(ctx, u.Email, token, "expired-password"); !errors.Is(err, ErrCredentials) {
		t.Fatalf("expired reset accepted: %v", err)
	}
}

func TestRegistrationPreservesInviterAndImportedEmailPolicies(t *testing.T) {
	c, _, sender := securityControl(t)
	inviter := securityUser(t, c, "email_inviter")
	mustExec(t, c.pool, `UPDATE v3_identity.users SET aff_code='INVITE123' WHERE id=$1`, inviter.ID)
	mustExec(t, c.pool, `INSERT INTO v3_platform.settings(key,value) VALUES
	 ('EmailVerificationEnabled','true'),('EmailDomainRestrictionEnabled','true'),('EmailDomainWhitelist','"allowed.test,second.test"')`)
	in := RegisterInput{Username: "policy_invitee", Password: "strong-password", Email: "invitee@allowed.test", AffiliateCode: "INVITE123"}
	if _, err := c.Register(ctx, in); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("imported verification setting bypassed: %v", err)
	}
	if err := c.SendEmailVerification(ctx, "name.surname@allowed.test", nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("registration alias allowed: %v", err)
	}
	if err := c.SendEmailVerification(ctx, "user@other.test", nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("domain whitelist bypassed: %v", err)
	}
	if err := c.SendEmailVerification(ctx, in.Email, nil); err != nil {
		t.Fatal(err)
	}
	in.VerificationCode = sender.code()
	u, err := c.Register(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	var got int64
	if err = c.pool.QueryRow(ctx, `SELECT inviter_id FROM v3_identity.users WHERE id=$1`, u.ID).Scan(&got); err != nil || got != inviter.ID {
		t.Fatalf("inviter=%d want=%d err=%v", got, inviter.ID, err)
	}
	mustExec(t, c.pool, `UPDATE v3_platform.settings SET value='"broken"' WHERE key='EmailVerificationEnabled'`)
	in.Username = "malformed_policy"
	if _, err = c.Register(ctx, in); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("malformed verification policy fell open: %v", err)
	}
}

func TestMailboxUniquenessFencesConcurrentRegistration(t *testing.T) {
	c, _, _ := securityControl(t)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, name := range []string{"mailbox_owner_a", "mailbox_owner_b"} {
		wg.Go(func() {
			_, err := c.Register(ctx, RegisterInput{Username: name, Email: "SINGLE@example.test", Password: "strong-password"})
			results <- err
		})
	}
	wg.Wait()
	close(results)
	winners, duplicates := 0, 0
	for err := range results {
		if err == nil {
			winners++
		} else if errors.Is(err, ErrDuplicate) {
			duplicates++
		} else {
			t.Fatal(err)
		}
	}
	if winners != 1 || duplicates != 1 {
		t.Fatalf("mailbox concurrent winners=%d duplicates=%d", winners, duplicates)
	}
}
