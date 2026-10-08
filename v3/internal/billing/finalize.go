package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync/atomic"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// Finalize settles the actual cost, or releases the hold when out.Charge is
// false (Lua #3). Retrying it is safe.
func (s *Settler) Finalize(ctx context.Context, req *gateway.Request, out gateway.Outcome) error {
	h, ok := req.Reserve.(*hold)
	if !ok {
		return nil // rejected admission is canceled separately
	}
	actual, err := s.settlementPrice(h, out)
	if err != nil {
		return fmt.Errorf("%w: finalize %s: %v", gateway.ErrBillingUnavailable, req.ID, err)
	}
	rec := s.finalizeCall(req, out, h, actual)
	if len(h.funding) > 0 {
		rec, err = s.fundingFinalizeCall(req, out, h, actual)
		if err != nil {
			return fmt.Errorf("%w: finalize source price: %v", gateway.ErrBillingUnavailable, err)
		}
	}
	rec, err = s.appendCardCall(rec, h, out)
	if err != nil {
		return err
	}
	rec, err = s.appendMarketCall(rec, h, out, actual)
	if err != nil {
		return err
	}
	rec, err = appendEconomicsCall(rec, h, out)
	if err != nil {
		return err
	}
	if s.wal != nil && (h.local || s.br.open()) {
		return s.finalizeLocal(h, rec, actual)
	}
	res, err := s.runFinalize(ctx, rec)
	if err != nil {
		if isOutage(ctx, err) {
			s.br.fail()
			if s.wal != nil {
				return s.finalizeLocal(h, rec, actual)
			}
		}
		return fmt.Errorf("billing: finalize %s: %w", req.ID, err)
	}
	s.br.ok()
	if len(res) > 3 {
		actual = credits.Micro(res[3])
	}
	if res[0] == 1 {
		atomic.StoreInt64(&req.SettledAmount, int64(actual))
		s.local.observe(h.account, credits.Micro(res[1]))
		if res[2] == 1 {
			s.log.Warn("billing: settlement beyond overdraft cap", "request_id", req.ID, "account", h.account,
				"charged", int64(actual), "balance_after", res[1])
		}
	}
	return nil
}

func (s *Settler) runFinalize(ctx context.Context, rec walRecord) ([]int64, error) {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.RedisTimeout)
	defer cancel()
	args := make([]any, len(rec.Args))
	for i, a := range rec.Args {
		args[i] = a
	}
	if len(rec.Args) > 0 && (rec.Args[0] == "funding" || rec.Args[0] == "source-v1" || rec.Args[0] == "source-v2") {
		result, err := fundingFinalizeScript.Run(ctx, s.rdb, rec.Keys, args...).Int64Slice()
		if err == nil && len(result) > 4 && result[0] == 1 && result[4] > 0 {
			s.log.Warn("billing: subscription-only settlement shortfall", "request_id", rec.Args[5],
				"charged", result[3], "uncharged", result[4])
		}
		return result, err
	}
	return finalizeScript.Run(ctx, s.rdb, rec.Keys, args...).Int64Slice()
}

// finalizeCall builds the finalize script's KEYS and ARGV. The same record is
// run directly or, during an outage, written to the WAL and replayed later.
func (s *Settler) finalizeCall(req *gateway.Request, out gateway.Outcome, h *hold, actual credits.Micro) walRecord {
	var channel, cred int64
	if out.Target != nil {
		channel, cred = out.Target.ChannelID, out.Target.CredentialID
	}
	estimated := "0"
	if out.Usage.Estimated {
		estimated = "1"
	}
	i := strconv.FormatInt
	tools := "{}"
	if len(out.Usage.ToolCalls) > 0 {
		encoded, _ := json.Marshal(out.Usage.ToolCalls)
		tools = string(encoded)
	} // string/int64 maps cannot fail JSON encoding
	return walRecord{
		Keys: []string{h.keys.balance, h.keys.reservation, h.keys.done, redisx.KeyReservationOpen, redisx.StreamBillingEvents, h.keys.holds},
		Args: []string{
			i(int64(actual), 10), i(int64(s.cfg.OverdraftCap), 10), i(s.cfg.DoneTTL.Milliseconds(), 10), h.keys.member,
			FieldRequestID, req.ID, FieldAccountID, i(h.account, 10),
			FieldUserID, i(req.Principal.UserID, 10), FieldKeyID, i(req.Principal.KeyID, 10),
			FieldModel, req.Model, FieldChannelID, i(channel, 10), FieldCredentialID, i(cred, 10),
			FieldTerminal, out.Terminal.String(),
			"service_tier", out.Usage.ServiceTier, "service_tier_multiplier", i(pricingServiceTierMultiplier(h, out), 10),
			FieldPromptTokens, i(out.Usage.PromptTokens, 10), FieldOutputTokens, i(out.Usage.CompletionTokens, 10),
			FieldCachedTokens, i(out.Usage.CachedTokens, 10), FieldEstimated, estimated,
			FieldCacheWriteTokens, i(out.Usage.CacheWriteTokens, 10), FieldCacheWrite1hTokens, i(out.Usage.CacheWrite1hTokens, 10),
			FieldImageInputTokens, i(out.Usage.ImageInputTokens, 10), FieldImageOutputTokens, i(out.Usage.ImageOutputTokens, 10),
			FieldAudioInputTokens, i(out.Usage.AudioInputTokens, 10), FieldAudioOutputTokens, i(out.Usage.AudioOutputTokens, 10), FieldToolCalls, tools,
			FieldImageCount, i(out.Usage.ImageCount, 10), FieldAudioDurationMicros, i(out.Usage.AudioDurationMicros, 10),
			FieldAudioCharacters, i(out.Usage.AudioCharacters, 10), FieldVideoDurationMicros, i(out.Usage.VideoDurationMicros, 10),
			FieldAmount, i(int64(actual), 10), FieldTimestamp, i(s.cfg.Now().UnixMilli(), 10),
		},
	}
}
