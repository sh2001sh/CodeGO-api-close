//go:build pgintegration

package identity

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestEmailPasswordLoginNormalizationAndAccountBoundaries(t *testing.T) {
	pool, _ := testDeps(t)
	c, err := NewControl(pool, ControlConfig{SessionSecret: bytes.Repeat([]byte{1}, 32), EncryptionKey: bytes.Repeat([]byte{2}, 32)}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprint(time.Now().UnixNano())
	u, err := c.Register(ctx, RegisterInput{Username: "mail" + suffix, Email: "mail-" + suffix + "@example.test", Password: "strong-password"})
	if err != nil {
		t.Fatal(err)
	}
	for _, identifier := range []string{u.Username, " " + u.Username + " ", u.Email, "  MAIL-" + suffix + "@EXAMPLE.TEST  "} {
		got, err := c.Login(ctx, identifier, "strong-password")
		if err != nil || got.ID != u.ID {
			t.Fatalf("login with normalized identifier: id=%d err=%v", got.ID, err)
		}
	}
	for _, identifier := range []string{u.Username, u.Email, "unknown@example.test"} {
		if _, err := c.Login(ctx, identifier, "wrong-password"); !errors.Is(err, ErrCredentials) {
			t.Fatalf("invalid credentials accepted: %v", err)
		}
	}
	mustExec(t, pool, `INSERT INTO v3_identity.two_factor(user_id,secret_ciphertext,enabled) VALUES($1,decode(repeat('00',28),'hex'),true)`, u.ID)
	if got, err := c.Login(ctx, u.Email, "strong-password"); !errors.Is(err, ErrSecondFactorRequired) || got.ID != u.ID {
		t.Fatalf("email login bypassed second factor: id=%d err=%v", got.ID, err)
	}
	mustExec(t, pool, `UPDATE v3_identity.users SET status='disabled' WHERE id=$1`, u.ID)
	if _, err := c.Login(ctx, u.Email, "strong-password"); !errors.Is(err, ErrCredentials) {
		t.Fatalf("disabled account accepted: %v", err)
	}
	mustExec(t, pool, `UPDATE v3_identity.users SET status='active',deleted_at=now() WHERE id=$1`, u.ID)
	if _, err := c.Login(ctx, u.Email, "strong-password"); !errors.Is(err, ErrCredentials) {
		t.Fatalf("deleted account accepted: %v", err)
	}
}
