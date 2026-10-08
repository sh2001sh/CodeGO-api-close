//go:build pgintegration

package ledger

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func transferableV2(t *testing.T, pool *pgxpool.Pool, account int64, now time.Time) credits.Micro {
	t.Helper()
	var amount credits.Micro
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var err error
		amount, err = TransferableBalanceTx(ctx, tx, account, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return amount
}

func fundingV2Post(t *testing.T, p *Poster, e billing.Entry) {
	t.Helper()
	if _, err := p.Post(ctx, e); err != nil {
		t.Fatal(err)
	}
}

func referralV2Entry(account int64, amount credits.Micro, key string) billing.Entry {
	return billing.Entry{AccountID: account, Amount: amount, Kind: "reward", OperationID: key, Reason: "referral_consumption_reward", Metadata: map[string]any{"source": "referral_reward"}}
}

func TestFundingV2PermanentReferralAfter72HoursPaidFIFOAndReplay(t *testing.T) {
	pool := testPool(t)
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	account, p := rewardAccount(t, pool, now)
	p.now = func() time.Time { return now.Add(100 * time.Hour) }
	rewardPost(t, p, account, 100, "topup", "", "v2-paid")
	p.now = func() time.Time { return now.Add(101 * time.Hour) }
	reward := referralV2Entry(account, 100, "v2-referral")
	for range 2 {
		fundingV2Post(t, p, reward)
	}
	if got := lockedReward(t, pool, account, now.Add(1000*time.Hour)); got != 100 {
		t.Fatalf("permanent reward aged out: %d", got)
	}
	spend := billing.Entry{AccountID: account, Amount: -75, Kind: "usage", OperationID: "v2-paid-fifo"}
	for range 2 {
		fundingV2Post(t, p, spend)
	}
	if got := transferableV2(t, pool, account, now.Add(1000*time.Hour)); got != 25 {
		t.Fatalf("paid spending unlocked untouched reward: transferable=%d", got)
	}
	transfer := billing.Entry{AccountID: account, Amount: -26, Kind: "transfer", Reason: "wallet_peer_transfer_debit", OperationID: "v2-locked-transfer"}
	if _, err := p.Post(ctx, transfer); !errors.Is(err, ErrWalletRewardTransferLocked) {
		t.Fatalf("transfer bypassed permanent source: %v", err)
	}
	if bal, _ := pgBalance(t, pool, account); bal != 125 || count(t, pool, "ledger_entries") != 3 {
		t.Fatal("rejected transfer changed balance or ledger")
	}
	transfer.Amount, transfer.OperationID = -25, "v2-permitted-transfer"
	for range 2 {
		fundingV2Post(t, p, transfer)
	}
	if got := lockedReward(t, pool, account, now.Add(1000*time.Hour)); got != 100 {
		t.Fatalf("transfer consumed restricted lot: %d", got)
	}
	if _, err := p.Post(ctx, billing.Entry{AccountID: account, Amount: -1, Kind: "refund", OperationID: "v2-refund-reward"}); !errors.Is(err, ErrWalletRewardRefundLocked) {
		t.Fatalf("reward became refundable: %v", err)
	}
	rewardPost(t, p, account, -100, "usage", "", "v2-owner-can-spend")
	if bal, _ := pgBalance(t, pool, account); bal != 0 || lockedReward(t, pool, account, now) != 0 {
		t.Fatal("owner could not consume permanent reward normally")
	}
	if count(t, pool, "funding_lots") != 2 || count(t, pool, "wallet_reward_holds") != 0 {
		t.Fatal("permanent reward reused age-based holds or duplicated lots")
	}
}

func TestFundingV2TransferAndRefundSkipOlderRewardLots(t *testing.T) {
	for _, kind := range []string{"transfer", "refund"} {
		t.Run(kind, func(t *testing.T) {
			pool := testPool(t)
			now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
			account, p := rewardAccount(t, pool, now)
			fundingV2Post(t, p, referralV2Entry(account, 100, "older-reward"))
			p.now = func() time.Time { return now.Add(time.Hour) }
			rewardPost(t, p, account, 100, "topup", "", "newer-paid")
			debit := billing.Entry{AccountID: account, Amount: -100, Kind: kind, OperationID: "v2-permitted-debit"}
			if kind == "transfer" {
				debit.Reason = "wallet_peer_transfer_debit"
			}
			fundingV2Post(t, p, debit)
			var source string
			var remaining int64
			if err := pool.QueryRow(ctx, `SELECT source FROM v3_billing.funding_allocations WHERE request_id='native:operation:v2-permitted-debit'`).Scan(&source); err != nil || source != "topup" {
				t.Fatalf("restricted debit consumed %s: %v", source, err)
			}
			if err := pool.QueryRow(ctx, `SELECT remaining_amount FROM v3_billing.funding_lots WHERE source='referral_reward'`).Scan(&remaining); err != nil || remaining != 100 {
				t.Fatalf("permanent lot remaining=%d err=%v", remaining, err)
			}
			rewardPost(t, p, account, -30, "usage", "", "v2-reward-fifo")
			if got := lockedReward(t, pool, account, now.Add(1000*time.Hour)); got != 70 {
				t.Fatalf("actual reward spend did not consume its restriction: %d", got)
			}
		})
	}
}

func TestFundingV2ConversionPreservesOriginalRevenueAndRewardOrigin(t *testing.T) {
	pool := testPool(t)
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	account, p := rewardAccount(t, pool, now)
	paid := billing.Entry{AccountID: account, Amount: 80, Kind: "transfer", OperationID: "conversion-paid", Metadata: conversionFundingMetadata(80, 0)}
	for range 2 {
		fundingV2Post(t, p, paid)
	}
	p.now = func() time.Time { return now.Add(time.Minute) }
	fundingV2Post(t, p, billing.Entry{AccountID: account, Amount: 20, Kind: "transfer", OperationID: "conversion-reward", Metadata: conversionFundingMetadata(0, 20)})
	if _, err := pool.Exec(ctx, `UPDATE v3_billing.funding_source_policies SET revenue_multiplier_ppm=999999 WHERE source='subscription_conversion'`); err != nil {
		t.Fatal(err)
	}
	e := usageHookEvent(t, account, "conversion-usage", 90, false)
	e.fields["procurement_cost_multiplier_ppm"] = "300000"
	e.fields[billing.FieldTimestamp] = "1790899200000"
	e.fingerprint = fingerprint(e.fields)
	if _, err := post(ctx, pool, []event{e}, nil); err != nil {
		t.Fatal(err)
	}
	var paidUsed, rewardUsed, ppm, order int64
	if err := pool.QueryRow(ctx, `SELECT
	 (SELECT amount FROM v3_billing.funding_allocations WHERE lot_id=$1),
	 (SELECT amount FROM v3_billing.funding_allocations WHERE lot_id=$2),
	 revenue_multiplier_ppm,
	 (SELECT (metadata->>'original_order_id')::bigint FROM v3_billing.funding_lots WHERE lot_id=$1)
	 FROM v3_billing.request_economics WHERE request_id='conversion-usage'`, fundingID("lot", "conversion-paid"), fundingID("lot", "conversion-reward")).Scan(&paidUsed, &rewardUsed, &ppm, &order); err != nil {
		t.Fatal(err)
	}
	if paidUsed != 80 || rewardUsed != 10 || ppm != 711111 || order != 27 {
		t.Fatalf("conversion revenue/origin=%d/%d/%d/%d", paidUsed, rewardUsed, ppm, order)
	}
	if got := lockedReward(t, pool, account, now.Add(1000*time.Hour)); got != 10 {
		t.Fatalf("conversion reward restriction=%d", got)
	}
	paid.Metadata["revenue_multiplier_ppm"] = int64(999999)
	if _, err := p.Post(ctx, paid); !errors.Is(err, billing.ErrPostConflict) {
		t.Fatalf("conversion replay silently changed origin: %v", err)
	}
}

func TestFundingV2FailedConversionAndBusinessRollbackLeaveNoMoney(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 0)
	p := NewPoster(pool)
	meta := conversionFundingMetadata(80, 0)
	delete(meta, "revenue_multiplier_ppm")
	e := billing.Entry{AccountID: account, Amount: 80, Kind: "transfer", OperationID: "bad-conversion", Metadata: meta}
	if _, err := p.Post(ctx, e); err == nil {
		t.Fatal("missing source attribution was silently accepted")
	}
	if bal, _ := pgBalance(t, pool, account); bal != 0 || count(t, pool, "ledger_entries") != 0 || count(t, pool, "balance_outbox") != 0 {
		t.Fatal("invalid conversion committed money or delivery")
	}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := p.PostTx(ctx, tx, referralV2Entry(account, 100, "rollback-referral")); err != nil {
			return err
		}
		if _, err := p.PostTx(ctx, tx, billing.Entry{AccountID: account, Amount: -40, Kind: "usage", OperationID: "rollback-spend"}); err != nil {
			return err
		}
		return errors.New("business transition rejected")
	})
	if err == nil || count(t, pool, "funding_lots") != 0 || count(t, pool, "funding_allocations") != 0 || count(t, pool, "balance_outbox") != 0 {
		t.Fatal("rollback committed restricted provenance or money")
	}
	fundingV2Post(t, p, referralV2Entry(account, 100, "rollback-referral"))
	if bal, _ := pgBalance(t, pool, account); bal != 100 {
		t.Fatalf("retry after rollback balance=%d", bal)
	}
}

