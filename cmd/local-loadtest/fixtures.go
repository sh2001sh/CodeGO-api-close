package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformstore "github.com/sh2001sh/new-api/internal/platform/store"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func fixtureDB() (*gorm.DB, error) {
	platformdb.UsingPostgreSQL = true
	return gorm.Open(postgres.Open(fixtureDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
}

func seedFixtures() error {
	// Fixed, dedicated loopback database; never inherit SQL_DSN or a .env file.
	admin, err := gorm.Open(postgres.Open("postgres://loadtest@127.0.0.1:55437/postgres?sslmode=disable"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return err
	}
	sqlAdmin, _ := admin.DB()
	defer sqlAdmin.Close()
	var exists bool
	if err = admin.Raw("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = 'codego_loadtest')").Scan(&exists).Error; err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("fixture database already exists; refusing to overwrite it")
	}
	if err = admin.Exec("CREATE DATABASE codego_loadtest").Error; err != nil {
		return err
	}
	db, err := fixtureDB()
	if err != nil {
		return err
	}
	platformdb.DB, platformdb.LogDB = db, db
	platformconfig.IsMasterNode = true
	if err = platformstore.BootstrapPrimarySchema(); err != nil {
		return err
	}
	if err = platformstore.ApplyV2Migrations(context.Background(), false); err != nil {
		return err
	}
	if err = db.Exec("INSERT INTO options (key,value) VALUES (?,?) ON CONFLICT (key) DO UPDATE SET value=excluded.value", "billing_schema_version", commerceschema.UnifiedCreditMigrationVersion).Error; err != nil {
		return err
	}
	for key, value := range map[string]string{"ModelRequestRateLimitEnabled": "false", "PerformanceMonitorEnabled": "false"} {
		if err = db.Exec("INSERT INTO options (key,value) VALUES (?,?) ON CONFLICT (key) DO UPDATE SET value=excluded.value", key, value).Error; err != nil {
			return err
		}
	}
	for i := 1; i <= 128; i++ {
		user := identityschema.User{Id: 10000 + i, Username: fmt.Sprintf("loadtest%d", i), Password: "unused-fixture", DisplayName: "local fixture", Role: 1, Status: 1, Quota: 1_000_000_000, ClaudeQuota: 1_000_000_000, Group: "default", AffCode: fmt.Sprintf("loadtest%d", i)}
		if err = db.Create(&user).Error; err != nil {
			return err
		}
		key := identityschema.Token{UserId: user.Id, Key: fixtureKey(i), Name: "local loadtest", Status: 1, UnlimitedQuota: true, ExpiredTime: -1, Group: "default"}
		if err = db.Create(&key).Error; err != nil {
			return err
		}
		account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "claude_wallet", OwnerType: "user", OwnerID: int64(user.Id), QuotaUnit: "quota"})
		if err != nil {
			return err
		}
		if _, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: 1_000_000_000, IdempotencyKey: fmt.Sprintf("loadtest-seed-%d", i), ReasonCode: "local_fixture"}); err != nil {
			return err
		}
	}
	base := "http://127.0.0.1:18181"
	var members []gatewayschema.RoutePoolMember
	for i := 1; i <= 32; i++ {
		ch := gatewayschema.Channel{Id: 20000 + i, Type: 1, Key: "local-upstream-fixture", Status: 1, Name: fmt.Sprintf("fixture%d", i), Group: "default", Models: "gpt-4.1", BaseURL: &base, ChannelScope: gatewayschema.ChannelScopeOfficial, CreatedTime: time.Now().Unix()}
		if err = db.Create(&ch).Error; err != nil {
			return err
		}
		if err = gatewaystore.AddChannelAbilities(&ch, db); err != nil {
			return err
		}
		members = append(members, gatewayschema.RoutePoolMember{ChannelID: ch.Id, Enabled: true, CostMultiplier: 1, FaultDomain: fmt.Sprintf("fixture-%d", i)})
	}
	if _, err = gatewaystore.SaveRoutePool(&gatewayschema.RoutePool{Name: "local-loadtest", Group: "default", Enabled: true}, members); err != nil {
		return err
	}
	fmt.Println("seeded isolated PostgreSQL: 128 funded users, 32 official route members")
	return nil
}

