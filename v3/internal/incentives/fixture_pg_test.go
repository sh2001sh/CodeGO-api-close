//go:build pgintegration

package incentives

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/migrations"
)

func fixture(t *testing.T) (*Service, *pgxpool.Pool, *time.Time) {
	t.Helper()
	return fixtureConfigured(t, true)
}

func fixtureConfigured(t *testing.T, alignCutover bool) (*Service, *pgxpool.Pool, *time.Time) {
	t.Helper()
	dsn := os.Getenv("V3_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("incentives_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize())
		_ = admin.Close(ctx)
	})
	files, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		sql, err := migrations.Read(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, sql); err != nil {
			t.Fatalf("migration %s: %v", f, err)
		}
	}
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	// Most tests use a fixed historical clock. Migration-cutover tests retain
	// the actual persisted timestamp and use the production clock instead.
	if alignCutover {
		if _, err = pool.Exec(ctx, `UPDATE v3_commerce.referral_consumption_policy SET effective_at=$1 WHERE id`, now); err != nil {
			t.Fatal(err)
		}
	}
	s := New(pool, ledger.NewPoster(pool), Config{Now: func() time.Time { return now }})
	if _, err = pool.Exec(ctx, `INSERT INTO v3_identity.users(id,username,aff_code) VALUES(1,'inviter','abc'),(2,'invitee','def'),(3,'unrelated','xyz'); UPDATE v3_identity.users SET inviter_id=1 WHERE id=2;
 INSERT INTO v3_commerce.plans(id,name,price_minor,credits,period_seconds,plan_type,lucky_draw_enabled,membership_tier) VALUES(1,'month',100,1000000,2592000,'monthly',true,'pro'),(2,'day',10,100000,86400,'day_pass',false,'none')`); err != nil {
		t.Fatal(err)
	}
	return s, pool, &now
}
func seedDraw(t *testing.T, s *Service, amount int64) {
	t.Helper()
	ctx := context.Background()
	_, err := s.pool.Exec(ctx, `INSERT INTO v3_commerce.subscriptions(id,user_id,plan_id,starts_at,expires_at,total_credits,renewable_credits) VALUES(1,1,1,$1::timestamptz,$1::timestamptz+interval '30 days',1000000,1000000)`, s.now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO v3_commerce.subscription_lucky_draws(id,draw_date,winning_number,jackpot_before,jackpot_after,status,timezone,draw_hour,draw_minute,base_reward_1_usd,base_reward_2_usd,base_reward_3_usd,base_reward_4_usd,multiplier_lite,multiplier_standard,multiplier_pro,multiplier_ultra,jackpot_initial_usd,jackpot_increment_usd,jackpot_cap_usd,cost_per_usd,monthly_budget_usd,drawn_at)
 VALUES(100,'2026-09-30','1234',25,30,'settling','Asia/Shanghai',20,0,0.25,2.5,12.5,25,1,1.1,1.2,1.3,25,5,250,0.1,0,$1)`, s.now())
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO v3_commerce.subscription_lucky_rewards(id,draw_id,subscription_id,participation_type,user_id,lucky_number,membership_tier,matched_digits,base_reward_usd,tier_multiplier,jackpot_reward_usd,final_reward_credits,credit_status) VALUES(100,100,1,'subscription',1,'0034','pro',2,2.5,1.2,0,$1,'pending')`, amount)
	if err != nil {
		t.Fatal(err)
	}
}