func TestFundingV2TransferUsesUnattributedOpeningWithoutRelabelingReward(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 100)
	p := NewPoster(pool)
	fundingV2Post(t, p, referralV2Entry(account, 100, "known-reward"))
	fundingV2Post(t, p, billing.Entry{AccountID: account, Amount: -100, Kind: "transfer", Reason: "wallet_peer_transfer_debit", OperationID: "opening-transfer"})
	var source string
	if err := pool.QueryRow(ctx, `SELECT source FROM v3_billing.funding_allocations`).Scan(&source); err != nil || source != "legacy_unattributed" {
		t.Fatalf("opening funds relabeled as reward: %s %v", source, err)
	}
	if got := lockedReward(t, pool, account, time.Now().Add(1000*time.Hour)); got != 100 {
		t.Fatalf("opening transfer unlocked known reward: %d", got)
	}
}

func TestFundingV2DatabaseRejectsTransferableReferralReward(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 0)
	if _, err := pool.Exec(ctx, `INSERT INTO v3_billing.funding_lots
	 (lot_id,source_account_id,account_id,source,idempotency_key,original_amount,remaining_amount,revenue_multiplier_ppm)
	 VALUES('invalid-reward','source',$1,'referral_reward','invalid-reward',100,100,1000000)`, account); err == nil {
		t.Fatal("database allowed a paid transferable referral reward")
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_billing.funding_source_policies SET revenue_multiplier_ppm=1 WHERE source='referral_reward'`); err == nil {
		t.Fatal("database allowed referral rewards to count as external revenue")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO v3_billing.funding_lots
	 (lot_id,source_account_id,account_id,source,idempotency_key,original_amount,remaining_amount,revenue_multiplier_ppm,metadata)
	 VALUES('invalid-conversion','source',$1,'subscription_conversion','invalid-conversion',100,100,1000000,
	 '{"source":"subscription_conversion","subscription_id":13,"original_order_id":0,"paid_principal_credits":0,"reward_credits":100}')`, account); err == nil {
		t.Fatal("database allowed conversion reward to become transferable paid principal")
	}
}

