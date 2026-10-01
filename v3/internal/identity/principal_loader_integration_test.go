//go:build pgintegration

package identity

import (
	"errors"
	"slices"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func TestBackgroundPrincipalUsesCurrentOwnerAndAuthority(t *testing.T) {
	pool, _ := testDeps(t)
	seedKey(t, pool, 1, 10)
	seedKey(t, pool, 2, 20)
	mustExec(t, pool, `UPDATE v3_identity.api_keys SET allowed_models='{permitted}',allowed_cidrs='{10.0.0.0/8}',cross_group_retry=true,max_marketplace_multiplier=1.123456 WHERE id=10`)
	p, err := LoadPrincipal(ctx, pool, 1, 10)
	if err != nil || !slices.Equal(p.AllowedModels, []string{"permitted"}) || len(p.AllowedCIDRs) != 1 || !p.CrossGroupRetry || p.MaxMarketplaceMultiplierPPM != 1123456 {
		t.Fatalf("background policy=%+v err=%v", p, err)
	}
	if _, err = LoadPrincipal(ctx, pool, 2, 10); !errors.Is(err, gateway.ErrInvalidKey) {
		t.Fatalf("wrong owner admitted: %v", err)
	}
	for _, alias := range []string{"zero-hour", "monthly-pass"} {
		mustExec(t, pool, `UPDATE v3_identity.api_keys SET group_name=$1 WHERE id=10`, alias)
		if got, e := LoadPrincipal(ctx, pool, 1, 10); e != nil || got.Group != alias {
			t.Fatalf("virtual alias did not reach entitlement planner: %s %v", alias, e)
		}
	}
	for _, change := range []string{
		`status='disabled'`, `deleted_at=now()`, `expires_at=now()-interval '1 second'`, `group_name='unauthorized'`, `budget_limited=true`,
	} {
		mustExec(t, pool, `UPDATE v3_identity.api_keys SET status='active',deleted_at=NULL,expires_at=NULL,group_name=NULL,budget_limited=false WHERE id=10`)
		mustExec(t, pool, `UPDATE v3_identity.api_keys SET `+change+` WHERE id=10`)
		if _, err = LoadPrincipal(ctx, pool, 1, 10); !errors.Is(err, gateway.ErrInvalidKey) {
			t.Fatalf("change %s admitted: %v", change, err)
		}
	}
	mustExec(t, pool, `UPDATE v3_identity.api_keys SET status='active',deleted_at=NULL,expires_at=NULL,group_name=NULL,budget_limited=false WHERE id=10`)
	mustExec(t, pool, `UPDATE v3_identity.users SET status='disabled' WHERE id=1`)
	if _, err = LoadPrincipal(ctx, pool, 1, 10); !errors.Is(err, gateway.ErrInvalidKey) {
		t.Fatalf("disabled owner admitted: %v", err)
	}
}
