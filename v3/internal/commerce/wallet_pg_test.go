//go:build pgintegration

package commerce_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
	"golang.org/x/crypto/bcrypt"
)

func walletFixture(t *testing.T) (*commerce.Service, *pgxpool.Pool, *time.Time) {
	t.Helper()
	pool := isolatedPool(t)
	now := time.Date(2026, 9, 30, 6, 0, 0, 0, time.UTC)
	hash, err := bcrypt.GenerateFromPassword([]byte("account-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(context.Background(), `UPDATE v3_identity.users SET external_id=CASE WHEN id=1 THEN 'SEND23' ELSE 'RECV23' END,display_name=CASE WHEN id=1 THEN 'Sender' ELSE 'Recipient' END,password_hash=$1,email=CASE WHEN id=1 THEN 'sender@example.test' ELSE 'recipient@example.test' END WHERE id IN(1,2)`, string(hash))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(context.Background(), `INSERT INTO v3_identity.users(id,username,external_id) VALUES(3,'stranger','THRD23')`); err != nil {
		t.Fatal(err)
	}
	s := commerce.New(pool, ledger.NewPoster(pool), nil, commerce.Config{Now: func() time.Time { return now }})
	walletSeed(t, pool, 1, 5_000_000)
	walletSeed(t, pool, 2, 5_000_000)
	return s, pool, &now
}

func walletSeed(t *testing.T, pool *pgxpool.Pool, uid int64, amount credits.Micro) {
	t.Helper()
	ctx := context.Background()
	var id int64
	err := pool.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',$1,'wallet') ON CONFLICT(owner_type,owner_id,kind) DO UPDATE SET owner_id=EXCLUDED.owner_id RETURNING id`, uid).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ledger.NewPoster(pool).Post(ctx, billing.Entry{AccountID: id, Amount: amount, Kind: "topup", OperationID: fmt.Sprintf("seed:%d:%d", uid, amount)}); err != nil {
		t.Fatal(err)
	}
}

func walletSetup(t *testing.T, s *commerce.Service, uid int64) {
	t.Helper()
	err := s.ConfigureWalletPassword(context.Background(), uid, commerce.WalletPasswordInput{CurrentPassword: "account-password", NewPaymentPassword: "payment123", ConfirmPassword: "payment123"})
	if err != nil {
		t.Fatal(err)
	}
}

func walletRequest(id string) commerce.WalletTransferInput {
	return commerce.WalletTransferInput{RecipientExternalID: "RECV23", Amount: 1_000_000, PaymentPassword: "payment123", RequestID: id}
}

func walletBalances(t *testing.T, pool *pgxpool.Pool) (int64, int64, int64) {
	t.Helper()
	var sender, receiver, fee int64
	err := pool.QueryRow(context.Background(), `SELECT coalesce(sum(balance) FILTER(WHERE owner_type='user' AND owner_id=1),0),coalesce(sum(balance) FILTER(WHERE owner_type='user' AND owner_id=2),0),coalesce(sum(balance) FILTER(WHERE owner_type='platform'),0) FROM v3_billing.accounts`).Scan(&sender, &receiver, &fee)
	if err != nil {
		t.Fatal(err)
	}
	return sender, receiver, fee
}

func TestWalletConcurrentReplayCreditsOnceAndScopesHistory(t *testing.T) {
	s, pool, _ := walletFixture(t)
	ctx := context.Background()
	walletSetup(t, s, 1)
	var wg sync.WaitGroup
	fail := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			item, err := s.CreateWalletTransfer(ctx, 1, walletRequest("replay"))
			if err == nil && (item.Amount != 1_000_000 || item.Fee != 10_000 || item.TotalDebit != 1_010_000 || item.BalanceAfter != 3_990_000) {
				err = fmt.Errorf("incorrect transfer: %+v", item)
			}
			fail <- err
		}()
	}
	wg.Wait()
	close(fail)
	for err := range fail {
		if err != nil {
			t.Fatal(err)
		}
	}
	sender, receiver, fee := walletBalances(t, pool)
	if sender != 3_990_000 || receiver != 6_000_000 || fee != 10_000 {
		t.Fatalf("balances %d %d %d", sender, receiver, fee)
	}
	var count, entries, outbox int64
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM v3_commerce.wallet_transfers),(SELECT count(*) FROM v3_billing.ledger_entries WHERE kind='transfer'),(SELECT count(*) FROM v3_billing.balance_outbox WHERE operation_id LIKE 'wallet-transfer:%')`).Scan(&count, &entries, &outbox); err != nil {
		t.Fatal(err)
	}
	if count != 1 || entries != 3 || outbox != 3 {
		t.Fatalf("non-atomic or repeated transfer %d %d %d", count, entries, outbox)
	}
	for _, uid := range []int64{1, 2, 3} {
		history, err := s.WalletTransfers(ctx, uid, 1, 10)
		if err != nil {
			t.Fatal(err)
		}
		if uid == 3 {
			if history.Total != 0 || len(history.Items) != 0 {
				t.Fatal("foreign transfer exposed")
			}
			continue
		}
		if history.Total != 1 || len(history.Items) != 1 {
			t.Fatalf("owned history %+v", history)
		}
		want := "outgoing"
		if uid == 2 {
			want = "incoming"
		}
		if history.Items[0].Direction != want {
			t.Fatalf("direction %+v", history.Items[0])
		}
	}
	bad := walletRequest("replay")
	bad.Amount = 2_000_000
	if _, err := s.CreateWalletTransfer(ctx, 1, bad); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("changed replay accepted: %v", err)
	}
	walletSetup(t, s, 2)
	bad = walletRequest("replay")
	bad.RecipientExternalID = "SEND23"
	if _, err := s.CreateWalletTransfer(ctx, 2, bad); !errors.Is(err, commerce.ErrStateConflict) {
		t.Fatalf("cross-sender replay accepted: %v", err)
	}
}

func TestWalletInsufficientSelfInactiveAndOverflowRollback(t *testing.T) {
	s, pool, _ := walletFixture(t)
	ctx := context.Background()
	walletSetup(t, s, 1)
	bad := walletRequest("insufficient")
	bad.Amount = 5_000_000
	if _, err := s.CreateWalletTransfer(ctx, 1, bad); !errors.Is(err, commerce.ErrWalletInsufficient) {
		t.Fatalf("insufficient accepted %v", err)
	}
	bad = walletRequest("self")
	bad.RecipientExternalID = "SEND23"
	if _, err := s.CreateWalletTransfer(ctx, 1, bad); !errors.Is(err, commerce.ErrWalletSelf) {
		t.Fatalf("self transfer %v", err)
	}
	if _, err := s.WalletRecipient(ctx, 1, "recv23"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_identity.users SET status='disabled' WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateWalletTransfer(ctx, 1, walletRequest("disabled")); !errors.Is(err, commerce.ErrNotFound) {
		t.Fatalf("disabled recipient %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_identity.users SET status='active' WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	walletSeed(t, pool, 2, credits.Micro(math.MaxInt64-5_000_000))
	if _, err := s.CreateWalletTransfer(ctx, 1, walletRequest("overflow")); !errors.Is(err, credits.ErrOverflow) {
		t.Fatalf("overflow recipient %v", err)
	}
	sender, receiver, fee := walletBalances(t, pool)
	if sender != 5_000_000 || receiver != math.MaxInt64 || fee != 0 {
		t.Fatalf("rollback balances %d %d %d", sender, receiver, fee)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.wallet_transfers`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed transaction history %d %v", count, err)
	}
}

