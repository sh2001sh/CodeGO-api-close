//go:build pgintegration

package ledger

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func blindBatchEntry(account int64, amount credits.Micro, source, operation string) billing.Entry {
	return billing.Entry{AccountID: account, Amount: amount, Kind: "reward", Reason: source, OperationID: operation}
}

func TestBlindBatchFundingPermanentAPISpendOnly(t *testing.T) {
	for _, source := range []string{"blind_box_batch_base", "blind_box_batch_reward"} {
		t.Run(source, func(t *testing.T) {
			pool := testPool(t)
			now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
			account, p := rewardAccount(t, pool, now)
			grant := blindBatchEntry(account, 100, source, "blind-permanent")
			// Restrictions come from the trusted server operation, even when
			// informational metadata advertises incompatible properties.
			grant.Metadata = map[string]any{"source": "topup", "non_transferable": false, "non_refundable": false, "revenue_multiplier_ppm": int64(1000000)}
			for range 2 {
				fundingV2Post(t, p, grant)
			}
			p.now = func() time.Time { return now.Add(1000 * time.Hour) }
			if got := lockedReward(t, pool, account, p.now()); got != 100 {
				t.Fatalf("API-only funding aged out: %d", got)
			}
			for _, debit := range []struct {
				kind, reason string
				want         error
			}{
				{"transfer", "blind_box_purchase", ErrWalletAPICreditsPurchaseLocked},
				{"transfer", "subscription_purchase", ErrWalletAPICreditsPurchaseLocked},
				{"transfer", "wallet_peer_transfer_debit", ErrWalletRewardTransferLocked},
				{"refund", "owner_refund", ErrWalletRewardRefundLocked},
			} {
				entry := billing.Entry{AccountID: account, Amount: -1, Kind: debit.kind, Reason: debit.reason, OperationID: "blind-blocked:" + debit.reason}
				if _, err := p.Post(ctx, entry); !errors.Is(err, debit.want) {
					t.Fatalf("%s bypassed API-only source: %v", debit.reason, err)
				}
			}
			if balance, _ := pgBalance(t, pool, account); balance != 100 || count(t, pool, "ledger_entries") != 1 || count(t, pool, "balance_outbox") != 1 || count(t, pool, "funding_allocations") != 0 {
				t.Fatalf("rejected operations changed accounting: balance=%d", balance)
			}
			var frozenSource string
			var ppm int64
			var transfer, refund bool
			if err := pool.QueryRow(ctx, `SELECT source,revenue_multiplier_ppm,non_transferable,non_refundable FROM v3_billing.funding_lots WHERE account_id=$1`, account).Scan(&frozenSource, &ppm, &transfer, &refund); err != nil || frozenSource != source || ppm != 0 || !transfer || !refund {
				t.Fatalf("server source policy=%s/%d/%t/%t: %v", frozenSource, ppm, transfer, refund, err)
			}
			// Exercise the actual accepted gateway ledger-event consumer, not
			// merely a direct debit with a convenient kind string.
			e := usageHookEvent(t, account, "blind-api-usage", 100, false)
			for range 2 {
				if _, err := post(ctx, pool, []event{e}, nil); err != nil {
					t.Fatal(err)
				}
			}
			if balance, _ := pgBalance(t, pool, account); balance != 0 || lockedReward(t, pool, account, p.now()) != 0 || count(t, pool, "funding_allocations") != 1 || count(t, pool, "wallet_reward_holds") != 0 {
				t.Fatalf("API consumption/replay failed: balance=%d", balance)
			}
		})
	}
}

func TestBlindBatchFundingMixedPaidPurchasesPreserveRestrictedRemainder(t *testing.T) {
	for _, kind := range []string{"purchase", "transfer", "refund"} {
		t.Run(kind, func(t *testing.T) {
			pool := testPool(t)
			now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
			account, p := rewardAccount(t, pool, now)
			fundingV2Post(t, p, blindBatchEntry(account, 60, "blind_box_batch_base", "older-blind-base"))
			p.now = func() time.Time { return now.Add(time.Minute) }
			fundingV2Post(t, p, blindBatchEntry(account, 40, "blind_box_batch_reward", "older-blind-reward"))
			p.now = func() time.Time { return now.Add(1000 * time.Hour) }
			rewardPost(t, p, account, 100, "topup", "", "new-paid")
			debit := billing.Entry{AccountID: account, Amount: -100, Kind: "transfer", Reason: "blind_box_purchase", OperationID: "mixed-debit"}
			want := ErrWalletAPICreditsPurchaseLocked
			switch kind {
			case "transfer":
				debit.Reason, want = "wallet_peer_transfer_debit", ErrWalletRewardTransferLocked
			case "refund":
				debit.Kind, debit.Reason, want = "refund", "owner_refund", ErrWalletRewardRefundLocked
			}
			overpaid := debit
			overpaid.Amount, overpaid.OperationID = -101, "mixed-over-eligible-paid"
			if _, err := p.Post(ctx, overpaid); !errors.Is(err, want) {
				t.Fatalf("partial paid allocation leaked into API-only source: %v", err)
			}
			if balance, _ := pgBalance(t, pool, account); balance != 200 || count(t, pool, "funding_allocations") != 0 || count(t, pool, "ledger_entries") != 3 || count(t, pool, "balance_outbox") != 3 {
				t.Fatalf("failed partial allocation did not fully roll back: %d", balance)
			}
			for range 2 {
				fundingV2Post(t, p, debit)
			}
			var selected string
			var base, reward int64
			if err := pool.QueryRow(ctx, `SELECT
			 (SELECT source FROM v3_billing.funding_allocations WHERE request_id='native:operation:mixed-debit'),
			 (SELECT remaining_amount FROM v3_billing.funding_lots WHERE source='blind_box_batch_base'),
			 (SELECT remaining_amount FROM v3_billing.funding_lots WHERE source='blind_box_batch_reward')`).Scan(&selected, &base, &reward); err != nil || selected != "topup" || base != 60 || reward != 40 {
				t.Fatalf("mixed spend selected wrong source=%s/%d/%d: %v", selected, base, reward, err)
			}
			debit.Amount, debit.OperationID = -1, "mixed-after-paid-depleted"
			if _, err := p.Post(ctx, debit); !errors.Is(err, want) {
				t.Fatalf("depleted paid funds unlocked remainder: %v", err)
			}
			if balance, _ := pgBalance(t, pool, account); balance != 100 || count(t, pool, "funding_allocations") != 1 {
				t.Fatalf("failed mixed debit changed balance=%d", balance)
			}
		})
	}
}

