package ledger

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fundingLoader struct {
	sources []FundingSource
	err     error
}

func (l *fundingLoader) ActiveFundingSources(context.Context) ([]FundingSource, error) {
	return l.sources, l.err
}

func TestFundingProfilesOrderExpirationAndFailBeforeReady(t *testing.T) {
	now := time.Unix(1000, 0)
	loader := &fundingLoader{sources: []FundingSource{{UserID: 1, AccountID: 3, ExpiresAt: now.Add(2 * time.Hour)},
		{UserID: 1, AccountID: 2, ExpiresAt: now.Add(time.Hour)}, {UserID: 1, AccountID: 4, ExpiresAt: now}}}
	a := NewFundingAccounts(nil, loader, func() time.Time { return now })
	if _, err := a.SubscriptionAccounts(context.Background(), 1); err == nil {
		t.Fatal("unready profile returned wallet-only admission")
	}
	if err := a.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	ids, err := a.SubscriptionAccounts(context.Background(), 1)
	if err != nil || len(ids) != 2 || ids[0] != 2 || ids[1] != 3 {
		t.Fatalf("sources=%v %v", ids, err)
	}
	now = now.Add(time.Hour)
	ids, err = a.SubscriptionAccounts(context.Background(), 1)
	if err != nil || len(ids) != 1 || ids[0] != 3 {
		t.Fatalf("expired bucket used: %v %v", ids, err)
	}
	loader.err = errors.New("loader unavailable")
	if err := a.Refresh(context.Background()); err == nil {
		t.Fatal("refresh failure swallowed")
	}
}
