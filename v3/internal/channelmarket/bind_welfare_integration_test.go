//go:build pgintegration

package channelmarket_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestBindingAndWelfareEnforceOwnedBoundaryBeforeDependency(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "private")
	f.active(t, c)
	var issued, transfers, gifts int
	s := channelmarket.New(f.pool, nil, f.poster, channelmarket.Config{
		IssueKey: func(_ context.Context, user int64, group string) (channelmarket.BoundToken, error) {
			issued++
			if user != 1 || !strings.HasPrefix(group, "market_") {
				t.Fatal("wrong identity key target")
			}
			return channelmarket.BoundToken{TokenID: 55, TokenGroup: group, APIKey: "one-time-user-key"}, nil
		},
		WelfareTransfer: func(_ context.Context, user int64, external string, amount int64, password, op string) error {
			transfers++
			if user != 1 || external != "USR2" || amount != 20 || password != "fixture-payment" || !strings.Contains(op, "market-welfare:1:") {
				t.Fatal("wrong transfer boundary")
			}
			return errors.New("insufficient sender funds")
		},
		GiftBoxes: func(_ context.Context, sender, recipient int64, request string, count int) ([]int64, error) {
			gifts++
			if sender != 1 || recipient != 2 || count != 1 || !strings.Contains(request, "market-welfare:1:") {
				t.Fatal("wrong gift boundary")
			}
			return nil, errors.New("no sender inventory")
		},
	}, nil)
	if _, err := f.pool.Exec(ctx, `UPDATE v3_identity.users SET external_id='USR2' WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BindGroup(ctx, 2, c.GroupID, 0); !errors.Is(err, channelmarket.ErrNotFound) || issued != 0 {
		t.Fatalf("private key issued %v %d", err, issued)
	}
	if out, err := s.BindGroup(ctx, 1, c.GroupID, 0); err != nil || out.TokenID != 55 || out.APIKey == "" || issued != 1 {
		t.Fatalf("owner key %+v %v", out, err)
	}
	p, err := f.s.SavePool(ctx, 1, channelmarket.RoutePool{Name: "Owned", Members: []channelmarket.PoolMember{{GroupID: c.GroupID}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.BindRoutePool(ctx, 2, p.ID, 0); !errors.Is(err, channelmarket.ErrNotFound) || issued != 1 {
		t.Fatalf("foreign pool key %v", err)
	}
	r := channelmarket.WelfareRequest{UserIDs: []string{"2"}, Type: "transfer", Amount: 10, PaymentPassword: "fixture-payment", OperationID: "test-transfer"}
	if _, err = s.Welfare(ctx, channelmarket.Actor{UserID: 3}, c.InternalChannelID, r); !errors.Is(err, channelmarket.ErrNotFound) || transfers != 0 {
		t.Fatalf("foreign welfare %v", err)
	}
	if out, err := s.Welfare(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID, r); err != nil || out.Failed != 1 || out.Success != 0 || transfers != 1 {
		t.Fatalf("unbacked transfer %+v %v", out, err)
	}
	r.Type = "blind_box"
	r.Amount = 1
	if out, err := s.Welfare(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID, r); err != nil || out.Failed != 1 || out.Success != 0 || gifts != 1 {
		t.Fatalf("invented inventory %+v %v", out, err)
	}
	r.UserIDs = []string{"2", " 2 "}
	if _, err = s.Welfare(ctx, channelmarket.Actor{UserID: 1}, c.InternalChannelID, r); !errors.Is(err, channelmarket.ErrInvalid) || gifts != 1 {
		t.Fatalf("duplicated welfare %v", err)
	}
}
