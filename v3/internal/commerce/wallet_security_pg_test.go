//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestWalletFirstPasswordSetupIsBoundedAndPasswordlessSetupWorks(t *testing.T) {
	s, pool, now := walletFixture(t)
	ctx := context.Background()
	setup := commerce.WalletPasswordInput{CurrentPassword: "wrong", NewPaymentPassword: "payment123", ConfirmPassword: "payment123"}
	for i := 1; i <= 5; i++ {
		want := commerce.ErrWalletAccountPassword
		if i == 5 {
			want = commerce.ErrWalletPasswordLocked
		}
		if err := s.ConfigureWalletPassword(ctx, 1, setup); !errors.Is(err, want) {
			t.Fatalf("first-setup attempt %d: %v", i, err)
		}
	}
	setup.CurrentPassword = "account-password"
	if err := s.ConfigureWalletPassword(ctx, 1, setup); !errors.Is(err, commerce.ErrWalletPasswordLocked) {
		t.Fatalf("setup lock bypass %v", err)
	}
	overview, err := s.WalletOverview(ctx, 1, 1, 10)
	if err != nil || overview.Security.PasswordSet {
		t.Fatalf("failed setup created password: %+v %v", overview.Security, err)
	}
	*now = now.Add(30*time.Minute + time.Second)
	if err = s.ConfigureWalletPassword(ctx, 1, setup); err != nil {
		t.Fatal(err)
	}
	setup.CurrentPassword = ""
	if err = s.ConfigureWalletPassword(ctx, 3, setup); err != nil {
		t.Fatalf("passwordless first setup: %v", err)
	}
	// Existing payment credentials cannot be overwritten by a passwordless
	// account or by presenting only the login credential.
	if err = s.ConfigureWalletPassword(ctx, 3, setup); !errors.Is(err, commerce.ErrInvalid) {
		t.Fatalf("passwordless credential overwrite: %v", err)
	}
	setup.VerificationMethod = "payment_password"
	if err = s.ConfigureWalletPassword(ctx, 3, setup); !errors.Is(err, commerce.ErrWalletPasswordWrong) {
		t.Fatalf("empty old credential accepted: %v", err)
	}
	var failures int
	if err = pool.QueryRow(ctx, `SELECT failed_attempts FROM v3_commerce.wallet_transfer_security WHERE user_id=3`).Scan(&failures); err != nil || failures != 1 {
		t.Fatalf("failed rotation was rolled back: %d %v", failures, err)
	}
}
