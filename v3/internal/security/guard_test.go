package security

import (
	"context"
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestDisabledGuardAndSafeErrors(t *testing.T) {
	g, err := New(nil, nil, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = g.Check(context.Background(), 3, "replayed-id"); err != nil {
		t.Fatal(err)
	}
	if err = g.RecordUsageTx(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if n, err := g.FlushStateCache(context.Background(), 2); n != 0 || err != nil {
		t.Fatalf("disabled flush %d %v", n, err)
	}
	if _, err = New(nil, nil, Config{Enabled: true}); err == nil {
		t.Fatal("missing enabled dependencies accepted")
	}
	var e *gateway.UpstreamError
	if !errors.As(unavailable(), &e) || e.Status != 503 || e.Body != nil {
		t.Fatal("unsafe dependency error")
	}
}

func TestAuditScopeAndParameterizedFilters(t *testing.T) {
	if _, _, err := auditWhere(Actor{}, Query{}); !errors.Is(err, ErrInvalid) {
		t.Fatal("anonymous query accepted")
	}
	where, args, err := auditWhere(Actor{UserID: 9}, Query{Source: "quote'", Search: "secret'"})
	if err != nil || len(args) != 4 || args[0] != false || args[1] != int64(9) || where == "" {
		t.Fatalf("scope %v %v %v", where, args, err)
	}
}
