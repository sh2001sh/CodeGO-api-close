package billing

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
	"github.com/sh2001sh/new-api/v3/pkg/exactfactor"
)

type workflowPart struct {
	Account int64         `json:"account"`
	Amount  credits.Micro `json:"amount"`
}

// The request body is bound by digest and supplied by the durable task on
// restore. Credentials, URLs and client authorization are never serialized.
type workflowSnapshot struct {
	Version           int                      `json:"version"`
	RequestID         string                   `json:"request_id"`
	UserID            int64                    `json:"user_id"`
	KeyID             int64                    `json:"key_id"`
	Group             string                   `json:"group"`
	Model             string                   `json:"model"`
	BodyDigest        string                   `json:"body_digest"`
	ChannelID         int64                    `json:"channel_id"`
	CredentialID      int64                    `json:"credential_id"`
	Account           int64                    `json:"account"`
	Amount            credits.Micro            `json:"amount"`
	Price             catalog.Price            `json:"price"`
	Multiplier        float64                  `json:"multiplier"`
	Headers           map[string]string        `json:"headers,omitempty"`
	Now               time.Time                `json:"now"`
	Funding           []workflowPart           `json:"funding,omitempty"`
	BudgetAccount     int64                    `json:"budget_account,omitempty"`
	CardMultiplier    float64                  `json:"card_multiplier"`
	CardChannels      map[int64]bool           `json:"card_channels,omitempty"`
	Cards             []catalog.MultiplierCard `json:"cards,omitempty"`
	TargetPrices      map[string]targetPrice   `json:"target_prices,omitempty"`
	SourceMode        bool                     `json:"source_mode,omitempty"`
	SourceLimits      map[int64]sourceLimit    `json:"source_limits,omitempty"`
	FundingPreference string                   `json:"funding_preference,omitempty"`
}

func requestDigest(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func safePricingHeaders(headers map[string]string) map[string]string {
	result := make(map[string]string, len(headers))
	for name, value := range headers {
		lower := strings.ToLower(name)
		if strings.Contains(lower, "auth") || strings.Contains(lower, "cookie") || strings.Contains(lower, "secret") ||
			strings.Contains(lower, "token") || strings.Contains(lower, "api-key") || strings.Contains(lower, "apikey") || strings.Contains(lower, "api_key") {
			continue
		}
		result[name] = value
	}
	return result
}

func encodeWorkflowHold(req *gateway.Request, h *hold) (workflow.Reservation, error) {
	if len(req.Targets) != 1 || h.local {
		return workflow.Reservation{}, errors.New("billing: durable task requires one target and a Redis hold")
	}
	target := req.Targets[0]
	data := workflowSnapshot{Version: 1, RequestID: req.ID, UserID: req.Principal.UserID, KeyID: req.Principal.KeyID,
		Group: req.Principal.Group, Model: req.Model, BodyDigest: requestDigest(req.Body), ChannelID: target.ChannelID,
		CredentialID: target.CredentialID, Account: h.account, Amount: h.amount, Price: h.price, Multiplier: h.multiplier,
		Headers: safePricingHeaders(h.pricingInput.Headers), Now: h.pricingInput.Now, BudgetAccount: h.budgetAccount,
		CardMultiplier: h.cardMultiplier, CardChannels: h.cardChannels, Cards: h.cards, TargetPrices: h.targetPrices, SourceMode: h.sourceMode, SourceLimits: h.sourceLimits, FundingPreference: h.fundingPreference}
	for _, p := range h.funding {
		data.Funding = append(data.Funding, workflowPart{p.account, p.amount})
	}
	encoded, err := json.Marshal(data)
	return workflow.Reservation{Data: encoded, EstimatedCredits: h.amount}, err
}

func restoreWorkflowHold(req *gateway.Request, reservation workflow.Reservation) (*hold, error) {
	var data workflowSnapshot
	decoder := json.NewDecoder(bytes.NewReader(reservation.Data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&data); err != nil {
		return nil, err
	}
	if req == nil || data.Version != 1 || len(req.Targets) != 1 || data.RequestID != req.ID ||
		data.UserID != req.Principal.UserID || data.KeyID != req.Principal.KeyID || data.Group != req.Principal.Group ||
		data.Model != req.Model || data.BodyDigest != requestDigest(req.Body) || data.Account <= 0 || data.Amount < 0 ||
		data.Amount != reservation.EstimatedCredits || data.ChannelID != req.Targets[0].ChannelID || data.CredentialID != req.Targets[0].CredentialID {
		return nil, errors.New("billing: task reservation identity mismatch")
	}
	if err := pricing.Validate(data.Price); err != nil {
		return nil, err
	}
	for _, target := range data.TargetPrices {
		if err := pricing.Validate(target.Price); err != nil {
			return nil, err
		}
		if _, err := target.exactMultiplier(); err != nil {
			return nil, err
		}
		if target.SubscriptionAllowed {
			if _, err := exactfactor.Resolve(target.SubscriptionFactorPPM, target.SubscriptionFactorPPMExact); err != nil {
				return nil, err
			}
		}
	}
	h := &hold{account: data.Account, amount: data.Amount, keys: keysFor(data.Account, req.ID), price: data.Price,
		multiplier: data.Multiplier, pricingInput: pricing.RequestInput{Body: req.Body, Headers: data.Headers, Now: data.Now},
		budgetAccount: data.BudgetAccount, cardMultiplier: data.CardMultiplier, cardChannels: data.CardChannels, cards: data.Cards, targetPrices: data.TargetPrices, sourceMode: data.SourceMode, sourceLimits: data.SourceLimits, fundingPreference: data.FundingPreference}
	seen := make(map[int64]bool)
	for _, p := range data.Funding {
		if p.Account <= 0 || p.Amount < 0 || seen[p.Account] {
			return nil, errors.New("billing: invalid task funding part")
		}
		seen[p.Account] = true
		h.funding = append(h.funding, fundingHold{account: p.Account, amount: p.Amount, keys: keysFor(p.Account, req.ID)})
	}
	if len(h.funding) > 0 && !seen[h.account] || h.budgetAccount > 0 && !seen[h.budgetAccount] {
		return nil, errors.New("billing: missing task funding account")
	}
	return h, nil
}

func frozenWorkflowTarget(h *hold, target gateway.Target) (gateway.Target, error) {
	if len(h.targetPrices) == 0 {
		return target, nil
	}
	prefix := targetPriceKey(gateway.Target{ChannelID: target.ChannelID, CredentialID: target.CredentialID})
	for key, price := range h.targetPrices {
		if strings.HasPrefix(key, prefix) {
			target.Group = price.Group
			target.MultiplierPPM, target.MultiplierPPMExact = price.MultiplierPPM, price.MultiplierPPMExact
			return target, nil
		}
	}
	return target, errors.New("billing: workflow target price missing")
}
