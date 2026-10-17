package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestWalletTransferRetryOnlyDefinitePostgresRollback(t *testing.T) {
	for _, test := range []struct {
		err   error
		retry bool
	}{
		{nil, false},
		{&pgconn.PgError{Code: "40P01"}, true},
		{fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: "40001"}), true},
		{&pgconn.PgError{Code: "23505"}, false},
		{&pgconn.PgError{Code: "55P03"}, false},
		{&pgconn.PgError{Code: "57014"}, false},
		{io.EOF, false},
		{errors.New("network error mentioning SQLSTATE 40P01"), false},
		{context.DeadlineExceeded, false},
	} {
		require.Equal(t, test.retry, retryableWalletTransferError(test.err), "error=%v", test.err)
	}
	db := setupRedemptionTestDB(t)
	attempts := 0
	err := runWalletTransferTransaction(context.Background(), db, func(*gorm.DB) error {
		attempts++
		return &pgconn.PgError{Code: "40P01"}
	})
	require.Error(t, err)
	require.Equal(t, 1, attempts, "a non-PostgreSQL transaction must never use the PostgreSQL retry policy")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = runWalletTransferTransaction(ctx, db, func(*gorm.DB) error {
		t.Fatal("a canceled context must not open a transaction")
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
}
