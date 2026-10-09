package channelmarket

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
	"github.com/sh2001sh/new-api/v3/pkg/exactfactor"
)

// AccrueUsageBatchTx accepts fresh primary events only; secondary funding
// never produces a second supplier settlement. Non-market channels retain the
// single-event behavior of ignoring fields that are irrelevant to settlement.
func (s *Service) AccrueUsageBatchTx(ctx context.Context, tx pgx.Tx, events []map[string]string, debitAccounts []int64) error {
	channels := make([]int64, 0, len(events))
	eligible := make([]map[string]string, 0, len(events))
	for _, fields := range events {
		if fields["funding_part"] == "secondary" || fields[billing.FieldModel] == "" {
			continue
		}
		channel, err := strconv.ParseInt(fields[billing.FieldChannelID], 10, 64)
		if err != nil || channel <= 0 {
			continue
		}
		channels = append(channels, channel)
		eligible = append(eligible, fields)
	}
	if len(channels) == 0 {
		return nil
	}
	groups, err := loadAccrualGroupsTx(ctx, tx, channels)
	if err != nil {
		return err
	}
	inputs := make([]SettlementInput, 0, len(eligible))
	for i, fields := range eligible {
		if groups[channels[i]].owner == 0 {
			continue
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
			continue
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
		request := fields[billing.FieldRequestID]
		if request == "" {
			return ErrInvalid
		}
		inputs = append(inputs, SettlementInput{RequestID: request, ChannelID: channels[i], ConsumerUserID: user, ConsumerMicro: credits.Micro(amount), GrossMicro: credits.Micro(gross), BillingSource: fields["billing_source"], MultiplierPPMExact: exactfactor.Decimal(factor)})
	}
	return s.accrueKnownGroupsTx(ctx, tx, inputs, groups, debitAccounts)
}