func TestBlindBatchFundingFailedRefundRestoresOriginalPaidSource(t *testing.T) {
	pool := testPool(t)
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	account, p := rewardAccount(t, pool, now)
	fundingV2Post(t, p, blindBatchEntry(account, 100, "blind_box_batch_reward", "blind-refund-reward"))
	rewardPost(t, p, account, 50, "topup", "", "order:paid:blind-refund-topup")
	meta := map[string]any{"refund_no": "blind-refund", "refund_trade_no": "blind-refund-topup"}
	fundingV2Post(t, p, billing.Entry{AccountID: account, Amount: -40, Kind: "refund", OperationID: "user-refund:blind-refund:reserve", Metadata: meta})
	release := billing.Entry{AccountID: account, Amount: 40, Kind: "refund", OperationID: "user-refund:blind-refund:release", Metadata: meta}
	for range 2 {
		fundingV2Post(t, p, release)
	}
	var paid, restricted int64
	if err := pool.QueryRow(ctx, `SELECT
	 (SELECT remaining_amount FROM v3_billing.funding_lots WHERE reference_id='order:paid:blind-refund-topup'),
	 (SELECT remaining_amount FROM v3_billing.funding_lots WHERE source='blind_box_batch_reward')`).Scan(&paid, &restricted); err != nil || paid != 50 || restricted != 100 || count(t, pool, "funding_lots") != 2 {
		t.Fatalf("refund restoration lost source=%d/%d: %v", paid, restricted, err)
	}
	if balance, _ := pgBalance(t, pool, account); balance != 150 || transferableV2(t, pool, account, now.Add(1000*time.Hour)) != 50 {
		t.Fatalf("refund restoration manufactured/unlocked funding: %d", balance)
	}
}

func TestBlindBatchFundingMetadataCannotRelabelLegacySources(t *testing.T) {
	pool := testPool(t)
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	account, p := rewardAccount(t, pool, now)
	for i, kind := range []string{"topup", "reward"} {
		entry := billing.Entry{AccountID: account, Amount: 25, Kind: kind, OperationID: fmt.Sprintf("old-source-%d", i), Metadata: map[string]any{"source": "blind_box_batch_reward", "non_transferable": true, "non_refundable": true}}
		if kind == "reward" {
			entry.Reason = "blind_box_reward"
		}
		fundingV2Post(t, p, entry)
	}
	p.now = func() time.Time { return now.Add(1000 * time.Hour) }
	if got := transferableV2(t, pool, account, p.now()); got != 50 {
		t.Fatalf("new metadata changed legacy promises: %d", got)
	}
	fundingV2Post(t, p, billing.Entry{AccountID: account, Amount: -50, Kind: "transfer", Reason: "blind_box_purchase", OperationID: "legacy-can-buy"})
	if balance, _ := pgBalance(t, pool, account); balance != 0 {
		t.Fatalf("legacy source became API-only: %d", balance)
	}
}

func TestBlindBatchFundingVerifiedChargebackRemainsRecordable(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 0)
	p := NewPoster(pool)
	rewardPost(t, p, account, 50, "topup", "", "order:paid:batch-chargeback")
	rewardPost(t, p, account, -50, "usage", "", "spent-before-chargeback")
	fundingV2Post(t, p, blindBatchEntry(account, 100, "blind_box_batch_reward", "independent-blind-reward"))
	// A provider has already refunded the real payment. This operation is a
	// verified adjustment from commerce, not another user product purchase.
	entry := providerTopupRefundEntryV2(account, "batch-chargeback", 50)
	for range 2 {
		fundingV2Post(t, p, entry)
	}
	if balance, _ := pgBalance(t, pool, account); balance != 50 {
		t.Fatalf("confirmed chargeback was omitted/duplicated: %d", balance)
	}
}
