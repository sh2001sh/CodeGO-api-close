package billing

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// FundingResolver supplies active subscription buckets, earliest-expiry first,
// from an account profile cache. WalletAccount remains the final source. Its
// implementation must not query PostgreSQL on the request path.
type FundingResolver interface {
	SubscriptionAccounts(context.Context, int64) ([]int64, error)
}

type FundingSource struct {
	UserID, AccountID int64
	ExpiresAt         time.Time
}

type fundingHold struct {
	account int64
	amount  credits.Micro
	keys    keys
}

func fundingKeys(parts []fundingHold, wallet keys) []string {
	result := []string{wallet.done, redisx.KeyReservationOpen, redisx.StreamBillingEvents}
	for _, p := range parts {
		result = append(result, p.keys.balance, p.keys.reservation, p.keys.done, p.keys.holds)
	}
	return result
}

func (s *Settler) reserveFunding(ctx context.Context, req *gateway.Request, h *hold, sources []int64) error {
	if s.wal != nil && s.br.open() {
		return ErrBillingDegraded
	}
	budgetIndex, err := s.buildFundingHolds(req, h, sources)
	if err != nil {
		return err
	}
	lifetime := s.reservationLifetime(ctx)
	args := []any{int64(h.amount), int64(s.cfg.OverdraftAllowance), s.cfg.Now().Add(lifetime).UnixMilli(),
		(lifetime + reservationGrace).Milliseconds(), req.ID}
	for _, p := range h.funding {
		args = append(args, p.account)
	}
	args = append(args, budgetIndex)
	if h.sourceMode {
		args, err = s.sourceReserveArgs(ctx, req, h, budgetIndex)
		if err != nil {
			return fmt.Errorf("%w: source quote: %v", gateway.ErrBillingUnavailable, err)
		}
	}
	return s.runFundingReserve(ctx, req, h, args)
}

// buildFundingHolds populates h.funding with the caller's wallet account,
// the funding sources (deduplicated against the wallet), and the API key's
// budget account if budget-limited. It returns the 1-based index of the
// budget hold within h.funding, or 0 if there is none.
func (s *Settler) buildFundingHolds(req *gateway.Request, h *hold, sources []int64) (budgetIndex int, err error) {
	seen := map[int64]bool{h.account: true}
	for _, id := range sources {
		if id <= 0 {
			return 0, fmt.Errorf("billing: invalid funding account %d", id)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		h.funding = append(h.funding, fundingHold{account: id, keys: keysFor(id, req.ID)})
	}
	h.funding = append(h.funding, fundingHold{account: h.account, keys: h.keys})
	if req.Principal.BudgetLimited {
		id := req.Principal.BudgetAccountID
		if id <= 0 || seen[id] {
			return 0, fmt.Errorf("%w: missing or invalid API key budget account", gateway.ErrBillingUnavailable)
		}
		h.budgetAccount = id
		h.funding = append(h.funding, fundingHold{account: id, keys: keysFor(id, req.ID)})
		budgetIndex = len(h.funding)
	}
	return budgetIndex, nil
}

// runFundingReserve runs the funding reserve script, retrying once per
// funding account to install any balance Redis hasn't loaded yet.
func (s *Settler) runFundingReserve(ctx context.Context, req *gateway.Request, h *hold, args []any) error {
	for retries := 0; retries <= len(h.funding); retries++ {
		callCtx, cancel := context.WithTimeout(ctx, s.cfg.RedisTimeout)
		result, err := fundingReserveScript.Run(callCtx, s.rdb, fundingKeys(h.funding, h.keys), args...).Int64Slice()
		cancel()
		if err != nil {
			if isOutage(ctx, err) {
				s.br.fail()
			}
			return s.rejectReserve(ctx, req, h, err)
		}
		code := result[0]
		if code == codeNotLoaded {
			index := int(result[1])
			if index < 1 || index > len(h.funding) {
				return fmt.Errorf("billing: invalid missing source index")
			}
			p := h.funding[index-1]
			if err := s.installBalance(ctx, p.account, p.keys); err != nil {
				return err
			}
			continue
		}
		if code == codeInsufficient {
			return gateway.ErrInsufficientCredits
		}
		if code != codeReserved {
			return fmt.Errorf("billing: funding reserve returned %d", code)
		}
		for i := range h.funding {
			h.funding[i].amount = credits.Micro(result[i+1])
		}
		if h.sourceMode {
			h.amount = credits.Micro(result[len(h.funding)+1])
		}
		req.Reserve = h
		s.br.ok()
		return nil
	}
	return fmt.Errorf("%w: funding balances failed to load", gateway.ErrBillingUnavailable)
}

func (s *Settler) fundingFinalizeCall(req *gateway.Request, out gateway.Outcome, h *hold, actual credits.Micro) (walRecord, error) {
	if h.sourceMode {
		return s.sourceFinalizeCall(req, out, h)
	}
	rec := s.finalizeCall(req, out, h, actual)
	args := []string{"funding", strconv.FormatInt(int64(actual), 10), strconv.FormatInt(int64(s.cfg.OverdraftCap), 10),
		strconv.FormatInt(s.cfg.DoneTTL.Milliseconds(), 10), req.ID, strconv.Itoa(len(h.funding))}
	for _, p := range h.funding {
		args = append(args, strconv.FormatInt(p.account, 10), strconv.FormatInt(int64(p.amount), 10))
	}
	args = append(args, rec.Args[4:]...)
	if h.budgetAccount > 0 {
		args = append(args, "budget_account_id", strconv.FormatInt(h.budgetAccount, 10))
	}
	return walRecord{Keys: fundingKeys(h.funding, h.keys), Args: args}, nil
}
