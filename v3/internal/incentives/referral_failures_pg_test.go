//go:build pgintegration

package incentives

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestReferralPostingFailureLeavesBudgetEntitlementAndWalletUnchanged(t *testing.T) {
	s, _, now := fixture(t)
	ctx := context.Background()
	enableReferral(t, s, 1000000, 2000000)
	referralOrderFixture(t, s, 100, 2, "subscription", "standard_v2")
	reserveReferral(t, s, 100)
	fulfillReferral(t, s, 100, 2, "subscription")
	subscriptionReferralFact(t, s, 100, 10000000, 9000000, "retry-payout", true)
	*now = now.Add(8 * 24 * time.Hour)
	realPoster := s.poster
	s.poster = failedPoster{}
	if _, err := s.SettleReferrals(ctx, 100); err == nil {
		t.Fatal("failed post reported success")
	}
	p, err := s.ReferralPolicy(ctx)
	if err != nil || p.SpentCredits != 0 || p.ReservedCredits != 1000000 {
		t.Fatalf("failed budget=%+v err=%v", p, err)
	}
	if b := referralWalletBalance(t, s, 1); b != 0 {
		t.Fatalf("failed post minted %d", b)
	}
	s.poster = realPoster
	if _, err = s.SettleReferrals(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if b := referralWalletBalance(t, s, 1); b != 100000 {
		t.Fatalf("retry minted %d", b)
	}
}
func TestReferralReservationConcurrentBudgetNeverOverpromises(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	enableReferral(t, s, 1000000, 1000000)
	if _, err := pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username,inviter_id) VALUES(4,'competing-invitee',1)`); err != nil {
		t.Fatal(err)
	}
	referralOrderFixture(t, s, 100, 2, "topup", "legacy")
	referralOrderFixture(t, s, 101, 4, "topup", "legacy")
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := range 8 {
		wg.Add(1)
		id := int64(100 + i%2)
		go func() {
			defer wg.Done()
			errs <- pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return s.ReserveReferralTx(ctx, tx, id) })
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var promised, unavailable int64
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_commerce.referral_consumption_qualifications),(SELECT count(*) FROM v3_commerce.orders WHERE referral_terms->>'reason'='budget_unavailable')`).Scan(&promised, &unavailable); err != nil || promised != 1 || unavailable != 1 {
		t.Fatalf("promised=%d unavailable=%d err=%v", promised, unavailable, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_commerce.orders SET state='canceled' WHERE referral_terms->>'eligible'='true'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleReferrals(ctx, 100); err != nil {
		t.Fatal(err)
	}
	p, err := s.ReferralPolicy(ctx)
	if err != nil || p.ReservedCredits != 0 {
		t.Fatalf("cancellation leak=%+v %v", p, err)
	}
}
func TestReferralHTTPReturnsOnlyOwnRecordsAndRootAloneSeesCosts(t *testing.T) {
	s, _, _ := fixture(t)
	enableReferral(t, s, 1000000, 2000000)
	referralOrderFixture(t, s, 100, 2, "subscription", "standard_v2")
	reserveReferral(t, s, 100)
	fulfillReferral(t, s, 100, 2, "subscription")
	actor := Actor{UserID: 1, Role: "user"}
	mux := http.NewServeMux()
	s.Register(mux, func(*http.Request) (Actor, error) { return actor, nil })
	request := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	own := request("/api/user/aff/consumption-rewards")
	if own.Code != 200 {
		t.Fatal(own.Body.String())
	}
	for _, field := range []string{"ancillary_cost_ppm", "net_cost_credits", "total_budget_credits", "spent_credits", "net_revenue_credits"} {
		if strings.Contains(own.Body.String(), field) {
			t.Fatalf("private field exposed %s", field)
		}
	}
	actor = Actor{UserID: 3, Role: "user"}
	other := request("/api/user/aff/consumption-rewards?user_id=1")
	var response struct {
		Data ReferralRewardsSummary `json:"data"`
	}
	if err := json.Unmarshal(other.Body.Bytes(), &response); err != nil || len(response.Data.Records) != 0 {
		t.Fatalf("other user data=%s err=%v", other.Body.String(), err)
	}
	for _, role := range []string{"user", "admin"} {
		actor.Role = role
		if got := request("/api/subscription/admin/referral-policy").Code; got != http.StatusForbidden {
			t.Fatalf("role%s budget=%d", role, got)
		}
	}
	actor.Role = "root"
	admin := request("/api/subscription/admin/referral-qualifications")
	if admin.Code != 200 || !strings.Contains(admin.Body.String(), "net_cost_credits") {
		t.Fatalf("root typed query=%s", admin.Body.String())
	}
}
