// local-querybench measures query and reclaim paths against disposable loopback data.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	gatewayrouting "github.com/sh2001sh/new-api/internal/gateway/routing/app"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	marketplace "github.com/sh2001sh/new-api/internal/marketplace/app"
	schema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	settlement "github.com/sh2001sh/new-api/internal/marketplace/settlement"
	config "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type counter struct {
	logger.Interface
	count atomic.Int64
}

func (c *counter) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	c.count.Add(1)
	c.Interface.Trace(ctx, begin, fc, err)
}

func main() {
	mode := flag.String("mode", "measure", "seed, upgrade-fixture, measure, stress, reclaim, or validate")
	n := flag.Int("n", 100000, "settlement rows")
	tag := flag.String("tag", "before", "unique run name")
	viewers := flag.Int("viewers", 500, "distinct users in the 500-client page-query burst")
	flag.Parse()
	if err := run(*mode, *n, *tag, *viewers); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func open(name string, log logger.Interface) (*gorm.DB, error) {
	return gorm.Open(postgres.Open("postgres://loadtest@127.0.0.1:55437/"+name+"?sslmode=disable"), &gorm.Config{Logger: log})
}
func run(mode string, n int, tag string, viewers int) error {
	c := &counter{Interface: logger.Default.LogMode(logger.Silent)}
	if mode == "new-baseline-fixture" || mode == "new-final-fixture" {
		admin, err := open("postgres", c)
		if err != nil {
			return err
		}
		archive := "codego_query_archive"
		if mode == "new-final-fixture" {
			archive = "codego_query_baseline"
		}
		if err = admin.Exec("ALTER DATABASE codego_query_test RENAME TO " + archive).Error; err != nil {
			return err
		}
		if err = admin.Exec("CREATE DATABASE codego_query_test TEMPLATE codego_loadtest").Error; err != nil {
			return err
		}
		db, err := open("codego_query_test", c)
		if err != nil {
			return err
		}
		return seed(db)
	}
	if mode == "init-reclaim-test" {
		db, err := open("postgres", c)
		if err != nil {
			return err
		}
		return db.Exec("CREATE DATABASE codego_reclaim_test").Error
	}
	if mode == "seed" {
		admin, err := open("postgres", c)
		if err != nil {
			return err
		}
		var exists bool
		if err = admin.Raw("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname='codego_query_test')").Scan(&exists).Error; err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("codego_query_test already exists; fixtures are never overwritten")
		}
		if err = admin.Exec("CREATE DATABASE codego_query_test TEMPLATE codego_loadtest").Error; err != nil {
			return err
		}
	}
	db, err := open("codego_query_test", c)
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	sqlDB.SetMaxOpenConns(32)
	platformdb.DB, platformdb.LogDB = db, db
	platformdb.UsingPostgreSQL = true
	platformdb.UsingSQLite = false
	if mode == "upgrade-fixture" {
		return db.AutoMigrate(&schema.Group{}, &schema.Shop{}, &schema.ShopRating{}, &schema.MarketReport{})
	}
	if mode == "seed" || mode == "populate" {
		return seed(db)
	}
	if mode == "reclaim" {
		return reclaim(db, c, n, tag)
	}
	if mode == "validate" {
		return validateReclaim(db)
	}
	if mode == "stress" {
		return stress(c, viewers)
	}
	if mode == "inspect" {
		for _, q := range []string{
			`SELECT indexdef FROM pg_indexes WHERE schemaname='marketplace' AND tablename='settlements'`,
			`EXPLAIN (ANALYZE,BUFFERS) SELECT id,owner_user_id,owner_net_amount,reclaimed_amount,created_at FROM marketplace.settlements WHERE owner_user_id=800001 AND status='released' AND owner_net_amount>reclaimed_amount LIMIT 5000 FOR UPDATE`,
		} {
			var rows []string
			if err = db.Raw(q).Scan(&rows).Error; err != nil {
				return err
			}
			for _, s := range rows {
				fmt.Println(s)
			}
		}
		return nil
	}
	if mode != "measure" {
		return fmt.Errorf("unknown mode")
	}
	measure := func(name string, fn func() (int, error)) error {
		c.count.Store(0)
		start := time.Now()
		size, e := fn()
		result := map[string]any{"name": name, "duration_ms": float64(time.Since(start).Microseconds()) / 1000, "sql": c.count.Load(), "rows": size}
		if e != nil {
			result["error"] = e.Error()
		}
		b, _ := json.Marshal(result)
		fmt.Println(string(b))
		return e
	}
	if err = measure("route_config", func() (int, error) { r, e := gatewayrouting.ListRoutePoolGroups(); return len(r), e }); err != nil {
		return err
	}
	config.MemoryCacheEnabled = true
	gatewaystore.InitChannelCache()
	detail, err := gatewaystore.LoadEnabledRoutePool("bench-pool-0")
	if err != nil {
		return err
	}
	if err = measure("route_candidates_1000x", func() (int, error) {
		size := 0
		for i := 0; i < 1000; i++ {
			r, e := gatewaystore.LoadRoutePoolCandidates("bench-pool-0", "gpt-bench", detail)
			if e != nil {
				return 0, e
			}
			size = len(r)
		}
		return size, nil
	}); err != nil {
		return err
	}
	for _, uid := range []int{800001, 800002} {
		if err = measure(fmt.Sprintf("key_groups_%d", uid), func() (int, error) { r, e := marketplace.ListKeyGroupOptions(uid); return len(r), e }); err != nil {
			return err
		}
		if err = measure(fmt.Sprintf("market_%d", uid), func() (int, error) {
			r, e := marketplace.ListMarketplaceGroups(marketplace.GroupQuery{ViewerUserID: uid, PageSize: 20})
			if e != nil {
				return 0, e
			}
			return r.Total, nil
		}); err != nil {
			return err
		}
	}
	if err = measure("market_cache_alternating_100x", func() (int, error) {
		for i := 0; i < 100; i++ {
			if _, e := marketplace.ListMarketplaceGroups(marketplace.GroupQuery{ViewerUserID: 800001 + i%2, PageSize: 20}); e != nil {
				return 0, e
			}
		}
		return 100, nil
	}); err != nil {
		return err
	}
	if err = measure("status", func() (int, error) { r, e := gatewayrouting.BuildUserGroupStatus(800001, true); return len(r), e }); err != nil {
		return err
	}
	for _, keyword := range []string{"", "Q99999", "bench-user-99999", "no-such-user"} {
		if err = measure("users:"+keyword, func() (int, error) { r, _, e := identitystore.SearchUsers(keyword, "", 0, 20); return len(r), e }); err != nil {
			return err
		}
	}
	return nil
}

