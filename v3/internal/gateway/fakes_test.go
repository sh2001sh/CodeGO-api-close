package gateway_test

import (
	"context"
	"sync"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type fakeAuth struct{}

func (fakeAuth) Authorize(_ context.Context, key string) (gateway.Principal, error) {
	if key != "sk-test" {
		return gateway.Principal{}, gateway.ErrInvalidKey
	}
	return gateway.Principal{UserID: 7, KeyID: 70, Group: "default"}, nil
}

// fakePlanner returns fixed targets and records feedback.
type fakePlanner struct {
	targets []gateway.Target
	mu      sync.Mutex
	reports []gateway.AttemptResult
}

func (p *fakePlanner) Plan(context.Context, *gateway.Request) ([]gateway.Target, error) {
	return append([]gateway.Target(nil), p.targets...), nil
}

func (p *fakePlanner) Report(_ gateway.Target, r gateway.AttemptResult) {
	p.mu.Lock()
	p.reports = append(p.reports, r)
	p.mu.Unlock()
}

func (p *fakePlanner) results() []gateway.AttemptResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]gateway.AttemptResult(nil), p.reports...)
}

// fakeSettler records reservations and delivers each Outcome on a channel.
type fakeSettler struct {
	insufficient bool
	reserved     int
	outcomes     chan gateway.Outcome
	mu           sync.Mutex
}

func newSettler() *fakeSettler { return &fakeSettler{outcomes: make(chan gateway.Outcome, 4)} }

func (s *fakeSettler) Reserve(context.Context, *gateway.Request) error {
	if s.insufficient {
		return gateway.ErrInsufficientCredits
	}
	s.mu.Lock()
	s.reserved++
	s.mu.Unlock()
	return nil
}

func (s *fakeSettler) Finalize(_ context.Context, _ *gateway.Request, out gateway.Outcome) error {
	s.outcomes <- out
	return nil
}
