package main

import "testing"

func TestMigrationLedgerArchiveMode(t *testing.T) {
	for _, tc := range []struct {
		mode    string
		archive bool
		invalid bool
	}{{"", false, false}, {"copy", false, false}, {"archive", true, false}, {"skip", false, true}, {"ARCHIVE", false, true}} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Setenv("V3_MIGRATION_LEDGER_HISTORY", tc.mode)
			archive, err := migrationLedgerArchive()
			if (err != nil) != tc.invalid || archive != tc.archive {
				t.Fatalf("mode=%q archive=%v error=%v", tc.mode, archive, err)
			}
		})
	}
}
