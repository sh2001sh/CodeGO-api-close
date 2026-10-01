//go:build pgintegration

package channelmarket_test

import (
	"errors"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
)

func TestZeroMarketFactorPreservesAbsoluteToolFeeAndRejectsNegativeReplay(t *testing.T) {
	f := setup(t)
	c := f.channel(t, "public")
	fields := map[string]string{
		billing.FieldRequestID: "absolute-tool-fee-zero-factor",
		billing.FieldModel:     "fixture-model", billing.FieldUserID: "2",
		billing.FieldChannelID: strconv.FormatInt(c.InternalChannelID, 10), billing.FieldAmount: "20",
		"marketplace_gross_micro": "20", "marketplace_multiplier_ppm": "0", "billing_source": "wallet",
	}
	post := func() error {
		return pgx.BeginFunc(ctx, f.pool, func(tx pgx.Tx) error { return f.s.AccrueUsageTx(ctx, tx, fields) })
	}
	if err := post(); err != nil {
		t.Fatal(err)
	}
	if err := post(); err != nil {
		t.Fatalf("unchanged replay: %v", err)
	}
	var factor, gross int64
	if err := f.pool.QueryRow(ctx, `SELECT multiplier_ppm,gross_micro FROM v3_channelmarket.settlements WHERE request_id=$1`, fields[billing.FieldRequestID]).Scan(&factor, &gross); err != nil {
		t.Fatal(err)
	}
	if factor != 0 || gross != 20 || f.balance(t, 1, "marketplace_pending") != 18 {
		t.Fatalf("zero factor/tool fee overwritten: factor=%d gross=%d", factor, gross)
	}
	fields["marketplace_multiplier_ppm"] = "1"
	if err := post(); !errors.Is(err, channelmarket.ErrConflict) {
		t.Fatalf("altered zero-factor replay accepted: %v", err)
	}
	fields["marketplace_multiplier_ppm"] = "-1"
	if err := post(); !errors.Is(err, channelmarket.ErrInvalid) {
		t.Fatalf("negative factor accepted: %v", err)
	}
}
