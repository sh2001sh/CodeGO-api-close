package ledger

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type orderedUsageRecorder struct{ calls []string }

func (r *orderedUsageRecorder) RecordUsageTx(_ context.Context, _ pgx.Tx, _ int64, request string, _ credits.Micro) error {
	r.calls = append(r.calls, "usage:"+request)
	return nil
}
func (r *orderedUsageRecorder) RecordDiscountUsageTx(_ context.Context, _ pgx.Tx, _, _, _ int64, request string, _, _ credits.Micro) error {
	r.calls = append(r.calls, "discount:"+request)
	return nil
}

func progressOrderEvent(user, request string, card bool) event {
	fields := map[string]string{billing.FieldUserID: user, billing.FieldRequestID: request, billing.FieldAmount: "0", billing.FieldModel: "model"}
	if card {
		fields[billing.FieldCardID], fields[billing.FieldCardBefore], fields[billing.FieldCardAfter], fields[billing.FieldChannelID] = "3", "100", "0", "9"
	}
	return event{requestID: request, fields: fields}
}

func TestMarketplaceCallbacksSortNumericUsersAndPreserveSameUserOrder(t *testing.T) {
	batch := []event{progressOrderEvent("10", "ten-first", false), progressOrderEvent("2", "two-first", true), progressOrderEvent("10", "ten-second", true), progressOrderEvent("2", "two-second", false)}
	recorder := &orderedUsageRecorder{}
	if err := recordMarketplaceAndSubscriptionUsage(context.Background(), nil, recorder, batch); err != nil {
		t.Fatal(err)
	}
	want := []string{"usage:two-first", "discount:two-first", "usage:two-second", "usage:ten-first", "usage:ten-second", "discount:ten-second"}
	if !reflect.DeepEqual(recorder.calls, want) {
		t.Fatalf("callbacks=%v want=%v", recorder.calls, want)
	}
	var original []string
	for _, e := range batch {
		original = append(original, e.requestID)
	}
	if !reflect.DeepEqual(original, []string{"ten-first", "two-first", "ten-second", "two-second"}) {
		t.Fatalf("financial event slice reordered: %v", original)
	}
}

func TestMarketplaceUserOrderingPreservesInvalidAndSkippedTelemetry(t *testing.T) {
	for _, invalid := range []string{"", "0", "-1", "wrong", "9223372036854775808"} {
		t.Run(invalid, func(t *testing.T) {
			err := recordMarketplaceAndSubscriptionUsage(context.Background(), nil, &orderedUsageRecorder{}, []event{progressOrderEvent(invalid, "invalid-user", false)})
			if err == nil || !strings.Contains(err.Error(), "ledger: invalid usage user") {
				t.Fatalf("invalid user %q error=%v", invalid, err)
			}
		})
	}
	secondary := progressOrderEvent("wrong", "secondary", false)
	secondary.fields["funding_part"] = "secondary"
	modelLess := progressOrderEvent("wrong", "model-less", false)
	modelLess.fields[billing.FieldModel] = ""
	recorder := &orderedUsageRecorder{}
	if err := recordMarketplaceAndSubscriptionUsage(context.Background(), nil, recorder, []event{secondary, modelLess}); err != nil || len(recorder.calls) != 0 {
		t.Fatalf("skipped telemetry=%v calls=%v", err, recorder.calls)
	}
	if err := recordMarketplaceAndSubscriptionUsage(context.Background(), nil, nil, []event{progressOrderEvent("wrong", "disabled-recorder", false)}); err != nil {
		t.Fatalf("disabled recorder newly rejected unused telemetry: %v", err)
	}
}