func TestFundingV2PeerCreditKeepsConversionOriginAndRejectsUnbackedCredit(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 0)
	recipient := fundedAccount(t, pool, 8, 0)
	p := NewPoster(pool)
	fundingV2Post(t, p, referralV2Entry(account, 20, "transfer-locked-first"))
	fundingV2Post(t, p, billing.Entry{AccountID: account, Amount: 100, Kind: "transfer", OperationID: "transfer-original-conversion", Metadata: conversionFundingMetadata(100, 0)})
	meta := map[string]any{"request_id": "origin-transfer", "sender_user_id": int64(7), "recipient_user_id": int64(8), "amount_micro": int64(80), "fee_micro": int64(1)}
	debit := billing.Entry{AccountID: account, Amount: -81, Kind: "transfer", OperationID: "wallet-transfer:origin-transfer:debit", Reason: "wallet_peer_transfer_debit", Metadata: meta}
	credit := billing.Entry{AccountID: recipient, Amount: 80, Kind: "transfer", OperationID: "wallet-transfer:origin-transfer:credit", Reason: "wallet_peer_transfer_credit", Metadata: meta}
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := p.PostTx(ctx, tx, debit); err != nil {
			return err
		}
		_, err := p.PostTx(ctx, tx, credit)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	fundingV2Post(t, p, credit)
	var source, sourceAccount string
	var amount, ppm, originalOrder int64
	if err := pool.QueryRow(ctx, `SELECT source,source_account_id,remaining_amount,revenue_multiplier_ppm,(metadata->>'original_order_id')::bigint
	 FROM v3_billing.funding_lots WHERE account_id=$1`, recipient).Scan(&source, &sourceAccount, &amount, &ppm, &originalOrder); err != nil {
		t.Fatal(err)
	}
	if source != "subscription_conversion" || sourceAccount != "native:account:"+fmt.Sprint(account) || amount != 80 || ppm != 800000 || originalOrder != 27 {
		t.Fatalf("peer conversion origin=%s/%s/%d/%d/%d", source, sourceAccount, amount, ppm, originalOrder)
	}
	if got := lockedReward(t, pool, account, time.Now().Add(1000*time.Hour)); got != 20 {
		t.Fatalf("peer transfer consumed sender's permanent reward: %d", got)
	}
	unbacked := billing.Entry{AccountID: recipient, Amount: 100, Kind: "transfer", OperationID: "wallet-transfer:unbacked:credit", Reason: "wallet_peer_transfer_credit", Metadata: map[string]any{"request_id": "unbacked"}}
	if _, err := p.Post(ctx, unbacked); err == nil {
		t.Fatal("peer credit manufactured money without original allocation")
	}
	if balance, _ := pgBalance(t, pool, recipient); balance != 80 {
		t.Fatalf("rejected peer credit changed balance=%d", balance)
	}
}

