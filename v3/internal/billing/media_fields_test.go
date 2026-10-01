package billing

import (
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// Regression: durable events carry media dimensions for both wallet-only and
// split funding settlements, so WAL/ledger replay preserves actual units.
func TestFinalizeEventsPreserveMediaUnits(t *testing.T) {
	s := &Settler{cfg: Config{Now: func() time.Time { return time.Unix(1, 0) }}}
	h := &hold{account: 7, keys: keysFor(7, "media"), funding: []fundingHold{{account: 7, keys: keysFor(7, "media")}}}
	req := &gateway.Request{ID: "media", Model: "media-model"}
	out := gateway.Outcome{Terminal: gateway.TerminalCompleted, Usage: gateway.Usage{
		ImageCount: 2, AudioDurationMicros: 9_007_199_254_740_993, AudioCharacters: 4, VideoDurationMicros: 5_000_001,
	}}
	funded, err := s.fundingFinalizeCall(req, out, h, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range []walRecord{s.finalizeCall(req, out, h, 10), funded} {
		fields := make(map[string]string)
		for index, value := range rec.Args {
			if index+1 < len(rec.Args) {
				fields[value] = rec.Args[index+1]
			}
		}
		for key, value := range map[string]string{
			FieldImageCount: "2", FieldAudioDurationMicros: "9007199254740993", FieldAudioCharacters: "4", FieldVideoDurationMicros: "5000001",
		} {
			if fields[key] != value {
				t.Fatalf("event %s = %q; want %q", key, fields[key], value)
			}
		}
	}
}
