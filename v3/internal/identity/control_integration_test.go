//go:build pgintegration

package identity

import (
	"bytes"
	"errors"
	"sync"
	"testing"
)

func TestControlRegistrationSessionsAndOwnedKeys(t *testing.T) {
	pool, _ := testDeps(t)
	c, err := NewControl(pool, ControlConfig{SessionSecret: bytes.Repeat([]byte{1}, 32), EncryptionKey: bytes.Repeat([]byte{2}, 32)}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	alice, err := c.Register(ctx, RegisterInput{Username: "alice", Password: "strong-password"})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := c.Register(ctx, RegisterInput{Username: "bob", Password: "strong-password"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Login(ctx, "alice", "wrong-password"); !errors.Is(err, ErrCredentials) {
		t.Fatalf("wrong password accepted: %v", err)
	}
	if _, err := c.Login(ctx, "alice", "strong-password"); err != nil {
		t.Fatal(err)
	}
	session, err := c.NewSession(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	user, err := c.Authenticate(ctx, session.AccessToken)
	if err != nil || user.ID != alice.ID {
		t.Fatalf("authenticated=%+v err=%v", user, err)
	}
	key, raw, err := c.CreateKey(ctx, alice.ID, KeyInput{Name: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c.RevealKey(ctx, alice.ID, key.ID); err != nil || got != raw {
		t.Fatalf("reveal mismatch: %v", err)
	}
	if _, err := c.RevealKey(ctx, bob.ID, key.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user read key: %v", err)
	}
	if err := c.DeleteKey(ctx, bob.ID, key.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user deleted key: %v", err)
	}
	keys, err := c.ListKeys(ctx, alice.ID, 0, 50)
	if err != nil || len(keys) != 1 {
		t.Fatalf("keys=%v err=%v", keys, err)
	}
	if err := c.DeleteKey(ctx, alice.ID, key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RevealKey(ctx, alice.ID, key.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted key still visible: %v", err)
	}
	if err := c.Logout(ctx, session.AccessToken); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Authenticate(ctx, session.AccessToken); !errors.Is(err, ErrCredentials) {
		t.Fatalf("revoked access accepted: %v", err)
	}
	if _, err := c.Refresh(ctx, session.RefreshToken); !errors.Is(err, ErrCredentials) {
		t.Fatalf("revoked refresh accepted: %v", err)
	}
}

func TestRefreshRotationHasExactlyOneConcurrentWinner(t *testing.T) {
	pool, _ := testDeps(t)
	c, err := NewControl(pool, ControlConfig{SessionSecret: bytes.Repeat([]byte{1}, 32), EncryptionKey: bytes.Repeat([]byte{2}, 32)}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Register(ctx, RegisterInput{Username: "refresh_user", Password: "strong-password"})
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.NewSession(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Go(func() { _, err := c.Refresh(ctx, s.RefreshToken); results <- err })
	}
	wg.Wait()
	close(results)
	winners, losers := 0, 0
	for err := range results {
		if err == nil {
			winners++
		} else if errors.Is(err, ErrCredentials) {
			losers++
		} else {
			t.Fatal(err)
		}
	}
	if winners != 1 || losers != 1 {
		t.Fatalf("refresh winners=%d losers=%d", winners, losers)
	}
}

func TestAdminPermissionBoundariesAndPasswordRevocation(t *testing.T) {
	pool, _ := testDeps(t)
	c, err := NewControl(pool, ControlConfig{SessionSecret: bytes.Repeat([]byte{1}, 32), EncryptionKey: bytes.Repeat([]byte{2}, 32)}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	root, err := c.Register(ctx, RegisterInput{Username: "root_actor", Password: "strong-password"})
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, pool, `UPDATE v3_identity.users SET role='root' WHERE id=$1`, root.ID)
	root.Role = "root"
	admin, err := c.CreateUser(ctx, root, AdminCreateInput{RegisterInput: RegisterInput{Username: "admin_actor", Password: "strong-password"}, Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := c.CreateUser(ctx, admin, AdminCreateInput{RegisterInput: RegisterInput{Username: "ordinary_user", Password: "strong-password"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateUser(ctx, user, AdminCreateInput{RegisterInput: RegisterInput{Username: "unauthorized", Password: "strong-password"}}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("ordinary user created account: %v", err)
	}
	if _, err := c.CreateUser(ctx, admin, AdminCreateInput{RegisterInput: RegisterInput{Username: "elevated_user", Password: "strong-password"}, Role: "root"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin created root: %v", err)
	}
	if err := c.DeleteUser(ctx, admin, root.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("admin deleted root: %v", err)
	}
	session, err := c.NewSession(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateProfile(ctx, user.ID, ProfileInput{Password: "changed-password", OriginalPassword: "wrong-password"}); !errors.Is(err, ErrCredentials) {
		t.Fatalf("password changed without proof: %v", err)
	}
	if _, err := c.UpdateProfile(ctx, user.ID, ProfileInput{Password: "changed-password", OriginalPassword: "strong-password"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Authenticate(ctx, session.AccessToken); !errors.Is(err, ErrCredentials) {
		t.Fatalf("password change left old session valid: %v", err)
	}
	if _, err := c.Login(ctx, user.Username, "changed-password"); err != nil {
		t.Fatal(err)
	}
}
