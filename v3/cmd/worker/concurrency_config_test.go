package main

import (
	"strings"
	"testing"
)

func TestWorkerLedgerConcurrencyConfiguration(t *testing.T) {
	for _, tc := range []struct {
		consumers, batch         string
		wantConsumers, wantBatch int
		invalidField             string
	}{
		{"", "", 1, 500, ""},
		{"2", "128", 2, 128, ""},
		{"32", "5000", 32, 5000, ""},
		{"0", "", 0, 0, "V3_LEDGER_CONSUMERS"},
		{"33", "", 0, 0, "V3_LEDGER_CONSUMERS"},
		{"two", "", 0, 0, "V3_LEDGER_CONSUMERS"},
		{"", "0", 0, 0, "V3_LEDGER_BATCH_SIZE"},
		{"", "5001", 0, 0, "V3_LEDGER_BATCH_SIZE"},
		{"", "100.5", 0, 0, "V3_LEDGER_BATCH_SIZE"},
	} {
		t.Run(tc.consumers+"/"+tc.batch, func(t *testing.T) {
			t.Setenv("V3_LEDGER_CONSUMERS", tc.consumers)
			t.Setenv("V3_LEDGER_BATCH_SIZE", tc.batch)
			cfg, err := loadWorkerConfig()
			if tc.invalidField != "" {
				if err == nil || !strings.Contains(err.Error(), tc.invalidField) {
					t.Fatalf("got %v, expected %s validation", err, tc.invalidField)
				}
				return
			}
			if err != nil || cfg.ledgerConsumers != tc.wantConsumers || cfg.ledgerBatchSize != tc.wantBatch {
				t.Fatalf("got (%+v, %v), want consumers=%d batch=%d", cfg, err, tc.wantConsumers, tc.wantBatch)
			}
		})
	}
}
