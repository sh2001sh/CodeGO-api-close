package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSettlementBacklogIndexStatementsPostgres(t *testing.T) {
	statements := settlementBacklogIndexStatements("postgres")
	require.Len(t, statements, 3)
	require.Contains(t, statements[0], "idx_marketplace_settlements_releasable")
	require.Contains(t, statements[0], "status = 'pending' AND referral_rebate_pending = false")
	require.Contains(t, statements[1], "idx_billing_outbox_pending_referral")
	require.Contains(t, statements[1], "status = 'pending' AND event_type = 'billing.reservation_settled'")
	require.Contains(t, statements[2], "idx_billing_outbox_pending_referral_account")
	require.Contains(t, statements[2], "status = 'pending' AND event_type = 'billing.reservation_settled'")
}

func TestMarketplaceReferralBacklogIndexStatementsPostgres(t *testing.T) {
	statements := marketplaceReferralBacklogIndexStatements("postgres")
	require.Len(t, statements, 1)
	require.Contains(t, statements[0], "idx_billing_outbox_pending_marketplace_referral")
	require.Contains(t, statements[0], "payload::jsonb ? 'referral_context'")
}

func TestMarketplaceReleaseOwnerIndexStatementsPostgres(t *testing.T) {
	statements := marketplaceReleaseOwnerIndexStatements("postgres")
	require.Len(t, statements, 1)
	require.Contains(t, statements[0], "idx_marketplace_settlements_releasable_owner")
	require.Contains(t, statements[0], "owner_user_id, available_at ASC, id ASC")
}
