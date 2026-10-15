package legacy

import "testing"

func TestAmbiguousRecoveryMailCannotChooseAnotherAccount(t *testing.T) {
	deleted := "2026-01-01"
	users := []sourceUser{
		{ID: 1, Email: "Shared@example.com", Username: "first", Password: "first-hash", WalletUnits: 123},
		{ID: 2, Email: " shared@example.com ", Username: "second", Password: "second-hash", WalletUnits: 456},
		{ID: 3, Email: "SHARED@example.com", DeletedAt: &deleted},
		{ID: 4, Email: "unique@example.com"},
	}
	preserveAmbiguousEmails(users)
	if users[0].Email != "" || users[1].Email != "" || !users[0].EmailPreservedInSource || !users[1].EmailPreservedInSource {
		t.Fatal("ambiguous mailbox retained as a recovery identity")
	}
	if users[0].Username != "first" || users[0].Password != "first-hash" || users[0].WalletUnits != 123 || users[1].WalletUnits != 456 {
		t.Fatal("login identity or money changed")
	}
	if users[2].Email != "SHARED@example.com" || users[3].Email != "unique@example.com" {
		t.Fatal("nonconflicting or deleted address changed")
	}
}
