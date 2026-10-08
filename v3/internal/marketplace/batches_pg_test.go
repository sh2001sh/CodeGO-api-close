//go:build pgintegration

package marketplace

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func seedBatch(t *testing.T, f *fixture, purpose string, rewards []BatchReward) Batch {
	t.Helper()
	b := Batch{Name: "finite", Purpose: purpose, Budget: 5000, AncillaryCostPPM: 30000, ContributionSharePPM: 100000, CostsConfirmed: true, Rewards: rewards}
	if purpose == "credits" {
		b.Price, b.BaseCredits = 100, 100
	}
	b, err := f.s.SaveBatch(testContext, 1, b)
	if err != nil {
		t.Fatal(err)
	}
	b, err = f.s.ChangeBatchState(testContext, 1, b.ID, b.Revision, fmt.Sprintf("publish-%d", b.ID), true)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBlindBatchFiniteBudgetProbabilityAndReplay(t *testing.T) {
	f := newFixture(t)
	b := seedBatch(t, f, "credits", []BatchReward{{ID: "big", Title: "big", Kind: "credits", Amount: 80, Quantity: 1}, {ID: "small", Title: "small", Kind: "credits", Amount: 20, Quantity: 1}})
	if b.RequiredBudget != 300 || b.Rewards[0].InitialProbability != 0.5 || f.balance(t, 1) != 9700 {
		t.Fatalf("reserve %+v", b)
	}
	first, err := f.s.DrawBatch(testContext, 2, b.ID, "first", 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Charged != 100 || first.BaseCredits != 100 || first.Records[0].Reward.Amount != 80 || f.balance(t, 2) != 10080 {
		t.Fatalf("first %+v", first)
	}
	list, err := f.s.ListBatches(testContext, true)
	if err != nil {
		t.Fatal(err)
	}
	if list[0].RemainingCount != 1 || list[0].RemainingBudget != 120 || list[0].Rewards[0].RemainingProbability != 0 || list[0].Rewards[1].RemainingProbability != 1 {
		t.Fatalf("remaining %+v", list[0])
	}
	second, err := f.s.DrawBatch(testContext, 2, b.ID, "second", 1)
	if err != nil || second.Records[0].Reward.Amount != 20 {
		t.Fatalf("second %+v %v", second, err)
	}
	for range 3 {
		same, e := f.s.DrawBatch(testContext, 2, b.ID, "first", 1)
		want, encodeErr := json.Marshal(first)
		got, replayEncodeErr := json.Marshal(same)
		// Persisted JSON must be identical; in-memory time.Location identity
		// differs after decoding UTC on Linux and is not part of the contract.
		if e != nil || encodeErr != nil || replayEncodeErr != nil || !bytes.Equal(want, got) {
			t.Fatalf("replay want=%s got=%s errors=%v/%v/%v", want, got, e, encodeErr, replayEncodeErr)
		}
	}
	if _, err = f.s.DrawBatch(testContext, 2, b.ID, "first", 2); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed count %v", err)
	}
	if _, err = f.s.DrawBatch(testContext, 3, b.ID, "extra", 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("exhaustion %v", err)
	}
	if f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_open_records WHERE batch_id=$1`, b.ID) != 2 {
		t.Fatal("duplicate draw")
	}
	stats, err := f.s.BatchStats(testContext, b.ID)
	if err != nil || stats.DrawCount != 2 || stats.BaseCredits != 200 || stats.RewardCredits != 100 || stats.Spent != 300 || stats.Remaining != 0 {
		t.Fatalf("stats %+v %v", stats, err)
	}
	if _, err = f.s.SaveBatch(testContext, 1, b); !errors.Is(err, ErrConflict) {
		t.Fatalf("published edit %v", err)
	}
}

func TestBlindBatchLastPrizeConcurrencyAndInjectedRollback(t *testing.T) {
	f := newFixture(t)
	b := seedBatch(t, f, "credits", []BatchReward{{ID: "only", Title: "only", Kind: "credits", Amount: 500, Quantity: 1}})
	f.s.money = failRewards{base: f.poster}
	if _, err := f.s.DrawBatch(testContext, 2, b.ID, "rollback", 1); err == nil {
		t.Fatal("injected failure accepted")
	}
	if f.balance(t, 2) != 10000 || f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_open_records WHERE batch_id=$1`, b.ID) != 0 {
		t.Fatal("partial debit or prize")
	}
	f.s.money = f.poster
	var wg sync.WaitGroup
	results := make(chan error, 16)
	for i := range 16 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := f.s.DrawBatch(testContext, int64(2+i%2), b.ID, fmt.Sprintf("race-%d", i), 1)
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrInventory) {
			t.Fatal(err)
		}
	}
	if wins != 1 || f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_open_records WHERE batch_id=$1`, b.ID) != 1 {
		t.Fatalf("wins %d", wins)
	}
	var balance int64
	if err := f.pool.QueryRow(testContext, `SELECT balance FROM v3_billing.accounts WHERE id=$1`, b.EscrowAccountID).Scan(&balance); err != nil || balance != 0 {
		t.Fatalf("budget %d %v", balance, err)
	}
}

func TestBlindBatchStatsTrackActualUsageAndRefundWithoutCostFacts(t *testing.T) {
	f := newFixture(t)
	draft, err := f.s.SaveBatch(testContext, 1, Batch{Name: "unreserved", Purpose: "consumption", Budget: 5000, AncillaryCostPPM: 30000, ContributionSharePPM: 100000, Rewards: []BatchReward{{ID: "draft", Title: "draft", Kind: "credits", Amount: 10, Quantity: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if draft.RemainingBudget != 0 || draft.RequiredBudget != 10 {
		t.Fatalf("unpublished draft reports locked reserve: %+v", draft)
	}
	stats, err := f.s.BatchStats(testContext, draft.ID)
	if err != nil || stats.Reserved != 0 || stats.Remaining != 0 || stats.APIUsed != 0 {
		t.Fatalf("draft reports a reserve that was never locked: %+v %v", stats, err)
	}
	wallet, err := f.accounts.WalletAccount(testContext, 2)
	if err != nil {
		t.Fatal(err)
	}
	post := func(entry billing.Entry) {
		t.Helper()
		if _, err := f.poster.Post(testContext, entry); err != nil {
			t.Fatal(err)
		}
	}
	post(billing.Entry{AccountID: wallet, Amount: -10000, Kind: "usage", OperationID: "stats-clear"})
	post(billing.Entry{AccountID: wallet, Amount: 100, Kind: "topup", OperationID: "stats-purchase-funds"})
	b := seedBatch(t, f, "credits", []BatchReward{{ID: "r", Title: "r", Kind: "credits", Amount: 80, Quantity: 1}})
	if _, err := f.s.DrawBatch(testContext, 2, b.ID, "stats-draw", 1); err != nil {
		t.Fatal(err)
	}
	post(billing.Entry{AccountID: wallet, Amount: -90, Kind: "usage", RequestID: "stats-api", OperationID: "stats-api-debit"})
	stats, err = f.s.BatchStats(testContext, b.ID)
	if err != nil || stats.APIUsed != 90 || stats.Spent != 180 || stats.DrawCount != 1 {
		t.Fatalf("actual use requires procurement facts or counts grants as usage: %+v %v", stats, err)
	}
	post(billing.Entry{AccountID: wallet, Amount: 30, Kind: "refund", RequestID: "stats-api", OperationID: "stats-api-refund"})
	stats, err = f.s.BatchStats(testContext, b.ID)
	if err != nil || stats.APIUsed != 60 || stats.Spent != 180 {
		t.Fatalf("refund did not reduce actual use independently of reserve: %+v %v", stats, err)
	}
}

func TestBlindBatchDraftConflictAndFullBudgetRequired(t *testing.T) {
	f := newFixture(t)
	draft := Batch{Name: "draft", Purpose: "credits", Price: 100, BaseCredits: 100, Budget: 15000, AncillaryCostPPM: 30000, ContributionSharePPM: 100000, CostsConfirmed: true, Rewards: []BatchReward{{ID: "r", Title: "r", Kind: "credits", Amount: 14000, Quantity: 1}}}
	b, err := f.s.SaveBatch(testContext, 1, draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.SaveBatch(testContext, 1, draft); err != nil {
		t.Fatal(err)
	} // independent new draft
	old := b
	b.Name = "new"
	b, err = f.s.SaveBatch(testContext, 1, b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.SaveBatch(testContext, 1, old); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale save %v", err)
	}
	if _, err = f.s.ChangeBatchState(testContext, 1, b.ID, b.Revision, "underfunded", true); err == nil {
		t.Fatal("published without full reserve")
	}
	if f.balance(t, 1) != 10000 || f.count(t, `SELECT count(*) FROM v3_billing.accounts WHERE kind='promotion_budget'`) != 0 {
		t.Fatal("failed publish leaked funds")
	}
	b.CostsConfirmed = false
	b, err = f.s.SaveBatch(testContext, 1, b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ChangeBatchState(testContext, 1, b.ID, b.Revision, "unreviewed", true); !errors.Is(err, ErrConflict) {
		t.Fatalf("unreviewed %v", err)
	}
}

func TestBlindBatchConsumptionClaimsMatureOnceAndSurvivePause(t *testing.T) {
	f := newFixture(t)
	b := seedBatch(t, f, "consumption", []BatchReward{{ID: "r", Title: "r", Kind: "credits", Amount: 10, Quantity: 10}})
	wallet, err := f.accounts.WalletAccount(testContext, 2)
	if err != nil {
		t.Fatal(err)
	}
	post := func(e billing.Entry) {
		t.Helper()
		if _, err := f.poster.Post(testContext, e); err != nil {
			t.Fatal(err)
		}
	}
	post(billing.Entry{AccountID: wallet, Amount: -10000, Kind: "usage", OperationID: "clear-legacy"})
	if _, err = f.pool.Exec(testContext, `INSERT INTO v3_commerce.orders(id,user_id,kind,provider,trade_no,amount_minor,credits,currency,state,expires_at,paid_at,recognized_revenue_credits) VALUES(500,2,'topup','test','qualified',100,1000,'usd','paid',$1,$2,1000)`, time.Unix(f.now.Load(), 0).Add(30*24*time.Hour), time.Unix(f.now.Load(), 0)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(testContext, `INSERT INTO v3_commerce.payment_events(provider,event_id,trade_no,fingerprint) VALUES('test','qualified-event','qualified',$1)`, make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	post(billing.Entry{AccountID: wallet, Amount: 1000, Kind: "topup", OperationID: "order:paid:qualified"})
	post(billing.Entry{AccountID: wallet, Amount: -1000, Kind: "usage", OperationID: "qualified-usage", RequestID: "qualified-request"})
	if _, err = f.pool.Exec(testContext, `INSERT INTO v3_billing.funding_source_usage(request_id,account_id,policy_version,amount,wallet_equivalent_amount,revenue_multiplier_ppm,procurement_cost_amount,settled_at) VALUES('qualified-request',$1,'wallet',1000,1000,1000000,800,$2)`, wallet, time.Unix(f.now.Load(), 0)); err != nil {
		t.Fatal(err)
	}
	if n, err := f.s.AccrueBatchEntitlements(testContext, 100); err != nil || n != 0 {
		t.Fatalf("immature %d %v", n, err)
	}
	f.now.Add(int64(8 * 24 * time.Hour / time.Second))
	for i := range 2 {
		n, err := f.s.AccrueBatchEntitlements(testContext, 100)
		if err != nil || n != 1-i {
			t.Fatalf("mature %d %v", n, err)
		}
	}
	other := seedBatch(t, f, "consumption", []BatchReward{{ID: "r", Title: "r", Kind: "credits", Amount: 10, Quantity: 10}})
	// Reusing the same old fact across a second eligible campaign is impossible.
	if _, err = f.pool.Exec(testContext, `UPDATE v3_marketplace.blind_box_batches SET published_at=$2 WHERE id=$1`, other.ID, b.PublishedAt); err != nil {
		t.Fatal(err)
	}
	if n, err := f.s.AccrueBatchEntitlements(testContext, 100); err != nil || n != 0 {
		t.Fatalf("reused contribution %d %v", n, err)
	}
	b, err = f.s.ChangeBatchState(testContext, 1, b.ID, b.Revision, "pause", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.DrawBatch(testContext, 3, b.ID, "unauthorized", 1); !errors.Is(err, ErrInventory) {
		t.Fatalf("foreign entitlement %v", err)
	}
	got, err := f.s.DrawBatch(testContext, 2, b.ID, "claim", 1)
	if err != nil || got.Charged != 0 || got.Records[0].Reward.Amount != credits.Micro(10) {
		t.Fatalf("claim %+v %v", got, err)
	}
	if _, err = f.s.DrawBatch(testContext, 2, b.ID, "again", 1); !errors.Is(err, ErrInventory) {
		t.Fatalf("overclaim %v", err)
	}
}

func TestBlindBatchConsumptionExcludesUnknownRefundedReferralAndRewardFacts(t *testing.T) {
	for _, scenario := range []string{"unknown_cost", "provider_refund", "usage_refund", "referral_budget", "negative_contribution", "no_payment_event", "reward_consumption"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			b := seedBatch(t, f, "consumption", []BatchReward{{ID: "r", Title: "r", Kind: "credits", Amount: 10, Quantity: 100}})
			wallet, err := f.accounts.WalletAccount(testContext, 2)
			if err != nil {
				t.Fatal(err)
			}
			post := func(e billing.Entry) {
				t.Helper()
				if _, err := f.poster.Post(testContext, e); err != nil {
					t.Fatal(err)
				}
			}
			post(billing.Entry{AccountID: wallet, Amount: -10000, Kind: "usage", OperationID: "exclude-clear"})
			now := time.Unix(f.now.Load(), 0)
			if _, err = f.pool.Exec(testContext, `INSERT INTO v3_commerce.orders(id,user_id,kind,provider,trade_no,amount_minor,credits,currency,state,expires_at,paid_at,recognized_revenue_credits) VALUES(501,2,'topup','test','exclude',100,1000,'usd','paid',$1,$2,1000)`, now.Add(30*24*time.Hour), now); err != nil {
				t.Fatal(err)
			}
			if scenario != "no_payment_event" {
				if _, err = f.pool.Exec(testContext, `INSERT INTO v3_commerce.payment_events(provider,event_id,trade_no,fingerprint) VALUES('test','exclude-event','exclude',$1)`, make([]byte, 32)); err != nil {
					t.Fatal(err)
				}
			}
			entry := billing.Entry{AccountID: wallet, Amount: 1000, Kind: "topup", OperationID: "order:paid:exclude"}
			if scenario == "reward_consumption" {
				entry.Kind, entry.Reason = "reward", "blind_box_batch_reward"
			}
			post(entry)
			post(billing.Entry{AccountID: wallet, Amount: -1000, Kind: "usage", OperationID: "exclude-usage", RequestID: "exclude-request"})
			var cost any = int64(800)
			if scenario == "unknown_cost" {
				cost = nil
			}
			if scenario == "negative_contribution" {
				cost = int64(1001)
			}
			if _, err = f.pool.Exec(testContext, `INSERT INTO v3_billing.funding_source_usage(request_id,account_id,policy_version,amount,wallet_equivalent_amount,revenue_multiplier_ppm,procurement_cost_amount,settled_at) VALUES('exclude-request',$1,'wallet',1000,1000,1000000,$2,$3)`, wallet, cost, now); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "provider_refund":
				if _, err = f.pool.Exec(testContext, `INSERT INTO v3_commerce.provider_refund_progress(order_id,amount_minor,reversed_credits,updated_at) VALUES(501,1,10,$1)`, now); err != nil {
					t.Fatal(err)
				}
			case "usage_refund":
				post(billing.Entry{AccountID: wallet, Amount: 10, Kind: "refund", OperationID: "exclude-usage-refund", RequestID: "exclude-request"})
			case "referral_budget":
				if _, err = f.pool.Exec(testContext, `INSERT INTO v3_commerce.referral_consumption_qualifications(order_id,inviter_id,invitee_id,state,terms,reward_ppm,profit_share_ppm,ancillary_cost_ppm,window_days,delay_days,max_reward_credits,reserved_credits) VALUES(501,1,2,'active','{}',10000,200000,30000,30,7,200,200)`); err != nil {
					t.Fatal(err)
				}
			}
			f.now.Add(int64(8 * 24 * time.Hour / time.Second))
			if n, err := f.s.AccrueBatchEntitlements(testContext, 100); err != nil || n != 0 {
				t.Fatalf("unsafe qualification %d %v", n, err)
			}
			if f.count(t, `SELECT count(*) FROM v3_marketplace.blind_box_batch_entitlements WHERE batch_id=$1 AND available_count>0`, b.ID) != 0 {
				t.Fatal("exclusion minted boxes")
			}
		})
	}
}
