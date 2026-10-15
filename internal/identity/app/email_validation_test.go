package app

import (
	"errors"
	"testing"
)

func TestValidateRegistrationEmailRejectsAliases(t *testing.T) {
	for _, email := range []string{"user+batch@example.com", "user.name@example.com"} {
		if !errors.Is(validateRegistrationEmail(email), ErrEmailAliasNotAllowed) {
			t.Fatalf("expected alias email %q to be rejected", email)
		}
	}
}

func TestValidateRegistrationEmailAcceptsOrdinaryAddress(t *testing.T) {
	if err := validateRegistrationEmail("user@example.com"); err != nil {
		t.Fatalf("expected ordinary email to be accepted: %v", err)
	}
}
