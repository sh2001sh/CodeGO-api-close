package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	marketplacesettlement "github.com/sh2001sh/new-api/internal/marketplace/settlement"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func incomePostgresDB(t *testing.T, env, requiredName string) *gorm.DB {
	t.Helper()
	dsn := os.Getenv(env)
	if dsn == "" {
		t.Skip(env + " is not set")
	}
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	var name, host string
	require.NoError(t, db.Raw("SELECT current_database(), host(inet_server_addr())").Row().Scan(&name, &host))
	require.Equal(t, requiredName, name)
	require.Contains(t, []string{"127.0.0.1", "::1"}, host, "local fixtures only")
	oldDB, oldPG, oldSQLite := platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite
	platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite = db, true, false
	t.Cleanup(func() {
		platformdb.DB, platformdb.UsingPostgreSQL, platformdb.UsingSQLite = oldDB, oldPG, oldSQLite
		invalidateAdminMarketplaceStatsCache()
		conn, e := db.DB()
		if e == nil {
			require.NoError(t, conn.Close())
		}
	})
	return db
}

func installIncomeProjection(t *testing.T, db *gorm.DB) {
	t.Helper()
	script, err := os.ReadFile("../../../scripts/marketplace-income-hourly.sql")
	require.NoError(t, err)
	// The caller owns this transaction, including the lock, DDL and backfill.
	sql := strings.ReplaceAll(strings.ReplaceAll(string(script), "BEGIN;", ""), "COMMIT;", "")
	require.NoError(t, db.Exec(sql).Error)
}

type incomeCheckRow struct {
	OwnerUserID                                                                                int
	GroupID                                                                                    string
	RequestCount, TotalIncome, PendingIncome, ReleasedIncome, ReclaimedIncome, ForfeitedIncome int64
}

func assertIncomeProjection(t *testing.T, db *gorm.DB, start, end int64) {
	t.Helper()
	facts, err := incomeFacts(db, start, end)
	require.NoError(t, err)
	var got, want []incomeCheckRow
	require.NoError(t, facts.Select("owner_user_id, group_id, "+incomeSums).Group("owner_user_id, group_id").Having("SUM(request_count)>0").Order("owner_user_id, group_id").Scan(&got).Error)
	raw := db.Model(&marketplaceschema.Settlement{}).Select(`owner_user_id, group_id, COUNT(*) request_count, SUM(owner_net_amount) total_income,
 SUM(CASE WHEN status='pending' THEN owner_net_amount ELSE 0 END) pending_income,
 SUM(CASE WHEN status='released' THEN owner_net_amount-reclaimed_amount ELSE 0 END) released_income,
 SUM(CASE WHEN status='reclaimed' THEN owner_net_amount ELSE reclaimed_amount END) reclaimed_income,
 SUM(CASE WHEN status='forfeited' THEN owner_net_amount ELSE 0 END) forfeited_income`)
	if start > 0 {
		raw = raw.Where("created_at>=?", time.Unix(start, 0))
	}
	if end > 0 {
		raw = raw.Where("created_at<?", time.Unix(end+1, 0))
	}
	require.NoError(t, raw.Group("owner_user_id, group_id").Order("owner_user_id, group_id").Scan(&want).Error)
	require.Equal(t, want, got, "range %d..%d", start, end)
}