func checkFixtures() error {
	db, err := fixtureDB()
	if err != nil {
		return err
	}
	for _, query := range []string{
		"SELECT status, count(*) FROM billing.reservations GROUP BY status",
		"SELECT count(*) AS negative_snapshots FROM billing.balance_snapshots WHERE available_balance < 0 OR reserved_balance < 0",
		"SELECT count(*) AS ledger_entries FROM billing.ledger_entries",
		"SELECT count(*) AS pending_events FROM billing.outbox_events WHERE status = 'pending'",
		"SELECT COALESCE(round(extract(epoch FROM now()-min(created_at))::numeric,2),0) AS oldest_pending_seconds FROM billing.outbox_events WHERE status = 'pending'",
		"SELECT count(*) AS balance_conservation_errors FROM billing.balance_snapshots WHERE available_balance + reserved_balance + consumed_total <> granted_total",
		"SELECT count(*) AS settled_without_settlement FROM billing.reservations r LEFT JOIN billing.settlements s USING (reservation_id) WHERE r.status = 'settled' AND s.settlement_id IS NULL",
		"SELECT count(*) AS reservation_balance_errors FROM billing.balance_snapshots b WHERE b.reserved_balance <> COALESCE((SELECT sum(r.reserved_amount) FROM billing.reservations r WHERE r.account_id = b.account_id AND r.status = 'open'),0)",
		"SELECT count(*) AS settlement_total_errors FROM billing.balance_snapshots b WHERE b.consumed_total <> COALESCE((SELECT sum(s.actual_amount) FROM billing.settlements s JOIN billing.reservations r USING (reservation_id) WHERE r.account_id = b.account_id AND r.status = 'settled'),0)",
		`SELECT model_name, prompt_tokens, completion_tokens, other::jsonb->>'cache_tokens' AS cache_tokens, quota, count(*) FROM logs WHERE type=2 AND model_name IN ('gpt-6.1-sol','gpt-6-astra') GROUP BY model_name,prompt_tokens,completion_tokens,other::jsonb->>'cache_tokens',quota ORDER BY model_name,prompt_tokens,completion_tokens`,
		`SELECT count(*) AS long_context_settlement_audit_errors FROM logs l JOIN billing.reservations r ON r.request_id=l.request_id JOIN billing.settlements s USING (reservation_id) WHERE l.type=2 AND l.model_name IN ('gpt-6.1-sol','gpt-6-astra') AND l.quota<>s.actual_amount`,
		`SELECT round(avg((other::jsonb->'first_byte_trace'->>'billing_reservation_ms')::numeric),2) AS reserve_ms, round(avg((other::jsonb->'first_byte_trace'->>'route_selection_ms')::numeric),2) AS route_ms, round(avg((other::jsonb->'first_byte_trace'->>'upstream_first_event_ms')::numeric),2) AS upstream_ms, round(avg((other::jsonb->'first_byte_trace'->>'semantic_to_first_flush_ms')::numeric),2) AS flush_ms FROM (SELECT other FROM logs WHERE type=2 ORDER BY id DESC LIMIT 2000) AS recent`,
		"SELECT state, wait_event_type, count(*) FROM pg_stat_activity WHERE datname = 'codego_loadtest' GROUP BY state, wait_event_type",
	} {
		var rows []map[string]any
		if err = db.Raw(query).Scan(&rows).Error; err != nil {
			return err
		}
		fmt.Printf("%s\n%v\n", query, rows)
	}
	return nil
}

// Only amend known local fixtures; never accept a database from the environment.
func prepareLongFixtures() error {
	db, err := fixtureDB()
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var channels []gatewayschema.Channel
		if err := tx.Where("id BETWEEN 20001 AND 20032 AND name LIKE ?", "fixture%").Find(&channels).Error; err != nil {
			return err
		}
		if len(channels) != 32 {
			return fmt.Errorf("expected 32 known fixture channels")
		}
		for _, ch := range channels {
			ch.Models = "gpt-4.1,gpt-6.1-sol,gpt-6-astra"
			if err := tx.Model(&ch).Update("models", ch.Models).Error; err != nil {
				return err
			}
			if err := gatewaystore.AddChannelAbilities(&ch, tx); err != nil {
				return err
			}
		}
		// Explicit fixture prices, independent of production options or credentials.
		for key, additions := range map[string]map[string]float64{
			"ModelRatio":      {"gpt-6.1-sol": 1, "gpt-6-astra": 5},
			"CompletionRatio": {"gpt-6.1-sol": 5, "gpt-6-astra": 5},
			"CacheRatio":      {"gpt-6.1-sol": 0.1, "gpt-6-astra": 0.1},
		} {
			var rows []struct{ Value string }
			if err := tx.Raw("SELECT value FROM options WHERE key = ?", key).Scan(&rows).Error; err != nil {
				return err
			}
			values := map[string]float64{}
			defaults := map[string]string{
				"ModelRatio": gatewaystore.ModelRatio2JSONString(), "CompletionRatio": gatewaystore.CompletionRatio2JSONString(), "CacheRatio": gatewaystore.CacheRatio2JSONString(),
			}
			encoded := defaults[key]
			if len(rows) > 0 {
				encoded = rows[0].Value
			}
			if err := json.Unmarshal([]byte(encoded), &values); err != nil {
				return fmt.Errorf("decode fixture %s: %w", key, err)
			}
			if values == nil {
				return fmt.Errorf("fixture %s must be a JSON object", key)
			}
			for model, ratio := range additions {
				values[model] = ratio
			}
			value, err := json.Marshal(values)
			if err != nil {
				return err
			}
			if err := tx.Exec("INSERT INTO options (key,value) VALUES (?,?) ON CONFLICT (key) DO UPDATE SET value=excluded.value", key, string(value)).Error; err != nil {
				return err
			}
		}
		fmt.Println("prepared local aliases: gpt-6.1-sol, gpt-6-astra")
		return nil
	})
}