func stress(c *counter, viewers int) error {
	const clients = 500
	if viewers < 1 || viewers > 500 {
		return fmt.Errorf("viewers must be 1..500")
	}
	var wg sync.WaitGroup
	startGate := make(chan struct{})
	var failures atomic.Int64
	latencies := make([]float64, clients)
	c.count.Store(0)
	start := time.Now()
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-startGate
			t := time.Now()
			var value any
			var err error
			uid := 800001 + i%viewers
			switch i % 4 {
			case 0:
				var rows []marketplace.KeyGroupOption
				rows, err = marketplace.ListKeyGroupOptions(uid)
				value = rows
				if err == nil && len(rows) < 10001 {
					err = fmt.Errorf("truncated key selector: %d", len(rows))
				}
			case 1:
				var page *marketplace.GroupListResult
				page, err = marketplace.ListMarketplaceGroups(marketplace.GroupQuery{ViewerUserID: uid, PageSize: 20})
				value = page
				if err == nil && (page.Total < 10001 || len(page.Items) != 20) {
					err = fmt.Errorf("truncated market page")
				}
			case 2:
				var rows []gatewayrouting.UserGroupStatusItem
				rows, err = gatewayrouting.BuildUserGroupStatus(uid, true)
				value = rows
				if err == nil && len(rows) < 10002 {
					err = fmt.Errorf("truncated status groups: %d", len(rows))
				}
			case 3:
				rows, total, e := identitystore.SearchUsers(fmt.Sprintf("Q%05d", 99999-i), "", 0, 20)
				value, err = rows, e
				if err == nil && (len(rows) != 1 || total != 1) {
					err = fmt.Errorf("incorrect user search")
				}
			}
			if err == nil {
				err = json.NewEncoder(io.Discard).Encode(value)
			}
			if err != nil {
				failures.Add(1)
				fmt.Fprintln(os.Stderr, err)
			}
			latencies[i] = float64(time.Since(t).Microseconds()) / 1000
		}(i)
	}
	close(startGate)
	wg.Wait()
	sort.Float64s(latencies)
	b, _ := json.Marshal(map[string]any{"clients": clients, "viewers": viewers, "failures": failures.Load(), "duration_ms": float64(time.Since(start).Microseconds()) / 1000, "p50_ms": latencies[249], "p95_ms": latencies[474], "p99_ms": latencies[494], "sql": c.count.Load()})
	fmt.Println(string(b))
	if failures.Load() > 0 {
		return fmt.Errorf("stress requests failed")
	}
	return nil
}
func seed(db *gorm.DB) error {
	statements := []string{
		`INSERT INTO users (id,username,password,display_name,email,aff_code,external_id,claude_quota,"group",status) SELECT 800000+i,'bench-user-'||i,'','Bench '||i,'bench-'||i||'@invalid.local','bench-aff-'||i,'Q'||lpad(i::text,5,'0'),0,'default',1 FROM generate_series(1,99999) i`,
		`INSERT INTO channels (id,type,key,status,name,models,"group") SELECT 800000+i,1,'fixture-only',1,'bench-channel-'||i,'gpt-bench','bench-pool-'||((i-1)/100) FROM generate_series(1,10000) i`,
		`INSERT INTO abilities ("group",model,channel_id,enabled,priority,weight) SELECT 'bench-pool-'||((i-1)/100),'gpt-bench',800000+i,true,0,1 FROM generate_series(1,10000) i`,
		`INSERT INTO route_pools (id,name,"group",enabled,auto_discover) SELECT 800000+i,'bench-pool-'||i,'bench-pool-'||i,true,true FROM generate_series(0,999) i`,
		`INSERT INTO route_pool_members (route_pool_id,channel_id,cost_multiplier,model_cost_overrides,enabled) SELECT 800000+((i-1)/100),800000+i,1,'{}',true FROM generate_series(1,10000) i`,
		`INSERT INTO marketplace.channels (id,owner_user_id,provider_type,base_url_ciphertext,credential_ciphertext,status,declared_models,approved_source_label,source_label_status,model_verification_results,internal_channel_id) SELECT 'bench-channel-'||i,800001,'openai','fixture-only','fixture-only','active','["gpt-bench"]','bench','approved',repeat(' ',8192),800000+i FROM generate_series(1,10000) i`,
		`INSERT INTO marketplace.groups (id,channel_id,owner_user_id,public_slug,system_display_name,internal_group_name,source_type,credit_pool_policy,multiplier,lifecycle_status,verification_status,visibility,created_at,updated_at) SELECT 'bench-group-'||i,'bench-channel-'||i,800001,'bench-group-'||i,'Bench group '||i,'bench-market-'||i,'marketplace_user','universal_only',1,'active','passed','public',now(),now() FROM generate_series(1,10000) i`,
		`INSERT INTO marketplace.ranking_snapshots (id,group_id,window_hours,ranking_version,calculated_at) SELECT 'bench-snapshot-'||i,'bench-group-'||i,24,'marketplace-v7-consumer-cost',now() FROM generate_series(1,10000) i`,
		`ANALYZE`,
	}
	for _, s := range statements {
		if s != "ANALYZE" {
			s += " ON CONFLICT DO NOTHING"
		}
		if err := db.Exec(s).Error; err != nil {
			return fmt.Errorf("seed: %w", err)
		}
	}
	fmt.Println("seeded 99,999 users, 10,000 groups/channels, 1,000 route pools")
	return nil
}
func reclaim(db *gorm.DB, c *counter, n int, tag string) error {
	if n < 1 || n > 2000000 {
		return fmt.Errorf("n must be 1..2000000")
	}
	if err := db.Exec(`INSERT INTO marketplace.settlements (id,request_id,group_id,owner_user_id,consumer_user_id,consumer_amount,settlement_gross_amount,platform_commission,transaction_fee,owner_net_amount,reclaimed_amount,multiplier,status,created_at) SELECT ?||'-'||i,?||'-'||i,?,800001,800002,100,100,0,0,100,0,1,'released',now() + i*interval '1 microsecond' FROM generate_series(1,?) i`, tag, tag, tag, n).Error; err != nil {
		return err
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return billingapp.CreditMarketplaceOwnerEarningsTx(tx, 800001, int64(n)*100, "bench-credit:"+tag, "marketplace_owner_release")
	}); err != nil {
		return err
	}
	if err := db.Exec("ANALYZE marketplace.settlements").Error; err != nil {
		return err
	}
	calls := 0
	settlement.RegisterReclaimHook(func(tx *gorm.DB, owner, admin int, amount int64, key string) error {
		calls++
		return billingapp.ReclaimMarketplaceOwnerEarningsTx(tx, owner, admin, amount, key)
	})
	c.count.Store(0)
	start := time.Now()
	r, err := settlement.ReclaimPending(settlement.ReleaseFilter{OwnerUserIDs: []int{800001}, MaxAmount: int64(n) * 100, OperationID: "bench-reclaim:" + tag})
	ms := float64(time.Since(start).Microseconds()) / 1000
	if err != nil {
		return err
	}
	var sum int64
	if err = db.Model(&schema.Settlement{}).Where("group_id = ?", tag).Select("sum(reclaimed_amount)").Scan(&sum).Error; err != nil {
		return err
	}
	owner, e := billingapp.GetUserClaudeWalletQuota(800001)
	if e != nil {
		return e
	}
	if sum != int64(n)*100 || r.Amount != sum || owner != 0 {
		return fmt.Errorf("accounting mismatch: sum=%d amount=%d owner=%d", sum, r.Amount, owner)
	}
	b, _ := json.Marshal(map[string]any{"name": tag, "rows": n, "duration_ms": ms, "sql": c.count.Load(), "wallet_transfers": calls, "amount": r.Amount, "owner_balance": owner, "accounting": "PASS"})
	fmt.Println(string(b))
	return nil
}