func TestPostgresIncomeProjection(t *testing.T) {
	db := incomePostgresDB(t, "TEST_INCOME_POSTGRES_DSN", "codego_income_projection_test")
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	platformdb.DB = tx
	require.NoError(t, tx.Exec("CREATE SCHEMA IF NOT EXISTS marketplace").Error)
	require.NoError(t, tx.AutoMigrate(&marketplaceschema.Settlement{}, &marketplaceschema.IncomeReclaim{}))
	base := time.Date(2026, 10, 7, 22, 0, 0, 0, time.UTC)
	statuses := []string{"pending", "released", "released", "reclaimed", "forfeited", "unknown"}
	for i, status := range statuses {
		item := marketplaceschema.Settlement{ID: fmt.Sprint(i), RequestID: fmt.Sprint(i), GroupID: "g", OwnerUserID: 42, OwnerNetAmount: 100 + int64(i), Status: status, CreatedAt: base.Add(time.Duration(i)*time.Hour + 500*time.Millisecond)}
		if i == 2 {
			item.ReclaimedAmount = 40
		}
		require.NoError(t, tx.Create(&item).Error)
	}
	// Cover existing data (including legacy reclaimed_amount=0), not just new inserts.
	installIncomeProjection(t, tx)
	ranges := [][2]int64{{0, 0}, {base.Unix(), base.Add(6*time.Hour).Unix() - 1}, {base.Add(time.Minute).Unix(), base.Add(4*time.Hour + 30*time.Minute).Unix()}, {base.Unix(), base.Unix()}, {base.Add(time.Second).Unix(), base.Add(30 * time.Minute).Unix()}, {base.Add(time.Minute).Unix(), 0}, {0, base.Add(2*time.Hour + 17*time.Second).Unix()}, {base.Add(10 * time.Hour).Unix(), base.Add(11 * time.Hour).Unix()}}
	check := func() {
		for _, r := range ranges {
			assertIncomeProjection(t, tx, r[0], r[1])
		}
	}
	check()
	t.Run("insert release partial reclaim forfeit move and delete before compaction", func(t *testing.T) {
		require.NoError(t, tx.Exec("INSERT INTO marketplace.settlements SELECT * FROM marketplace.settlements WHERE false").Error)
		item := marketplaceschema.Settlement{ID: "late", RequestID: "late", GroupID: "g", OwnerUserID: 42, OwnerNetAmount: 3000000000, Status: "released", CreatedAt: base.Add(2*time.Hour + 999*time.Millisecond)}
		require.NoError(t, tx.Create(&item).Error)
		require.NoError(t, tx.Model(&item).Update("reclaimed_amount", int64(2147483648)).Error)
		require.NoError(t, tx.Model(&marketplaceschema.Settlement{}).Where("id = ?", "0").Update("status", "released").Error)
		require.NoError(t, tx.Model(&marketplaceschema.Settlement{}).Where("id = ?", "1").Updates(map[string]any{"owner_user_id": 77, "group_id": "moved", "created_at": base.Add(3 * time.Hour)}).Error)
		require.NoError(t, tx.Model(&marketplaceschema.Settlement{}).Where("id = ?", "2").Update("status", "forfeited").Error)
		require.NoError(t, tx.Delete(&marketplaceschema.Settlement{}, "id = ?", "3").Error)
		check()
		facts, err := incomeFacts(tx, 0, 0)
		require.NoError(t, err)
		var total int64
		require.NoError(t, facts.Select("COALESCE(SUM(total_income),0)").Where("owner_user_id=? AND group_id=?", 77, "g").Scan(&total).Error)
		require.Zero(t, total, "owner/group scope cannot leak earnings")
	})
	t.Run("compaction and transaction rollback preserve all sums", func(t *testing.T) {
		require.NoError(t, tx.SavePoint("before_compact").Error)
		n, err := marketplacesettlement.CompactIncomeProjection(context.Background())
		require.NoError(t, err)
		require.Positive(t, n)
		check()
		require.NoError(t, tx.RollbackTo("before_compact").Error)
		check()
		_, err = marketplacesettlement.CompactIncomeProjection(context.Background())
		require.NoError(t, err)
		check()
	})
	t.Run("wallet hook failure rolls back audit and projection then same ID succeeds", func(t *testing.T) {
		marketplacesettlement.RegisterReclaimHook(func(*gorm.DB, int, int, int64, string) error { return errors.New("injected wallet failure") })
		defer marketplacesettlement.RegisterReclaimHook(nil)
		task, err := marketplacesettlement.CreateIncomeReclaimTask(marketplacesettlement.ReleaseFilter{OwnerUserIDs: []int{42}, MaxAmount: 50, OperationID: "projection-reclaim"})
		require.NoError(t, err)
		_, err = marketplacesettlement.ProcessIncomeReclaimTask(task.ID)
		require.ErrorContains(t, err, "injected wallet failure")
		var untouched marketplaceschema.Settlement
		require.NoError(t, tx.First(&untouched, "id = ?", "0").Error)
		require.Equal(t, "released", untouched.Status)
		require.Zero(t, untouched.ReclaimedAmount)
		check()
		transfers := 0
		marketplacesettlement.RegisterReclaimHook(func(*gorm.DB, int, int, int64, string) error { transfers++; return nil })
		task, err = marketplacesettlement.CreateIncomeReclaimTask(marketplacesettlement.ReleaseFilter{OwnerUserIDs: []int{42}, MaxAmount: 50, OperationID: "projection-reclaim"})
		require.NoError(t, err)
		task, err = marketplacesettlement.ProcessIncomeReclaimTask(task.ID)
		require.NoError(t, err)
		require.Equal(t, "completed", task.Status)
		require.EqualValues(t, 50, task.Amount)
		require.Equal(t, 1, transfers)
		check()
		_, err = marketplacesettlement.ProcessIncomeReclaimTask(task.ID)
		require.NoError(t, err)
		require.Equal(t, 1, transfers)
	})
}