func TestWalletPaymentPasswordAttemptsLockAndRotation(t *testing.T) {
	s, pool, now := walletFixture(t)
	ctx := context.Background()
	if _, err := s.CreateWalletTransfer(ctx, 1, walletRequest("unset")); !errors.Is(err, commerce.ErrWalletPasswordNotSet) {
		t.Fatalf("unset password %v", err)
	}
	setup := commerce.WalletPasswordInput{CurrentPassword: "wrong", NewPaymentPassword: "payment123", ConfirmPassword: "payment123"}
	if err := s.ConfigureWalletPassword(ctx, 1, setup); !errors.Is(err, commerce.ErrWalletAccountPassword) {
		t.Fatalf("first setup bypass %v", err)
	}
	walletSetup(t, s, 1)
	var hash string
	if err := pool.QueryRow(ctx, `SELECT password_hash FROM v3_commerce.wallet_transfer_security WHERE user_id=1`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash == "payment123" || bcrypt.CompareHashAndPassword([]byte(hash), []byte("payment123")) != nil {
		t.Fatal("credential was not hashed")
	}
	bad := walletRequest("wrong-password")
	bad.PaymentPassword = "incorrect"
	for i := 1; i <= 5; i++ {
		_, err := s.CreateWalletTransfer(ctx, 1, bad)
		want := commerce.ErrWalletPasswordWrong
		if i == 5 {
			want = commerce.ErrWalletPasswordLocked
		}
		if !errors.Is(err, want) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if _, err := s.CreateWalletTransfer(ctx, 1, walletRequest("locked")); !errors.Is(err, commerce.ErrWalletPasswordLocked) {
		t.Fatalf("lock bypass %v", err)
	}
	overview, err := s.WalletOverview(ctx, 1, 1, 10)
	if err != nil || overview.Security.RemainingPasswordAttempts != 0 || overview.Security.LockedUntil != now.Add(30*time.Minute).Unix() {
		t.Fatalf("lock overview %+v %v", overview.Security, err)
	}
	*now = now.Add(30*time.Minute + time.Second)
	change := commerce.WalletPasswordInput{VerificationMethod: "payment_password", OldPaymentPassword: "payment123", NewPaymentPassword: "replacement123", ConfirmPassword: "replacement123"}
	if err = s.ConfigureWalletPassword(ctx, 1, change); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateWalletTransfer(ctx, 1, walletRequest("old")); !errors.Is(err, commerce.ErrWalletPasswordWrong) {
		t.Fatalf("old password accepted %v", err)
	}
	good := walletRequest("new")
	good.PaymentPassword = "replacement123"
	if _, err = s.CreateWalletTransfer(ctx, 1, good); err != nil {
		t.Fatal(err)
	}
	change.VerificationMethod = "email"
	change.EmailCode = "unverified"
	if err = s.ConfigureWalletPassword(ctx, 1, change); !errors.Is(err, commerce.ErrWalletEmailUnavailable) {
		t.Fatalf("unverified reset accepted %v", err)
	}
}

func TestWalletOppositeDirectionTransfersConserveMoney(t *testing.T) {
	s, pool, _ := walletFixture(t)
	walletSetup(t, s, 1)
	walletSetup(t, s, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	fail := make(chan error, 8)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			uid := int64(1 + i%2)
			in := walletRequest(fmt.Sprintf("opposite:%d", i))
			in.Amount = 10_000
			if uid == 2 {
				in.RecipientExternalID = "SEND23"
			}
			_, err := s.CreateWalletTransfer(ctx, uid, in)
			fail <- err
		}()
	}
	wg.Wait()
	close(fail)
	for err := range fail {
		if err != nil {
			t.Fatal(err)
		}
	}
	sender, receiver, fee := walletBalances(t, pool)
	if sender != 4_999_600 || receiver != 4_999_600 || fee != 800 || sender+receiver+fee != 10_000_000 {
		t.Fatalf("money changed %d %d %d", sender, receiver, fee)
	}
}
