package ledger

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// UsageRecorder shares the ledger event transaction with consumption-card
// audits and paid-usage progress. Implementations must never commit tx.
type UsageRecorder interface {
	RecordUsageTx(context.Context, pgx.Tx, int64, string, credits.Micro) error
	RecordDiscountUsageTx(context.Context, pgx.Tx, int64, int64, int64, string, credits.Micro, credits.Micro) error
}

// UsageHook allows another domain (such as channel-owner earnings) to post
// from the same fresh primary event. It runs before ledger account row locks.
type UsageHook func(context.Context, pgx.Tx, map[string]string) error

func recordMarketplaceUsage(ctx context.Context, tx pgx.Tx, recorder UsageRecorder, fields map[string]string) error {
	if recorder != nil {
		read := func(name string) (int64, error) {
			n, err := strconv.ParseInt(fields[name], 10, 64)
			if err != nil || n < 0 {
				return 0, fmt.Errorf("ledger: invalid usage field %s", name)
			}
			return n, nil
		}
		user, err := read(billing.FieldUserID)
		if err != nil || user == 0 {
			return fmt.Errorf("ledger: invalid usage user %q", fields[billing.FieldUserID])
		}
		amountField := billing.FieldAmount
		if fields["funding_part"] == "primary" {
			amountField = "usage_total_amount"
		}
		amount, err := read(amountField)
		if err != nil {
			return err
		}
		if err := recorder.RecordUsageTx(ctx, tx, user, fields[billing.FieldRequestID], credits.Micro(amount)); err != nil {
			return err
		}
		if fields[billing.FieldCardID] != "" {
			prop, err := read(billing.FieldCardID)
			if err != nil || prop == 0 {
				return fmt.Errorf("ledger: invalid usage prop")
			}
			channel, err := read(billing.FieldChannelID)
			if err != nil || channel == 0 {
				return fmt.Errorf("ledger: invalid discount channel")
			}
			before, err := read(billing.FieldCardBefore)
			if err != nil {
				return err
			}
			after, err := read(billing.FieldCardAfter)
			if err != nil || after != amount || after >= before {
				return fmt.Errorf("ledger: inconsistent consumption discount")
			}
			if err := recorder.RecordDiscountUsageTx(ctx, tx, user, prop, channel, fields[billing.FieldRequestID], credits.Micro(before), credits.Micro(after)); err != nil {
				return err
			}
		}
	}
	return nil
}