func TestPostgresIncomeProjectionConcurrency(t *testing.T) {
	db := incomePostgresDB(t, "TEST_INCOME_CONCURRENCY_DSN", "codego_income_concurrency_test")
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS marketplace").Error)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Settlement{}))
	var installed bool
	require.NoError(t, db.Raw("SELECT to_regclass('marketplace.income_hourly') IS NOT NULL").Scan(&installed).Error)
	if !installed {
		tx := db.Begin()
		installIncomeProjection(t, tx)
		require.NoError(t, tx.Commit().Error)
	}
	prefix := fmt.Sprintf("concurrent-%d-", time.Now().UnixNano())
	t.Cleanup(func() { require.NoError(t, db.Delete(&marketplaceschema.Settlement{}, "id LIKE ?", prefix+"%").Error) })
	insert := func(conn *gorm.DB, id string) error {
		return conn.Exec(`INSERT INTO marketplace.settlements (id,request_id,group_id,owner_user_id,consumer_user_id,consumer_amount,platform_commission,transaction_fee,owner_net_amount,multiplier,status,created_at)
   VALUES (?,?,'hot-group',900050,1,100,0,0,100,1,'released',now()) ON CONFLICT DO NOTHING`, prefix+id, prefix+id).Error
	}
	snapshot := func() error {
		var equal bool
		err := db.Raw(`SELECT
   COALESCE((SELECT SUM(request_count) FROM marketplace.income_hourly WHERE owner_user_id=900050),0) = (SELECT COUNT(*) FROM marketplace.settlements WHERE owner_user_id=900050)
   AND COALESCE((SELECT SUM(total_income) FROM marketplace.income_hourly WHERE owner_user_id=900050),0) = COALESCE((SELECT SUM(owner_net_amount) FROM marketplace.settlements WHERE owner_user_id=900050),0)
   AND COALESCE((SELECT SUM(released_income) FROM marketplace.income_hourly WHERE owner_user_id=900050),0) = COALESCE((SELECT SUM(CASE WHEN status='released' THEN owner_net_amount-reclaimed_amount ELSE 0 END) FROM marketplace.settlements WHERE owner_user_id=900050),0)
   AND COALESCE((SELECT SUM(reclaimed_income) FROM marketplace.income_hourly WHERE owner_user_id=900050),0) = COALESCE((SELECT SUM(CASE WHEN status='reclaimed' THEN owner_net_amount ELSE reclaimed_amount END) FROM marketplace.settlements WHERE owner_user_id=900050),0)`).Scan(&equal).Error
		if err != nil {
			return err
		}
		if !equal {
			return errors.New("projection differs from detail in one MVCC snapshot")
		}
		return nil
	}
	// A smaller sequence ID can commit after larger ones have been compacted.
	late := db.Begin()
	require.NoError(t, late.Error)
	defer late.Rollback()
	require.NoError(t, insert(late, "late"))
	require.NoError(t, insert(db, "early"))
	_, err := marketplacesettlement.CompactIncomeProjection(context.Background())
	require.NoError(t, err)
	require.NoError(t, snapshot())
	require.NoError(t, late.Commit().Error)
	_, err = marketplacesettlement.CompactIncomeProjection(context.Background())
	require.NoError(t, err)
	require.NoError(t, snapshot())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := make(chan error, 20)
	var background, writers sync.WaitGroup
	var snapshots atomic.Int64
	background.Add(2)
	go func() {
		defer background.Done()
		for ctx.Err() == nil {
			if _, e := marketplacesettlement.CompactIncomeProjection(context.Background()); e != nil {
				errs <- e
				return
			}
		}
	}()
	go func() {
		defer background.Done()
		for ctx.Err() == nil {
			if e := snapshot(); e != nil {
				errs <- e
				return
			}
			snapshots.Add(1)
		}
	}()
	for writer := range 16 {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for i := range 30 {
				id := fmt.Sprintf("%d-%d", writer, i)
				if e := insert(db, id); e != nil {
					errs <- e
					return
				}
				if e := insert(db, id); e != nil {
					errs <- e
					return
				}
				if e := db.Model(&marketplaceschema.Settlement{}).Where("id=?", prefix+id).Update("reclaimed_amount", 40).Error; e != nil {
					errs <- e
					return
				}
			}
		}()
	}
	writers.Wait()
	cancel()
	background.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	require.Positive(t, snapshots.Load())
	require.NoError(t, snapshot())
	t.Logf("PASS 16 concurrent writers, duplicate inserts, partial reclaims, compaction, late commits; %d consistent read snapshots", snapshots.Load())
}
