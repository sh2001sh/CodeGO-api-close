package ledger

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func fundingEventTime(e event) time.Time {
	ms, _ := strconv.ParseInt(e.fields[billing.FieldTimestamp], 10, 64)
	if ms <= 0 {
		stamp, _, _ := strings.Cut(e.streamID, "-")
		ms, _ = strconv.ParseInt(stamp, 10, 64)
	}
	return time.UnixMilli(ms).UTC()
}

func recordFundingUsageTx(ctx context.Context, tx pgx.Tx, e event, before credits.Micro) error {
	kind, err := accountKindTx(ctx, tx, e.accountID)
	if err != nil || kind != "wallet" {
		return err // key-budget entries are mirror limits, never actual funding
	}
	if err := allocateFundingTx(ctx, tx, e.accountID, e.requestID, credits.Micro(e.amount), before, fundingEventTime(e)); err != nil {
		return err
	}
	return consumeWalletRewardHoldsTx(ctx, tx, e.accountID, credits.Micro(e.amount))
}

func economicsNumber(e event, key string) (int64, error) {
	if e.fields[key] == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(e.fields[key], 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("ledger: invalid frozen economics %s", key)
	}
	return n, nil
}

// Record root-only economics snapshots in one insert per batch. Aggregate
// actual is a money charge; marketplace gross is explanatory metadata and is
// never added. A historical or same-batch request collision rejects the entire
// ledger transaction, including the source facts recorded first.
func recordRequestEconomicsTx(ctx context.Context, tx pgx.Tx, events []event) error {
	if err := recordFundingSourceFactsTx(ctx, tx, events); err != nil {
		return err
	}
	var requests, sources []string
	var channels, routes, amounts, subscriptions, costs, revenues []int64
	var settled []time.Time
	keys := []string{billing.FieldChannelID, "route_pool_id", "subscription_id", "procurement_cost_multiplier_ppm", "revenue_multiplier_ppm"}
	for _, e := range events {
		if e.fields[billing.FieldModel] == "" || e.fields["funding_part"] == "secondary" {
			continue
		}
		var values [5]int64
		for i, key := range keys {
			var err error
			values[i], err = economicsNumber(e, key)
			if err != nil {
				return err
			}
		}
		actual := e.amount
		if e.fields["funding_part"] == "primary" {
			var err error
			actual, err = economicsNumber(e, "usage_total_amount")
			if err != nil {
				return err
			}
		}
		source, err := requestEconomicsBillingSource(ctx, tx, e)
		if err != nil {
			return err
		}
		if err := fillRevenueMultiplier(ctx, tx, e, source, &values[4]); err != nil {
			return err
		}
		requests = append(requests, e.requestID)
		channels = append(channels, values[0])
		routes = append(routes, values[1])
		amounts = append(amounts, actual)
		sources = append(sources, source)
		subscriptions = append(subscriptions, values[2])
		costs = append(costs, values[3])
		revenues = append(revenues, values[4])
		settled = append(settled, fundingEventTime(e))
	}
	if len(requests) == 0 {
		return nil
	}
	result, err := tx.Exec(ctx, `INSERT INTO v3_billing.request_economics
	 (request_id,channel_id,route_pool_id,actual_amount,billing_source,subscription_id,procurement_cost_multiplier_ppm,revenue_multiplier_ppm,settled_at)
	 SELECT * FROM unnest($1::text[],$2::bigint[],$3::bigint[],$4::bigint[],$5::text[],$6::bigint[],$7::bigint[],$8::bigint[],$9::timestamptz[])
	 ON CONFLICT(request_id) DO NOTHING`, requests, channels, routes, amounts, sources, subscriptions, costs, revenues, settled)
	if err != nil {
		return err
	}
	if result.RowsAffected() != int64(len(requests)) {
		return fmt.Errorf("%w: economics batch inserted %d of %d requests", billing.ErrPostConflict, result.RowsAffected(), len(requests))
	}
	return nil
}

// requestEconomicsBillingSource resolves and validates e's billing source,
// defaulting to the funding account's kind when the event didn't freeze one.
func requestEconomicsBillingSource(ctx context.Context, tx pgx.Tx, e event) (string, error) {
	source := e.fields[billing.FieldBillingSource]
	if source == "" {
		kind, err := accountKindTx(ctx, tx, e.accountID)
		if err != nil {
			return "", err
		}
		source = "wallet"
		if kind == "subscription" {
			source = "subscription"
		}
	}
	if source != "wallet" && source != "subscription" && source != "mixed" {
		return "", fmt.Errorf("ledger: invalid frozen economics billing source")
	}
	return source, nil
}

// fillRevenueMultiplier populates revenueMultiplier when e didn't freeze one
// itself: wallet rates come from the lots actually consumed, while
// subscription rates come from the current source policy.
func fillRevenueMultiplier(ctx context.Context, tx pgx.Tx, e event, source string, revenueMultiplier *int64) error {
	if e.fields["funding_policy_version"] != "" {
		// Per-bucket facts are authoritative; a global legacy factor cannot
		// summarize new and old sources from the same request.
		return nil
	}
	if e.fields["revenue_multiplier_ppm"] != "" {
		return nil
	}
	// Wallet rates come from the immutable source lots actually consumed.
	if source == "wallet" {
		return tx.QueryRow(ctx, `SELECT coalesce(floor(sum(amount::numeric*revenue_multiplier_ppm)/nullif(sum(amount),0)+0.5),0)::bigint
		 FROM v3_billing.funding_allocations WHERE request_id=$1`, e.requestID).Scan(revenueMultiplier)
	}
	// V2 snapshots the actual subscription source policy at settlement.
	// Preserve that policy in this accepted transaction; redelivery does
	// not read it again or rewrite the immutable economics row.
	err := tx.QueryRow(ctx, `SELECT revenue_multiplier_ppm FROM v3_billing.funding_source_policies WHERE source='subscription'`).Scan(revenueMultiplier)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return nil
}
