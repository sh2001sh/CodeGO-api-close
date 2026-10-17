package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type fundingSQLCounter struct {
	logger.Interface
	count     atomic.Int64
	fallbacks atomic.Int64
	physical  atomic.Value
}

func (c *fundingSQLCounter) Trace(_ context.Context, _ time.Time, query func() (string, int64), _ error) {
	c.count.Add(1)
	sql, _ := query()
	if strings.HasPrefix(sql, "ROLLBACK TO SAVEPOINT funding_fifo_ctid_batch") {
		c.fallbacks.Add(1)
	}
	if strings.HasPrefix(sql, "WITH candidates AS MATERIALIZED") && strings.Contains(sql, "ctid AS row_tid") {
		c.physical.Store(sql)
	}
}

// This suite requires a dedicated, disposable database; it never uses the app DSN.
func TestFundingFIFOBatchPostgres(t *testing.T) {
	dsn := os.Getenv("FUNDING_BATCH_TEST_DSN")
	if dsn == "" {
		t.Skip("FUNDING_BATCH_TEST_DSN requires a dedicated codego_wallet_test_ database")
	}
	counter := &fundingSQLCounter{Interface: logger.Default.LogMode(logger.Silent)}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: counter})
	require.NoError(t, err)
	var identity struct {
		Database string
		Host     string
		Port     int
	}
	require.NoError(t, db.Raw("SELECT current_database() AS database, host(inet_server_addr()) AS host, inet_server_port() AS port").Scan(&identity).Error)
	require.True(t, strings.HasPrefix(identity.Database, "codego_wallet_test_"), "dedicated test database required")
	require.Contains(t, []string{"127.0.0.1", "::1"}, identity.Host)
	require.Equal(t, 55449, identity.Port)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(16)
	previousDB, previousPG, previousSQLite, previousMySQL := platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL
	platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL = db, true, false, false
	t.Cleanup(func() {
		platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL = previousDB, previousPG, previousSQLite, previousMySQL
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS billing").Error)
	require.NoError(t, db.AutoMigrate(&billingschema.BillingAccount{}, &billingschema.FundingLot{}, &billingschema.FundingAllocation{}))
	require.NoError(t, db.Exec(`CREATE INDEX IF NOT EXISTS idx_billing_funding_lots_available_fifo
		ON billing.funding_lots (account_id, created_at, lot_id) INCLUDE (remaining_amount) WHERE remaining_amount > 0`).Error)
	newAccount := func(t *testing.T) string {
		id := fmt.Sprintf("fifo-%d", time.Now().UnixNano())
		require.NoError(t, db.Create(&billingschema.BillingAccount{AccountID: id, AccountType: "wallet", OwnerType: "user", OwnerID: time.Now().UnixNano()}).Error)
		return id
	}
	seed := func(t *testing.T, account string, count int) {
		require.NoError(t, db.Exec(`INSERT INTO billing.funding_lots
			(lot_id,account_id,source,reference_type,reference_id,idempotency_key,original_amount,paid_quota,remaining_amount,revenue_multiplier,created_at)
			SELECT ? || '-' || lpad(n::text,8,'0'), ?, CASE WHEN n%2=0 THEN 'topup' ELSE 'blind_box' END,
			'test','synthetic',? || ':lot:' || n,1,CASE WHEN n%2=0 THEN 1 ELSE 0 END,1,
			CASE WHEN n%2=0 THEN 0.8 ELSE 0.2 END,'2026-01-01'::timestamptz
			FROM generate_series(1,?) n`, account, account, account, count).Error)
	}
	allocate := func(ctx context.Context, request, account string, amount int64) error {
		return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return allocateSettledFundingFIFOTx(tx, request, account, amount)
		})
	}
	remaining := func(t *testing.T, account string) int64 {
		var sum int64
		require.NoError(t, db.Model(&billingschema.FundingLot{}).Where("account_id = ?", account).Select("COALESCE(SUM(remaining_amount),0)").Scan(&sum).Error)
		return sum
	}
	allocations := func(t *testing.T, request string) []billingschema.FundingAllocation {
		var result []billingschema.FundingAllocation
		require.NoError(t, db.Where("request_id = ?", request).Order("lot_id").Find(&result).Error)
		return result
	}

	t.Run("100k_micro_lots_fifo_sources_and_replay", func(t *testing.T) {
		account := newAccount(t)
		seed(t, account, 100005)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		counter.count.Store(0)
		start := time.Now()
		require.NoError(t, allocate(ctx, account+":large", account, 100000))
		elapsed, statements := time.Since(start), counter.count.Load()
		t.Logf("PostgreSQL 100000 micro-lot allocation: elapsed=%s SQL_statements=%d complete_per_lot_receipts=100000", elapsed, statements)
		require.LessOrEqual(t, statements, int64(78), "guarded batching must avoid per-lot round trips")
		require.EqualValues(t, 5, remaining(t, account))
		rows := allocations(t, account+":large")
		require.Len(t, rows, 100000)
		for index, row := range rows {
			n := index + 1
			require.Equal(t, fmt.Sprintf("%s-%08d", account, n), row.LotID)
			require.EqualValues(t, 1, row.Amount)
			if n%2 == 0 {
				require.Equal(t, "topup", row.Source)
				require.EqualValues(t, 1, row.PaidQuota)
				require.Equal(t, 0.8, row.RevenueMultiplier)
			} else {
				require.Equal(t, "blind_box", row.Source)
				require.Zero(t, row.PaidQuota)
				require.Equal(t, 0.2, row.RevenueMultiplier)
			}
		}
		require.NoError(t, allocate(context.Background(), account+":large", account, 100000))
		require.EqualValues(t, 5, remaining(t, account))
		require.Len(t, allocations(t, account+":large"), 100000)
	})

	t.Run("partial_lot_rounding_and_int64_edges", func(t *testing.T) {
		account := newAccount(t)
		lot := billingschema.FundingLot{AccountID: account, Source: "topup", OriginalAmount: math.MaxInt64, RemainingAmount: math.MaxInt64, PaidQuota: math.MaxInt64 - 1, IdempotencyKey: account, RevenueMultiplier: 0.7}
		require.NoError(t, db.Create(&lot).Error)
		// The candidate prefix sum exceeds int64, while consumed quota does not.
		later := lot
		later.LotID, later.IdempotencyKey, later.CreatedAt = "", account+":later", lot.CreatedAt.Add(time.Second)
		require.NoError(t, db.Create(&later).Error)
		require.NoError(t, allocate(context.Background(), account+":one", account, 1))
		require.EqualValues(t, 0, allocations(t, account+":one")[0].PaidQuota)
		require.NoError(t, allocate(context.Background(), account+":two", account, math.MaxInt64-1))
		rows := allocations(t, account+":two")
		require.Len(t, rows, 1)
		require.Equal(t, lot.LotID, rows[0].LotID)
		require.EqualValues(t, math.MaxInt64-1, rows[0].PaidQuota)
		require.NoError(t, db.First(&later, "lot_id = ?", later.LotID).Error)
		require.EqualValues(t, math.MaxInt64, later.RemainingAmount)
		// A small paid fraction must be rounded cumulatively for each lot.
		small := newAccount(t)
		require.NoError(t, db.Create(&billingschema.FundingLot{AccountID: small, Source: "topup", OriginalAmount: 7, RemainingAmount: 7, PaidQuota: 3, IdempotencyKey: small}).Error)
		for n, want := range []int64{0, 0, 1, 0, 1, 0, 1} {
			request := fmt.Sprintf("%s:%d", small, n)
			require.NoError(t, allocate(context.Background(), request, small, 1))
			require.Equal(t, want, allocations(t, request)[0].PaidQuota)
		}
	})

	t.Run("multiple_batches_keep_same_timestamp_order_and_partial_lot", func(t *testing.T) {
		account := newAccount(t)
		seed(t, account, 12290)
		require.NoError(t, db.Model(&billingschema.FundingLot{}).Where("account_id = ?", account).
			Updates(map[string]any{"original_amount": 7, "remaining_amount": 7, "paid_quota": 3}).Error)
		const fullLots = 8192
		require.NoError(t, allocate(context.Background(), account+":partial", account, fullLots*7+3))
		rows := allocations(t, account+":partial")
		require.Len(t, rows, fullLots+1)
		for n, row := range rows {
			require.Equal(t, fmt.Sprintf("%s-%08d", account, n+1), row.LotID)
			if n < fullLots {
				require.EqualValues(t, 7, row.Amount)
				require.EqualValues(t, 3, row.PaidQuota)
			} else {
				require.EqualValues(t, 3, row.Amount)
				require.EqualValues(t, 1, row.PaidQuota)
			}
		}
		// A later request starts from the partially consumed lot rather than
		// inheriting the previous request's cursor or skipping the paid remainder.
		require.NoError(t, allocate(context.Background(), account+":rest", account, 5))
		rest := allocations(t, account+":rest")
		require.Len(t, rest, 2)
		require.Equal(t, rows[len(rows)-1].LotID, rest[0].LotID)
		require.EqualValues(t, 4, rest[0].Amount)
		require.EqualValues(t, 2, rest[0].PaidQuota)
		require.Equal(t, fmt.Sprintf("%s-%08d", account, fullLots+2), rest[1].LotID)
		require.EqualValues(t, 1, rest[1].Amount)
		require.Zero(t, rest[1].PaidQuota)
		require.EqualValues(t, 12290*7-(fullLots*7+3)-5, remaining(t, account))
	})

	t.Run("only_missing_amount_is_legacy", func(t *testing.T) {
		account := newAccount(t)
		seed(t, account, 2)
		require.NoError(t, allocate(context.Background(), account, account, 7))
		rows := allocations(t, account)
		require.Len(t, rows, 3)
		var legacy int64
		for _, row := range rows {
			if row.Source == billingschema.FundingSourceLegacyUnattributed {
				legacy += row.Amount
				require.Zero(t, row.PaidQuota)
			}
		}
		require.EqualValues(t, 5, legacy)
		require.Zero(t, remaining(t, account))
	})

	t.Run("concurrent_replay_waits_for_account_lock", func(t *testing.T) {
		account := newAccount(t)
		seed(t, account, 5)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		blocker := db.WithContext(ctx).Begin()
		require.NoError(t, blocker.Error)
		defer blocker.Rollback()
		var row billingschema.BillingAccount
		require.NoError(t, blocker.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id = ?", account).First(&row).Error)
		var pid int
		require.NoError(t, blocker.Raw("SELECT pg_backend_pid()").Scan(&pid).Error)
		results := make(chan error, 8)
		for n := 0; n < 8; n++ {
			go func() { results <- allocate(ctx, account, account, 3) }()
		}
		deadline := time.Now().Add(3 * time.Second)
		for {
			var waiting int
			require.NoError(t, db.Raw("SELECT COUNT(*) FROM pg_stat_activity WHERE ? = ANY(pg_blocking_pids(pid))", pid).Scan(&waiting).Error)
			if waiting > 0 {
				t.Logf("verified independent PostgreSQL account-lock waiters: %d", waiting)
				break
			}
			require.True(t, time.Now().Before(deadline), "expected real lock contention")
			time.Sleep(10 * time.Millisecond)
		}
		require.NoError(t, blocker.Commit().Error)
		for n := 0; n < 8; n++ {
			require.NoError(t, <-results)
		}
		require.EqualValues(t, 2, remaining(t, account))
		require.Len(t, allocations(t, account), 3)
	})

	t.Run("second_batch_failure_and_cancellation_roll_back", func(t *testing.T) {
		account := newAccount(t)
		seed(t, account, 5000)
		require.NoError(t, db.Exec(`CREATE OR REPLACE FUNCTION billing.test_fifo_failure() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
		IF NEW.request_id = current_setting('codego.test_fifo_failure', true) AND right(NEW.lot_id,8) = '00004500' THEN
			RAISE EXCEPTION 'synthetic second-batch allocation failure';
		END IF;
		RETURN NEW; END $$`).Error)
		require.NoError(t, db.Exec(`CREATE TRIGGER test_fifo_failure BEFORE INSERT ON billing.funding_allocations FOR EACH ROW EXECUTE FUNCTION billing.test_fifo_failure()`).Error)
		t.Cleanup(func() {
			require.NoError(t, db.Exec("DROP TRIGGER test_fifo_failure ON billing.funding_allocations").Error)
		})
		fallbacksBefore := counter.fallbacks.Load()
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT set_config('codego.test_fifo_failure', ?, true)", account).Error; err != nil {
				return err
			}
			return allocateSettledFundingFIFOTx(tx, account, account, 5000)
		})
		require.ErrorContains(t, err, "synthetic second-batch allocation failure")
		require.Equal(t, fallbacksBefore, counter.fallbacks.Load(), "SQL errors must propagate without primary-key fallback")
		require.EqualValues(t, 5000, remaining(t, account))
		require.Empty(t, allocations(t, account))
		// Cancel after the first batch has actually changed rows in this transaction.
		ctx, cancel := context.WithCancel(context.Background())
		err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			batch, err := allocateSettledFundingPostgresBatchTx(tx, account+":cancel", account, 5000, nil)
			if err != nil {
				return err
			}
			require.EqualValues(t, 4096, batch.Used)
			cancel()
			_, err = allocateSettledFundingPostgresBatchTx(tx, account+":cancel", account, 904, &batch)
			return err
		})
		cancel()
		require.ErrorIs(t, err, context.Canceled)
		require.EqualValues(t, 5000, remaining(t, account))
		require.Empty(t, allocations(t, account+":cancel"))
		require.NoError(t, allocate(context.Background(), account, account, 5000))
		require.Zero(t, remaining(t, account))
		require.Len(t, allocations(t, account), 5000)
	})

	t.Run("candidate_lock_uses_latest_tuple_after_concurrent_update", func(t *testing.T) {
		account := newAccount(t)
		lot := billingschema.FundingLot{LotID: account + "-first", AccountID: account, Source: "topup", OriginalAmount: 7, RemainingAmount: 7, PaidQuota: 3, IdempotencyKey: account}
		require.NoError(t, db.Create(&lot).Error)
		second := lot
		second.LotID, second.IdempotencyKey = account+"-second", account+":second"
		require.NoError(t, db.Create(&second).Error)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		blocker := db.WithContext(ctx).Begin()
		require.NoError(t, blocker.Error)
		defer blocker.Rollback()
		// Updating an indexed field creates a new physical tuple. Keep it
		// uncommitted so the candidate SELECT must wait and recheck its version.
		require.NoError(t, blocker.Model(&lot).Update("remaining_amount", 6).Error)
		var blockerPID int
		require.NoError(t, blocker.Raw("SELECT pg_backend_pid()").Scan(&blockerPID).Error)
		type outcome struct {
			batch fundingFIFOBatch
			err   error
		}
		result := make(chan outcome, 1)
		fallbacksBefore := counter.fallbacks.Load()
		go func() {
			var batch fundingFIFOBatch
			err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				var locked billingschema.BillingAccount
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("account_id = ?", account).First(&locked).Error; err != nil {
					return err
				}
				var err error
				batch, err = allocateSettledFundingPostgresBatchTx(tx, account, account, 8, nil)
				return err
			})
			result <- outcome{batch: batch, err: err}
		}()
		deadline := time.Now().Add(3 * time.Second)
		for {
			var waiting int
			require.NoError(t, db.Raw("SELECT COUNT(*) FROM pg_stat_activity WHERE ? = ANY(pg_blocking_pids(pid))", blockerPID).Scan(&waiting).Error)
			if waiting > 0 {
				break
			}
			require.True(t, time.Now().Before(deadline), "candidate must actually wait for the lot lock")
			time.Sleep(10 * time.Millisecond)
		}
		require.NoError(t, blocker.Commit().Error)
		out := <-result
		require.NoError(t, out.err)
		require.EqualValues(t, 8, out.batch.Used, "a concurrent tuple update must not skip a locked lot")
		require.EqualValues(t, fallbacksBefore+1, counter.fallbacks.Load(), "the stale-ctid partial batch must be rolled back before primary-key retry")
		require.Equal(t, second.LotID, out.batch.LotID)
		require.EqualValues(t, 5, remaining(t, account))
		rows := allocations(t, account)
		require.Len(t, rows, 2)
		require.EqualValues(t, 6, rows[0].Amount)
		require.EqualValues(t, 3, rows[0].PaidQuota)
		require.EqualValues(t, 2, rows[1].Amount)
		require.Zero(t, rows[1].PaidQuota)
	})

	t.Run("silent_receipt_skip_retries_entire_batch_or_fails_closed", func(t *testing.T) {
		require.NoError(t, db.Exec("CREATE SEQUENCE billing.test_fifo_skip_sequence").Error)
		require.NoError(t, db.Exec(`CREATE FUNCTION billing.test_fifo_skip_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
		IF NEW.request_id = current_setting('codego.test_fifo_skip', true) THEN
			IF current_setting('codego.test_fifo_skip_always', true) = 'yes' OR nextval('billing.test_fifo_skip_sequence') = 1 THEN
				RETURN NULL;
			END IF;
		END IF;
		RETURN NEW; END $$`).Error)
		require.NoError(t, db.Exec("CREATE TRIGGER test_fifo_skip_receipt BEFORE INSERT ON billing.funding_allocations FOR EACH ROW EXECUTE FUNCTION billing.test_fifo_skip_receipt()").Error)
		t.Cleanup(func() {
			require.NoError(t, db.Exec("DROP TRIGGER test_fifo_skip_receipt ON billing.funding_allocations").Error)
			require.NoError(t, db.Exec("DROP FUNCTION billing.test_fifo_skip_receipt()").Error)
			require.NoError(t, db.Exec("DROP SEQUENCE billing.test_fifo_skip_sequence").Error)
		})
		for _, scenario := range []string{"successful_fallback", "outer_rollback", "incomplete_primary_key_fallback"} {
			t.Run(scenario, func(t *testing.T) {
				account := newAccount(t)
				seed(t, account, 5)
				require.NoError(t, db.Exec("SELECT setval('billing.test_fifo_skip_sequence', 1, false)").Error)
				fallbacksBefore := counter.fallbacks.Load()
				outerFailure := errors.New("synthetic outer transaction failure after guarded fallback")
				err := db.Transaction(func(tx *gorm.DB) error {
					if err := tx.Exec("SELECT set_config('codego.test_fifo_skip', ?, true)", account).Error; err != nil {
						return err
					}
					if scenario == "incomplete_primary_key_fallback" {
						if err := tx.Exec("SELECT set_config('codego.test_fifo_skip_always', 'yes', true)").Error; err != nil {
							return err
						}
					}
					if err := allocateSettledFundingFIFOTx(tx, account, account, 5); err != nil {
						return err
					}
					if scenario == "outer_rollback" {
						return outerFailure
					}
					return nil
				})
				require.EqualValues(t, fallbacksBefore+1, counter.fallbacks.Load())
				if scenario == "outer_rollback" {
					require.ErrorIs(t, err, outerFailure)
				} else if scenario == "incomplete_primary_key_fallback" {
					require.ErrorContains(t, err, "funding FIFO batch attribution is incomplete")
				} else {
					require.NoError(t, err)
					require.Zero(t, remaining(t, account))
					require.Len(t, allocations(t, account), 5)
					require.NoError(t, allocate(context.Background(), account, account, 5))
					require.Len(t, allocations(t, account), 5)
					return
				}
				require.EqualValues(t, 5, remaining(t, account))
				require.Empty(t, allocations(t, account))
			})
		}
	})

	t.Run("null_timestamp_tail_is_not_skipped", func(t *testing.T) {
		account := newAccount(t)
		seed(t, account, 3)
		require.NoError(t, db.Model(&billingschema.FundingLot{}).Where("account_id = ?", account).Update("created_at", nil).Error)
		require.NoError(t, allocate(context.Background(), account, account, 4))
		require.Zero(t, remaining(t, account))
		require.Len(t, allocations(t, account), 4)
	})

	t.Run("guarded_fast_path_uses_tid_lookup", func(t *testing.T) {
		account := newAccount(t)
		seed(t, account, 1)
		require.NoError(t, allocate(context.Background(), account, account, 1))
		query, ok := counter.physical.Load().(string)
		require.True(t, ok)
		var raw []byte
		require.NoError(t, db.Raw("EXPLAIN (FORMAT JSON) "+query).Row().Scan(&raw))
		var plans []struct {
			Plan map[string]any `json:"Plan"`
		}
		require.NoError(t, json.Unmarshal(raw, &plans))
		var tid, primaryKey int
		var walk func(map[string]any)
		walk = func(node map[string]any) {
			if node["Node Type"] == "Tid Scan" {
				tid++
			}
			if node["Index Name"] == "funding_lots_pkey" {
				primaryKey++
			}
			children, _ := node["Plans"].([]any)
			for _, child := range children {
				walk(child.(map[string]any))
			}
		}
		walk(plans[0].Plan)
		t.Logf("guarded fast-path plan: Tid Scan=%d, funding_lots_pkey=%d", tid, primaryKey)
		require.Positive(t, tid)
		require.Zero(t, primaryKey)
	})
}
