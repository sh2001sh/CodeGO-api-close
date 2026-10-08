//go:build pgintegration

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/internal/identity"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

func redesignJSON(t *testing.T, value any) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func redesignBind(t *testing.T, s *restoredStack, root, owner identity.Session, p commerce.Plan, key string) int64 {
	t.Helper()
	result := restoreData[struct {
		ID int64 `json:"id"`
	}](t, s.call(t, http.MethodPost, "/api/subscription/admin/bind", root.AccessToken,
		fmt.Sprintf(`{"user_id":%d,"plan_id":%d,"request_id":%q}`, owner.User.ID, p.ID, key), http.StatusOK))
	return result.ID
}

func redesignQuote(t *testing.T, s *restoredStack, owner identity.Session, id int64, status int) commerce.WalletConversionQuote {
	t.Helper()
	w := s.call(t, http.MethodPost, "/api/subscription/self/wallet-conversion/quote", owner.AccessToken,
		fmt.Sprintf(`{"subscription_id":%d}`, id), status)
	if status != http.StatusOK {
		return commerce.WalletConversionQuote{}
	}
	return restoreData[commerce.WalletConversionQuote](t, w)
}

func TestComposedRedesignStrictExpiryOwnershipAndAtomicConversion(t *testing.T) {
	s := restoreStack(t, nil)
	root, owner, other := s.register(t, "redesign_root"), s.register(t, "redesign_owner"), s.register(t, "redesign_other")
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `UPDATE v3_identity.users SET role='root' WHERE id=$1`, root.User.ID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/subscription/admin/redesign-rules", "/api/subscription/admin/referral-policy"} {
		s.call(t, http.MethodGet, path, "", "", http.StatusUnauthorized)
		s.call(t, http.MethodGet, path, owner.AccessToken, "", http.StatusForbidden)
		s.call(t, http.MethodGet, path, root.AccessToken, "", http.StatusOK)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE v3_identity.users SET role='admin' WHERE id=$1`, other.User.ID); err != nil {
		t.Fatal(err)
	}
	s.call(t, http.MethodGet, "/api/subscription/admin/redesign-rules", other.AccessToken, "", http.StatusForbidden)
	p := restoreData[commerce.Plan](t, s.call(t, http.MethodPost, "/api/subscription/admin/plans", root.AccessToken,
		`{"name":"legacy conversion boundary","price_minor":100,"currency":"cny","credits":1000,"duration_unit":"day","duration_value":30,"enabled":true,"reset_period":"never","policy_version":"legacy"}`, http.StatusOK))
	id := redesignBind(t, s, root, owner, p, "composed-old")
	q := redesignQuote(t, s, owner, id, http.StatusOK)
	if q.State != "needs_review" {
		t.Fatalf("unconfigured conversion must need review: %+v", q)
	}
	redesignQuote(t, s, other, id, http.StatusNotFound)
	s.call(t, http.MethodPut, "/api/subscription/admin/redesign-rules", root.AccessToken,
		redesignJSON(t, commerce.RedesignRules{ConversionRules: []commerce.ConversionRule{{PlanID: p.ID, BasisKey: q.BasisKey, SourceCredits: 1000, WalletCredits: 100000, Enabled: true, Reviewed: true, Note: "audited gift: zero paid principal"}}}), http.StatusOK)
	q = redesignQuote(t, s, owner, id, http.StatusOK)
	if q.TargetCredits != 100000 || q.PaidCredits != 0 {
		t.Fatalf("gift conversion manufactured paid funds: %+v", q)
	}
	// A quote taken while valid must fail at the exact subscription expiry.
	*s.now = s.now.Add(time.Minute)
	if _, err := s.pool.Exec(ctx, `UPDATE v3_commerce.subscriptions SET expires_at=$2 WHERE id=$1`, id, *s.now); err != nil {
		t.Fatal(err)
	}
	confirm := func(quote, request string) string {
		return redesignJSON(t, commerce.RedesignConfirmation{QuoteID: quote, RequestID: request, AcceptedTerms: true})
	}
	s.call(t, http.MethodPost, "/api/subscription/self/wallet-conversion/confirm", owner.AccessToken, confirm(q.QuoteID, "at-exact-expiry"), http.StatusConflict)
	redesignQuote(t, s, owner, id, http.StatusConflict)
	*s.now = s.now.Add(time.Nanosecond)
	redesignQuote(t, s, owner, id, http.StatusConflict)
	var wallet int64
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(sum(balance),0)::bigint FROM v3_billing.accounts WHERE kind='wallet' AND owner_id=$1`, owner.User.ID).Scan(&wallet); err != nil || wallet != 0 {
		t.Fatalf("expired conversion changed wallet: %d %v", wallet, err)
	}
	// A separate valid entitlement converts only its actual remainder.
	validID := redesignBind(t, s, root, owner, p, "composed-valid")
	var account int64
	if err := s.pool.QueryRow(ctx, `SELECT account_id FROM v3_commerce.subscriptions WHERE id=$1`, validID).Scan(&account); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.NewPoster(s.pool).Post(ctx, billing.Entry{AccountID: account, Amount: -400, Kind: "usage", OperationID: "composed-redesign-spent"}); err != nil {
		t.Fatal(err)
	}
	q = redesignQuote(t, s, owner, validID, http.StatusOK)
	if q.TargetCredits != 60000 || q.SourceCredits != 600 {
		t.Fatalf("remainder proportion wrong: %+v", q)
	}
	s.call(t, http.MethodPost, "/api/subscription/self/wallet-conversion/confirm", other.AccessToken, confirm(q.QuoteID, "stolen-conversion"), http.StatusNotFound)
	pending := restoreData[commerce.WalletConversion](t, s.call(t, http.MethodPost, "/api/subscription/self/wallet-conversion/confirm", owner.AccessToken, confirm(q.QuoteID, "composed-once"), http.StatusAccepted))
	if pending.State != "pending" {
		t.Fatalf("undelivered grant must await drain: %+v", pending)
	}
	rdb, err := redisx.Connect(redisx.Config{Addr: os.Getenv("V3_TEST_REDIS_ADDR")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	if _, err := ledger.NewBalanceRelay(s.pool, rdb, slog.New(slog.NewTextHandler(io.Discard, nil))).Step(ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		c := restoreData[commerce.WalletConversion](t, s.call(t, http.MethodPost, "/api/subscription/self/wallet-conversion/confirm", owner.AccessToken, confirm(q.QuoteID, "composed-once"), http.StatusOK))
		if c.State != "completed" || c.TargetCredits != 60000 {
			t.Fatalf("conversion result %+v", c)
		}
	}
	s.call(t, http.MethodGet, "/api/subscription/self/wallet-conversion/composed-once", other.AccessToken, "", http.StatusNotFound)
	var active, preserved bool
	if err := s.pool.QueryRow(ctx, `SELECT state='active',benefits_until=expires_at FROM v3_commerce.subscriptions WHERE id=$1`, validID).Scan(&active, &preserved); err != nil || active || !preserved {
		t.Fatalf("converted source/benefits active=%v preserved=%v err=%v", active, preserved, err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(sum(balance),0)::bigint FROM v3_billing.accounts WHERE kind='wallet' AND owner_id=$1`, owner.User.ID).Scan(&wallet); err != nil || wallet != 60000 {
		t.Fatalf("replayed conversion wallet=%d err=%v", wallet, err)
	}
	var restricted int64
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(sum(remaining_amount),0)::bigint FROM v3_billing.funding_lots WHERE account_id IN(SELECT id FROM v3_billing.accounts WHERE kind='wallet' AND owner_id=$1) AND source='subscription_conversion' AND non_transferable AND non_refundable`, owner.User.ID).Scan(&restricted); err != nil || restricted != 60000 {
		t.Fatalf("converted gifts lost permanent source restrictions=%d err=%v", restricted, err)
	}
}
