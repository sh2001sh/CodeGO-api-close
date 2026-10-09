package channelmarket

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
	"github.com/sh2001sh/new-api/v3/pkg/exactfactor"
)

// AccrueUsageTx satisfies the ledger worker's business hook without exposing
// the ledger's private event type. Funds split across subscription and wallet
// generate exactly one accrual using the primary event's complete usage amount.
func (s *Service) AccrueUsageTx(ctx context.Context, tx pgx.Tx, fields map[string]string) error {
	if fields["funding_part"] == "secondary" || fields[billing.FieldModel] == "" {
		return nil
	}
	channel, err := strconv.ParseInt(fields[billing.FieldChannelID], 10, 64)
	if err != nil || channel <= 0 {
		return nil
	}
	var owner int64
	err = tx.QueryRow(ctx, `SELECT owner_user_id FROM v3_channelmarket.groups WHERE channel_id=$1`, channel).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	amount, err := strconv.ParseInt(fields[billing.FieldAmount], 10, 64)
	if err != nil || amount < 0 {
		return ErrInvalid
	}
	if fields["funding_part"] == "primary" {
		amount, err = strconv.ParseInt(fields["usage_total_amount"], 10, 64)
		if err != nil || amount < 0 {
			return ErrInvalid
		}
	}
	if amount == 0 {
		return nil
	}
	user, err := strconv.ParseInt(fields[billing.FieldUserID], 10, 64)
	if err != nil || user <= 0 {
		return ErrInvalid
	}
	gross, err := strconv.ParseInt(fields["marketplace_gross_micro"], 10, 64)
	if err != nil || gross < 0 {
		return ErrInvalid
	}
	factor, err := exactfactor.ParsePPM(fields["marketplace_multiplier_ppm"])
	if err != nil {
		return ErrInvalid
	}
	return s.AccrueTx(ctx, tx, SettlementInput{RequestID: fields[billing.FieldRequestID], ChannelID: channel, ConsumerUserID: user, ConsumerMicro: credits.Micro(amount), GrossMicro: credits.Micro(gross), BillingSource: fields["billing_source"], MultiplierPPMExact: exactfactor.Decimal(factor)})
}
