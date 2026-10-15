package legacy

import "testing"

func TestFrozenCanonicalWalletExactApproval(t *testing.T) {
	t.Setenv("V3_MIGRATION_CANONICAL_WALLET_EXCEPTIONS", `[{"user_id":7,"snapshot":101,"projection":99}]`)
	snapshot := int64(101)
	base := Wallet{UserID: 7, ProjectionUnits: 99, SnapshotUnits: &snapshot}
	amount, issues := ValidateWallet(base)
	if amount != 202 || len(issues) != 0 {
		t.Fatalf("exact frozen snapshot not preserved: amount=%d issues=%v", amount, issues)
	}
	for _, change := range []struct {
		name string
		edit func(*Wallet)
	}{
		{"other user", func(w *Wallet) { w.UserID = 8 }},
		{"changed projection", func(w *Wallet) { w.ProjectionUnits = 98 }},
		{"changed snapshot", func(w *Wallet) { n := int64(102); w.SnapshotUnits = &n }},
		{"live reservation", func(w *Wallet) { w.ReservedUnits = 1 }},
	} {
		t.Run(change.name, func(t *testing.T) {
			w := base
			change.edit(&w)
			_, issues := ValidateWallet(w)
			if len(issues) == 0 {
				t.Fatal("changed frozen facts accepted")
			}
		})
	}
}

func TestFrozenCanonicalWalletAbsentOrMalformedApproval(t *testing.T) {
	for _, value := range []string{"", "[]", "invalid-json"} {
		t.Setenv("V3_MIGRATION_CANONICAL_WALLET_EXCEPTIONS", value)
		n := int64(101)
		_, issues := ValidateWallet(Wallet{UserID: 7, ProjectionUnits: 99, SnapshotUnits: &n})
		if len(issues) != 1 || issues[0].Code != "wallet_projection_mismatch" {
			t.Fatalf("unapproved mismatch accepted for %q: %v", value, issues)
		}
	}
}
