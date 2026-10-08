//go:build pgintegration

package incentives

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestRetiredLuckyNumberCannotBeIssuedByBlindBoxHook(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO v3_marketplace.blind_box_open_records(id,user_id,request_id,reward) VALUES(1,1,'first','{}')`); err != nil {
		t.Fatal(err)
	}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return s.IssueBlindBoxNumberTx(ctx, tx, 1, 1) })
	if !errors.Is(err, ErrRetired) {
		t.Fatalf("retired blind box hook: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v3_commerce.blind_box_daily_lucky_numbers`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("retired hook created numbers=%d err=%v", count, err)
	}
}
