//go:build pgintegration

package ledger

import (
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/billing"
)

func TestFundingConversionRefundV2FailedRefundCannotRestoreRevokedOrigin(t *testing.T) {
	pool := testPool(t)
	account := fundedAccount(t, pool, 7, 0)
	p := NewPoster(pool)
	seedConversionV2(t, p, account, 100, 0)
	meta := map[string]any{"refund_no": "pending-v2", "refund_trade_no": "original-subscription-order"}
	fundingV2Post(t, p, billing.Entry{AccountID: account, Amount: -40, Kind: "refund", OperationID: "user-refund:pending-v2:reserve", Metadata: meta})
	if revoked, consumed, err := revokeConversionV2(pool, p, "revoked-before-release"); err != nil || revoked != 60 || consumed != 40 {
		t.Fatalf("reserved origin exposure=%d/%d err=%v", revoked, consumed, err)
	}
	rewardPost(t, p, account, 50, "topup", "", "unrelated-after-revocation")
	entries, outbox := count(t, pool, "ledger_entries"), count(t, pool, "balance_outbox")
	for range 2 {
		_, err := p.Post(ctx, billing.Entry{AccountID: account, Amount: 40, Kind: "refund", OperationID: "user-refund:pending-v2:release", Reason: "provider confirmed refund failure", Metadata: meta})
		if !errors.Is(err, ErrConversionOriginRevoked) {
			t.Fatalf("revoked conversion restored by failed refund: %v", err)
		}
	}
	if balance, _ := pgBalance(t, pool, account); balance != 50 || count(t, pool, "ledger_entries") != entries || count(t, pool, "balance_outbox") != outbox {
		t.Fatal("rejected restoration committed money or delivery")
	}
	var restored int64
	if err := pool.QueryRow(ctx, `SELECT sum(remaining_amount)::bigint FROM v3_billing.funding_lots WHERE source='subscription_conversion'`).Scan(&restored); err != nil || restored != 0 {
		t.Fatalf("revoked origin resurrected=%d err=%v", restored, err)
	}
	if revoked, consumed, err := revokeConversionV2(pool, p, "revoked-before-release"); err != nil || revoked != 60 || consumed != 40 {
		t.Fatalf("post-failure receipt replay=%d/%d err=%v", revoked, consumed, err)
	}
}
