package app

// Opt-in service/HTTP benchmark on synthetic loopback fixtures. Historical result
// caches and in-flight sharing are bypassed to measure independent cold reads.
import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	schema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"gorm.io/gorm"
)

func TestLocalIncomePerformance(t *testing.T) {
	db := incomePostgresDB(t, "INCOME_BENCH_DSN", "codego_income_fix_20261018")
	phase := os.Getenv("INCOME_BENCH_PHASE")
	if phase != "before" && phase != "after" {
		t.Fatal("INCOME_BENCH_PHASE must be before or after")
	}
	var installed bool
	if err := db.Raw("SELECT to_regclass('marketplace.income_hourly') IS NOT NULL").Scan(&installed).Error; err != nil {
		t.Fatal(err)
	}
	if installed != (phase == "after") {
		t.Fatal("benchmark phase does not match projection installation state")
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(32)
	if err = db.AutoMigrate(&schema.Group{}); err != nil {
		t.Fatal(err)
	}
	var serial atomic.Int64
	handlers := map[string]func() (any, error){
		"/income-10d": func() (any, error) {
			value, err := loadAdminOwnerIncome(AdminOwnerIncomeQuery{StartTimestamp: 1791381660, EndTimestamp: 1792245600}, "", fmt.Sprintf("bench-%s-%d", phase, serial.Add(1)))
			if err != nil {
				return nil, err
			}
			if value.OwnerCount != 151 || value.TotalIncome != value.RequestCount*110 {
				return nil, fmt.Errorf("incorrect income fixture totals")
			}
			return populateAdminOwnerBalances(value)
		},
		"/income-all": func() (any, error) {
			value, err := loadAdminOwnerIncome(AdminOwnerIncomeQuery{}, "", fmt.Sprintf("bench-%s-%d", phase, serial.Add(1)))
			if err != nil {
				return nil, err
			}
			if value.OwnerCount != 151 || value.TotalIncome != value.RequestCount*110 {
				return nil, fmt.Errorf("incorrect income fixture totals")
			}
			return populateAdminOwnerBalances(value)
		},
		"/owner-index-66": func() (any, error) {
			rows, e := loadOwnerChannelIndex(db, 900001)
			if e == nil && len(rows) != 66 {
				return nil, fmt.Errorf("expected 66 channels, got %d", len(rows))
			}
			return rows, e
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fn, ok := handlers[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		value, err := fn()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err = json.NewEncoder(w).Encode(value); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client := &http.Client{Timeout: 120 * time.Second, Transport: &http.Transport{MaxIdleConns: 64, MaxIdleConnsPerHost: 32}}
	defer client.CloseIdleConnections()
	request := func(path string) (float64, string, error) {
		start := time.Now()
		response, err := client.Get(server.URL + path)
		if err != nil {
			return 0, "", err
		}
		b, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			return 0, "", err
		}
		if response.StatusCode != 200 {
			return 0, "", fmt.Errorf("%d %s", response.StatusCode, b)
		}
		hash := sha256.Sum256(b)
		return float64(time.Since(start).Microseconds()) / 1000, hex.EncodeToString(hash[:]), nil
	}
	f, err := os.OpenFile(os.Getenv("INCOME_BENCH_OUTPUT"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	emit := func(value any) {
		b, _ := json.Marshal(value)
		fmt.Println(string(b))
		if _, err = f.Write(append(b, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"/income-10d", "/income-all", "/owner-index-66"} {
		ms, expected, err := request(path)
		if err != nil {
			t.Fatal(err)
		}
		emit(map[string]any{"phase": phase, "path": path, "first_ms": ms, "result_sha256": expected})
		for _, c := range []int{1, 4, 16, 32} {
			n := c * 3
			if n < 10 {
				n = 10
			}
			latencies := make([]float64, n)
			errs := make([]error, n)
			var next atomic.Int64
			var wg sync.WaitGroup
			start := time.Now()
			for range c {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for {
						i := int(next.Add(1) - 1)
						if i >= n {
							return
						}
						var hash string
						latencies[i], hash, errs[i] = request(path)
						if errs[i] == nil && hash != expected {
							errs[i] = fmt.Errorf("result mismatch")
						}
					}
				}()
			}
			wg.Wait()
			elapsed := time.Since(start).Seconds()
			failures := 0
			for _, e := range errs {
				if e != nil {
					t.Log(e)
					failures++
				}
			}
			sort.Float64s(latencies)
			emit(map[string]any{"phase": phase, "path": path, "concurrency": c, "requests": n, "failures": failures, "p50_ms": latencies[(n-1)/2], "p95_ms": latencies[int(math.Ceil(float64(n)*.95))-1], "elapsed_seconds": elapsed, "rps": float64(n) / elapsed, "result_sha256": expected})
			if failures != 0 {
				t.Fatal("load test failures")
			}
		}
	}
	for _, bounded := range []bool{true, false} {
		start, end := int64(0), int64(0)
		if bounded {
			start, end = 1791381660, 1792245600
		}
		query, e := incomeFacts(db, start, end)
		if e != nil {
			t.Fatal(e)
		}
		query = query.Select("owner_user_id, " + incomeSums).Group("owner_user_id").Having("SUM(request_count)>0").Order("total_income DESC, owner_user_id ASC")
		sql := query.ToSQL(func(q *gorm.DB) *gorm.DB { return q.Find(&[]AdminOwnerIncomeItem{}) })
		var plan string
		if e = db.Raw("EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) " + sql).Row().Scan(&plan); e != nil {
			t.Fatal(e)
		}
		emit(map[string]any{"phase": phase, "bounded": bounded, "plan": json.RawMessage(plan)})
	}
	// Insert overhead is measured with identical statements and rollback in both
	// phases; it includes the trigger and WAL, but does not alter the read fixture.
	for _, c := range []int{1, 16} {
		n := c * 20
		latencies := make([]float64, n)
		errs := make([]error, n)
		var next atomic.Int64
		var wg sync.WaitGroup
		start := time.Now()
		for range c {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					i := int(next.Add(1) - 1)
					if i >= n {
						return
					}
					begin := time.Now()
					tx := db.Begin()
					if tx.Error != nil {
						errs[i] = tx.Error
						continue
					}
					errs[i] = tx.Exec(`INSERT INTO marketplace.settlements (id,request_id,group_id,owner_user_id,consumer_user_id,consumer_amount,platform_commission,transaction_fee,owner_net_amount,multiplier,status,created_at)
   SELECT 'insert-bench-'||?||'-'||i, 'insert-bench-'||?||'-'||i,'income-group-0',900001,900002,100,0,0,100,1,'pending',now() FROM generate_series(1,100) i`, i, i).Error
					if e := tx.Rollback().Error; errs[i] == nil {
						errs[i] = e
					}
					latencies[i] = float64(time.Since(begin).Microseconds()) / 1000
				}
			}()
		}
		wg.Wait()
		for _, e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		sort.Float64s(latencies)
		emit(map[string]any{"phase": phase, "path": "insert-100-rollback", "concurrency": c, "requests": n, "failures": 0, "p50_ms": latencies[(n-1)/2], "p95_ms": latencies[int(math.Ceil(float64(n)*.95))-1], "elapsed_seconds": time.Since(start).Seconds()})
	}
}

func TestLocalIncomePerformanceFixture(t *testing.T) {
	incomePostgresDB(t, "INCOME_BENCH_DSN", "codego_income_fix_20261018")
	all, err := ListAdminOwnerIncome(AdminOwnerIncomeQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if all.OwnerCount != 151 || all.RequestCount != 11424108 || all.TotalIncome != 11424108*110 {
		t.Fatalf("incorrect complete fixture: %+v", all)
	}
	ranged, err := ListAdminOwnerIncome(AdminOwnerIncomeQuery{StartTimestamp: 1791381660, EndTimestamp: 1792245600})
	if err != nil {
		t.Fatal(err)
	}
	if ranged.OwnerCount != 151 || ranged.RequestCount < 3000000 || ranged.RequestCount > 4000000 || ranged.TotalIncome != ranged.RequestCount*110 {
		t.Fatal("incorrect ranged fixture")
	}
	index, err := ListOwnerChannelIndex(900001)
	if err != nil {
		t.Fatal(err)
	}
	if len(index) != 66 {
		t.Fatalf("expected 66 channels, got %d", len(index))
	}
	owner, err := ListAdminOwnerIncome(AdminOwnerIncomeQuery{OwnerSearch: "I00001"})
	if err != nil {
		t.Fatal(err)
	}
	total := int64(0)
	for _, v := range index {
		total += v.TotalIncome
	}
	if owner.OwnerCount != 1 || owner.Items[0].OwnerUserID != 900001 || owner.TotalIncome != total {
		t.Fatal("owner search/index scope mismatch")
	}
	for path, value := range map[string]any{"/income-all": all, "/income-10d": ranged, "/owner-index-66": index} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(append(encoded, '\n'))
		fmt.Printf("verified_response path=%s sha256=%x\n", path, hash)
	}
	fmt.Printf("PASS fixture: %d settlements, %d owners, %d range rows, 66 owner channels, exact scoped amounts\n", all.RequestCount, all.OwnerCount, ranged.RequestCount)
}
