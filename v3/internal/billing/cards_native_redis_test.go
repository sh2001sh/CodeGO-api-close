//go:build pgintegration

package billing

import (
	"strconv"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func TestNativeConsumptionCardChargesAndAudits(t *testing.T) {
	for _, tc := range []struct {
		name string
		card catalog.MultiplierCard
		want int64
	}{
		{"native", catalog.MultiplierCard{ID: 82, MultiplierPPM: 500000}, 500},
		{"free_native", catalog.MultiplierCard{ID: 83, MultiplierPPM: 0}, 0},
		{"capped_native_excluded", catalog.MultiplierCard{ID: 84, MultiplierPPM: 500000, MaxDiscountMicro: 100}, 1000},
		{"missing_id_excluded", catalog.MultiplierCard{MultiplierPPM: 500000}, 1000},
		{"negative_multiplier_excluded", catalog.MultiplierCard{ID: 85, MultiplierPPM: -1}, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, rdb, _, clock := setup(t, 2000)
			snapshot := cardSnapshot(clock.now(), 1000, tc.card)
			s.snapshot = func() *catalog.Snapshot { return snapshot }
			req := cardRequest(tc.name)
			if err := s.Reserve(ctx, req); err != nil {
				t.Fatal(err)
			}
			if got := int64(req.Reserve.(*hold).amount); got != tc.want {
				t.Fatalf("admission charge=%d want=%d", got, tc.want)
			}
			for range 2 {
				if err := s.Finalize(ctx, req, completed(0, 0)); err != nil {
					t.Fatal(err)
				}
			}
			if bal, held := balance(t, rdb); bal != 2000-tc.want || held != 0 {
				t.Fatalf("balance=%d held=%d want=%d/0", bal, held, 2000-tc.want)
			}
			all := events(t, rdb)
			if len(all) != 1 || all[0][FieldAmount] != strconv.FormatInt(tc.want, 10) {
				t.Fatalf("charged event=%v", all)
			}
			event := all[0]
			if tc.want < 1000 {
				if event[FieldCardID] != strconv.FormatInt(tc.card.ID, 10) || event[FieldCardBefore] != "1000" || event[FieldCardAfter] != strconv.FormatInt(tc.want, 10) {
					t.Fatalf("native discount audit=%v", event)
				}
			} else if event[FieldCardID] != nil {
				t.Fatalf("ineligible native reward carried discount audit=%v", event)
			}
		})
	}
}