func TestFundingV2FailedRefundRestoresOriginalConversionLot(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 0)
	p := NewPoster(pool)
	metadata := conversionFundingMetadata(100, 0)
	metadata["refund_trade_no"] = "original-subscription-order"
	fundingV2Post(t, p, billing.Entry{AccountID: account, Amount: 100, Kind: "transfer", OperationID: "refund-original-conversion", Metadata: metadata})
	meta := map[string]any{"refund_no": "v2-no", "refund_trade_no": "original-subscription-order"}
	fundingV2Post(t, p, billing.Entry{AccountID: account, Amount: -40, Kind: "refund", OperationID: "user-refund:v2-no:reserve", Metadata: meta})
	release := billing.Entry{AccountID: account, Amount: 41, Kind: "refund", OperationID: "user-refund:v2-no:release", Reason: "provider confirmed refund failure", Metadata: meta}
	if _, err := p.Post(ctx, release); err == nil {
		t.Fatal("refund release manufactured extra principal")
	}
	release.Amount = 40
	for range 2 {
		fundingV2Post(t, p, release)
	}
	var remaining, ppm int64
	var source, trade string
	if err := pool.QueryRow(ctx, `SELECT remaining_amount,revenue_multiplier_ppm,source,metadata->>'refund_trade_no'
	 FROM v3_billing.funding_lots WHERE account_id=$1`, account).Scan(&remaining, &ppm, &source, &trade); err != nil {
		t.Fatal(err)
	}
	if remaining != 100 || ppm != 800000 || source != "subscription_conversion" || trade != "original-subscription-order" || count(t, pool, "funding_lots") != 1 {
		t.Fatalf("refund failed to restore original lot=%d/%d/%s/%s", remaining, ppm, source, trade)
	}
}
