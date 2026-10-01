//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/commerce"
)

func TestWalletEmailRecoveryCanResetLockedPaymentCredential(t *testing.T) {
	s, recovery, capture, pool, _ := walletEmailFixture(t)
	ctx := context.Background()
	wrong := walletRequest("lock-before-recovery")
	wrong.PaymentPassword = "incorrect"
	for i := 1; i <= 5; i++ {
		_, err := s.CreateWalletTransfer(ctx, 1, wrong)
		want := commerce.ErrWalletPasswordWrong
		if i == 5 {
			want = commerce.ErrWalletPasswordLocked
		}
		if !errors.Is(err, want) {
			t.Fatalf("payment attempt %d: %v", i, err)
		}
	}
	if _, err := recovery.Send(ctx, 1); err != nil {
		t.Fatal(err)
	}
	_, code, _ := capture.latest()
	if err := s.ConfigureWalletPassword(ctx, 1, walletEmailReset(code)); err != nil {
		t.Fatal(err)
	}
	var locked bool
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT locked_until IS NOT NULL,failed_attempts FROM v3_commerce.wallet_transfer_security WHERE user_id=1`).Scan(&locked, &attempts); err != nil || locked || attempts != 0 {
		t.Fatalf("verified recovery did not clear payment lock %v %d %v", locked, attempts, err)
	}
	good := walletRequest("after-recovery")
	good.PaymentPassword = "recovered123"
	if _, err := s.CreateWalletTransfer(ctx, 1, good); err != nil {
		t.Fatal(err)
	}
}

func TestWalletEmailRecoveryPendingDeliveryCannotAuthorizeReset(t *testing.T) {
	s, recovery, capture, _, _ := walletEmailFixture(t)
	ctx := context.Background()
	started := make(chan struct{})
	release := make(chan struct{})
	result := make(chan error, 1)
	capture.hook = func() error { close(started); <-release; return nil }
	go func() { _, err := recovery.Send(ctx, 1); result <- err }()
	<-started
	_, code, _ := capture.latest()
	err := s.ConfigureWalletPassword(ctx, 1, walletEmailReset(code))
	close(release)
	if !errors.Is(err, commerce.ErrWalletEmailCodeInvalid) {
		t.Fatalf("pending delivery authorized recovery %v", err)
	}
	if err = <-result; err != nil {
		t.Fatal(err)
	}
	if err = s.ConfigureWalletPassword(ctx, 1, walletEmailReset(code)); err != nil {
		t.Fatal(err)
	}
}
