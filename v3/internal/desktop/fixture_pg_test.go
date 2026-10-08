//go:build pgintegration

package desktop

import (
	"bytes"
	"context"
	"crypto/rand"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/identity"
	"github.com/sh2001sh/new-api/v3/migrations"
)

func desktopFixture(t *testing.T) (*Service, identity.User, identity.User) {
	t.Helper()
	dsn := os.Getenv("V3_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	base, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	// Keep the unique part inside PostgreSQL's 63-byte identifier limit.
	// Truncating a long test name used to remove its timestamp and collide.
	name := "desktop_" + strings.ToLower(rand.Text())
	if _, err := base.Exec(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	names, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		sql, err := migrations.Read(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatalf("migration %s: %v", name, err)
		}
	}
	id, err := identity.NewControl(pool, identity.ControlConfig{SessionSecret: bytes.Repeat([]byte{1}, 32), EncryptionKey: bytes.Repeat([]byte{2}, 32), PublicURL: "http://localhost:18084"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := id.Register(ctx, identity.RegisterInput{Username: "desktop_alice", Password: "strong-password"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := id.Register(ctx, identity.RegisterInput{Username: "desktop_bob", Password: "strong-password"})
	if err != nil {
		t.Fatal(err)
	}
	crypto, err := catalog.NewAESGCM(bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return New(pool, id, Config{PublicURL: "http://localhost:18084", Crypto: crypto}), a, b
}
