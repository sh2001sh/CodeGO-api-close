package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/constant"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformstore "github.com/sh2001sh/new-api/internal/platform/store"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const fixtureDSN = "postgres://loadtest@127.0.0.1:55437/codego_stream_route_20261016?sslmode=disable"
const fixtureDatabaseName = "codego_stream_route_20261016"
const fixtureWalletAmount = 2_000_000_000 // Fits the legacy integer projection.

var fixtureProtocols = []struct {
	name, short, model string
	channelType        int
}{
	{"openai", "o", "gpt-4.1", constant.ChannelTypeOpenAI},
	{"claude", "c", "claude-sonnet-4", constant.ChannelTypeAnthropic},
	{"gemini", "g", "gemini-2.5-flash", constant.ChannelTypeGemini},
}

var fixtureAliases = []string{"sb-chat-to-responses", "sb-responses-to-chat", "sb-ws-sse", "sb-ws-native"}

// Logical route names select distinct tokens on the same user's funded wallet.
// For u1/u10 routes the persisted token group is a private named-pool identifier.
// These deterministic credentials are fake and valid only in this fixture DB.
func fixtureKey(user int, group string) string {
	digest := sha256.Sum256([]byte(group))
	return fmt.Sprintf("localstreamfixture%03d%x", user, digest)
}

func fixtureDB() (*gorm.DB, error) {
	platformdb.UsingPostgreSQL, platformdb.UsingSQLite = true, false
	db, err := gorm.Open(postgres.Open(fixtureDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(8)
	sqlDB.SetMaxIdleConns(4)
	return db, nil
}

func seedFixtures() error {
	// The target is fixed; SQL_DSN, REDIS_CONN_STRING and .env are never read.
	admin, err := gorm.Open(postgres.Open("postgres://loadtest@127.0.0.1:55437/postgres?sslmode=disable"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return err
	}
	sqlAdmin, err := admin.DB()
	if err != nil {
		return err
	}
	defer sqlAdmin.Close()
	var exists bool
	if err = admin.Raw("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = ?)", fixtureDatabaseName).Scan(&exists).Error; err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("fixture database %s already exists; refusing to overwrite it", fixtureDatabaseName)
	}
	if err = admin.Exec("CREATE DATABASE " + fixtureDatabaseName).Error; err != nil {
		return err
	}
	db, err := fixtureDB()
	if err != nil {
		return err
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	platformdb.DB, platformdb.LogDB = db, db
	platformconfig.IsMasterNode = true
	if err = platformstore.BootstrapPrimarySchema(); err != nil {
		return err
	}
	if err = platformstore.ApplyV2Migrations(context.Background(), false); err != nil {
		return err
	}

	groupRatios := map[string]float64{"default": 1}
	usableGroups := map[string]string{}
	for _, protocol := range fixtureProtocols {
		for _, suffix := range []string{"direct", "p32", "p256", "p1024"} {
			group := "sb-" + protocol.name + "-" + suffix
			groupRatios[group], usableGroups[group] = 1, group
		}
		for index := 1; index <= 10; index++ {
			group := fmt.Sprintf("s%s%d", protocol.short, index)
			groupRatios[group], usableGroups[group] = 1, group
		}
	}
	for _, alias := range fixtureAliases {
		groupRatios[alias], usableGroups[alias] = 1, alias
	}
	encode := func(value any) string {
		data, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			panic(marshalErr) // All call sites contain JSON primitives only.
		}
		return string(data)
	}
	// Match the repository's paid model prices, including an explicit Sonnet
	// alias. Every route charges the same nonzero model/group ratio. No auth or
	// wallet billing bypass is enabled; only the request-rate cap is disabled.
	modelRatios := gatewaystore.GetModelRatioCopy()
	modelRatios["gpt-4.1"], modelRatios["claude-sonnet-4"], modelRatios["gemini-2.5-flash"] = 1, 1.5, 0.15
	completionRatios := gatewaystore.GetCompletionRatioCopy()
	completionRatios["gpt-4.1"], completionRatios["claude-sonnet-4"], completionRatios["gemini-2.5-flash"] = 4, 5, 2.5/0.3
	cacheRatios := gatewaystore.GetCacheRatioCopy()
	cacheRatios["gpt-4.1"], cacheRatios["claude-sonnet-4"], cacheRatios["gemini-2.5-flash"] = 0.25, 0.1, 0.1
	createCacheRatios := gatewaystore.GetCreateCacheRatioCopy()
	createCacheRatios["claude-sonnet-4"] = 1.25
	for _, alias := range fixtureAliases {
		modelRatios[alias], completionRatios[alias], cacheRatios[alias] = 1, 4, 0.25
	}
	options := map[string]string{
		"billing_schema_version":       commerceschema.UnifiedCreditMigrationVersion,
		"ModelRequestRateLimitEnabled": "false", "PerformanceMonitorEnabled": "false",
		"AutomaticDisableChannelEnabled": "false", "AutomaticEnableChannelEnabled": "false",
		"GroupRatio": encode(groupRatios), "UserUsableGroups": encode(usableGroups),
		"ModelRatio": encode(modelRatios), "CompletionRatio": encode(completionRatios), "CacheRatio": encode(cacheRatios), "CreateCacheRatio": encode(createCacheRatios),
		"global.chat_completions_to_responses_policy": encode(gatewaystore.ProtocolBridgePolicy{Mode: gatewaystore.ProtocolBridgeModeForce, AllChannels: true, ModelPatterns: []string{"^sb-chat-to-responses$"}}),
		"global.responses_to_chat_completions_policy": encode(gatewaystore.ProtocolBridgePolicy{Mode: gatewaystore.ProtocolBridgeModeForce, AllChannels: true, ModelPatterns: []string{"^sb-responses-to-chat$"}}),
	}
	for key, value := range options {
		if err = platformstore.UpdateOption(key, value); err != nil {
			return fmt.Errorf("fixture option %s: %w", key, err)
		}
	}
	for userIndex := 1; userIndex <= 128; userIndex++ {
		user := identityschema.User{Id: 10000 + userIndex, Username: fmt.Sprintf("streambench%d", userIndex), Password: "unused-local-fixture", DisplayName: "local stream fixture", Role: 1, Status: 1, Quota: fixtureWalletAmount, ClaudeQuota: fixtureWalletAmount, Group: "default", AffCode: fmt.Sprintf("streambench%d", userIndex)}
		if err = db.Create(&user).Error; err != nil {
			return err
		}
		account, accountErr := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "claude_wallet", OwnerType: "user", OwnerID: int64(user.Id), QuotaUnit: "quota"})
		if accountErr != nil {
			return accountErr
		}
		if _, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: fixtureWalletAmount, IdempotencyKey: fmt.Sprintf("streambench-seed-%d", userIndex), ReasonCode: "local_fixture"}); err != nil {
			return err
		}
	}

	base := "http://127.0.0.1:18181"
	for protocolIndex, protocol := range fixtureProtocols {
		prefix := "sb-" + protocol.name + "-"
		if err = db.Transaction(func(tx *gorm.DB) error {
			channels := make([]gatewayschema.Channel, 0, 1056)
			for index := 1; index <= 1024; index++ {
				groups := []string{prefix + "p1024"}
				if index <= 256 {
					groups = append(groups, prefix+"p256")
				}
				if index <= 32 {
					groups = append(groups, prefix+"p32", prefix+"direct")
				}
				channel := gatewayschema.Channel{Id: 20000 + protocolIndex*2000 + index, Type: protocol.channelType, Key: "local-stream-upstream-fixture", Status: 1, Name: fmt.Sprintf("stream-%s-%d", protocol.name, index), Group: strings.Join(groups, ","), Models: protocol.model, BaseURL: &base, ChannelScope: gatewayschema.ChannelScopeOfficial, CreatedTime: time.Now().Unix()}
				if len(channel.Group) > 64 {
					return fmt.Errorf("fixture group list exceeds schema limit: %s", channel.Group)
				}
				channels = append(channels, channel)
			}
			// Ten short official group aliases share a separate 32-channel set.
			// These isolate named-pool group count from official channel count.
			var shortGroups []string
			for index := 1; index <= 10; index++ {
				shortGroups = append(shortGroups, fmt.Sprintf("s%s%d", protocol.short, index))
			}
			for index := 1; index <= 32; index++ {
				channels = append(channels, gatewayschema.Channel{Id: 20000 + protocolIndex*2000 + 1100 + index, Type: protocol.channelType, Key: "local-stream-upstream-fixture", Status: 1, Name: fmt.Sprintf("stream-%s-userpool-%d", protocol.name, index), Group: strings.Join(shortGroups, ","), Models: protocol.model, BaseURL: &base, ChannelScope: gatewayschema.ChannelScopeOfficial, CreatedTime: time.Now().Unix()})
			}
			if createErr := tx.CreateInBatches(channels, 128).Error; createErr != nil {
				return createErr
			}
			for index := range channels {
				if abilityErr := gatewaystore.AddChannelAbilities(&channels[index], tx); abilityErr != nil {
					return abilityErr
				}
			}
			return nil
		}); err != nil {
			return err
		}
		for _, size := range []int{32, 256, 1024} {
			members := make([]gatewayschema.RoutePoolMember, size)
			for index := range members {
				members[index] = gatewayschema.RoutePoolMember{ChannelID: 20001 + protocolIndex*2000 + index, Enabled: true, CostMultiplier: 1, ModelCostOverrides: "{}", FaultDomain: fmt.Sprintf("stream-%s-%d", protocol.name, index+1)}
			}
			group := fmt.Sprintf("%sp%d", prefix, size)
			if _, err = gatewaystore.SaveRoutePool(&gatewayschema.RoutePool{Name: group, Group: group, ModelScope: protocol.model, Enabled: true}, members); err != nil {
				return err
			}
		}
		fmt.Printf("seeded %s: direct32, official pools32/256/1024, ten shared user-pool groups\n", protocol.name)
	}
	for index, alias := range fixtureAliases {
		mapping := encode(map[string]string{alias: "gpt-4.1"})
		channel := gatewayschema.Channel{Id: 30001 + index, Type: constant.ChannelTypeOpenAI, Key: "local-stream-upstream-fixture", Status: 1, Name: alias, Group: alias, Models: alias, ModelMapping: &mapping, BaseURL: &base, ChannelScope: gatewayschema.ChannelScopeOfficial, CreatedTime: time.Now().Unix()}
		if alias == "sb-ws-native" || alias == "sb-ws-sse" {
			status := gatewayschema.CapabilityStatusUnsupported
			if alias == "sb-ws-native" {
				status = gatewayschema.CapabilityStatusSupported
			}
			channel.ChannelInfo.ResponsesCapabilities.WebSocket = gatewayschema.CapabilityProbeState{Status: status, CheckedAt: time.Now().Unix(), Model: alias, ProbeKeyIdx: 0}
		}
		if err = db.Create(&channel).Error; err != nil {
			return err
		}
		if err = gatewaystore.AddChannelAbilities(&channel, db); err != nil {
			return err
		}
	}
	gatewaystore.InitChannelCache()
	for userIndex := 1; userIndex <= 128; userIndex++ {
		for _, protocol := range fixtureProtocols {
			for _, suffix := range []string{"direct", "p32", "p256", "p1024"} {
				group := "sb-" + protocol.name + "-" + suffix
				if err = seedFixtureToken(db, userIndex, group, group); err != nil {
					return err
				}
			}
			for _, size := range []int{1, 10} {
				var groupIDs []string
				for groupIndex := 1; groupIndex <= size; groupIndex++ {
					groupIDs = append(groupIDs, fmt.Sprintf("official:s%s%d", protocol.short, groupIndex))
				}
				logicalRoute := fmt.Sprintf("sb-%s-u%d", protocol.name, size)
				pool, poolErr := marketplaceapp.CreateRoutePool(10000+userIndex, marketplaceapp.RoutePoolCreateRequest{Name: logicalRoute, GroupIDs: groupIDs, Config: &marketplaceapp.AutoRoutePoolConfig{Strategy: "score", MaxAttempts: 3, FailureCooldownSeconds: 30}})
				if poolErr != nil {
					return fmt.Errorf("user %d pool %s: %w", userIndex, logicalRoute, poolErr)
				}
				if pool.SelectedCount != size {
					return fmt.Errorf("user %d pool %s expected %d accepted groups, got %d", userIndex, logicalRoute, size, pool.SelectedCount)
				}
				if err = seedFixtureToken(db, userIndex, logicalRoute, pool.TokenGroup); err != nil {
					return err
				}
			}
		}
		for _, alias := range fixtureAliases {
			if err = seedFixtureToken(db, userIndex, alias, alias); err != nil {
				return err
			}
		}
	}
	if err = prepareCapacityFixtures(); err != nil {
		return err
	}
	fmt.Println("seeded isolated stream DB: 128 funded users, 2944 tokens, 3174 official channels, 10 official pools, 768 private user pools")
	return nil
}

func seedFixtureToken(db *gorm.DB, userIndex int, logicalRoute, tokenGroup string) error {
	return db.Create(&identityschema.Token{UserId: 10000 + userIndex, Key: fixtureKey(userIndex, logicalRoute), Name: logicalRoute, Status: 1, UnlimitedQuota: true, ExpiredTime: -1, Group: tokenGroup, CrossGroupRetry: false}).Error
}

// Only adds a known, separate fixture; it never changes the fair 32/256/1024
// channel sets. This can finish preparation when the initial seed already ran.
func prepareCapacityFixtures() error {
	db, err := fixtureDB()
	if err != nil {
		return err
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	platformdb.DB, platformdb.LogDB = db, db
	const group = "sb-openai-cap2"
	for _, optionKey := range []string{"GroupRatio", "UserUsableGroups"} {
		var value string
		if err = db.Raw("SELECT value FROM options WHERE key=?", optionKey).Scan(&value).Error; err != nil {
			return err
		}
		values := map[string]any{}
		if err = json.Unmarshal([]byte(value), &values); err != nil {
			return err
		}
		if optionKey == "GroupRatio" {
			values[group] = 1
		} else {
			values[group] = group
		}
		encoded, encodeErr := json.Marshal(values)
		if encodeErr != nil {
			return encodeErr
		}
		if err = platformstore.UpdateOption(optionKey, string(encoded)); err != nil {
			return err
		}
	}
	base := "http://127.0.0.1:18181"
	for index := 1; index <= 2; index++ {
		channel := gatewayschema.Channel{Id: 31000 + index, Type: constant.ChannelTypeOpenAI, Key: "local-stream-upstream-fixture", Status: 1, Name: fmt.Sprintf("stream-capacity-%d", index), Group: group, Models: "gpt-4.1", BaseURL: &base, ChannelScope: gatewayschema.ChannelScopeOfficial, MarketplaceMaxConcurrency: 1, CreatedTime: time.Now().Unix()}
		var existing gatewayschema.Channel
		loadErr := db.First(&existing, channel.Id).Error
		if loadErr == nil {
			if existing.Name != channel.Name || existing.Group != group || existing.MarketplaceMaxConcurrency != 1 || existing.BaseURL == nil || *existing.BaseURL != base {
				return fmt.Errorf("capacity fixture channel %d has unexpected settings", channel.Id)
			}
		} else if errors.Is(loadErr, gorm.ErrRecordNotFound) {
			if err = db.Create(&channel).Error; err != nil {
				return err
			}
		} else {
			return loadErr
		}
		if err = gatewaystore.AddChannelAbilities(&channel, db); err != nil {
			return err
		}
	}
	pool := gatewayschema.RoutePool{Name: group, Group: group, ModelScope: "gpt-4.1", Enabled: true}
	loadErr := db.Where("name=?", group).First(&pool).Error
	if loadErr != nil && !errors.Is(loadErr, gorm.ErrRecordNotFound) {
		return loadErr
	}
	if _, err = gatewaystore.SaveRoutePool(&pool, []gatewayschema.RoutePoolMember{
		{ChannelID: 31001, Enabled: true, CostMultiplier: 1, ModelCostOverrides: "{}", FaultDomain: "stream-capacity-1"},
		{ChannelID: 31002, Enabled: true, CostMultiplier: 1, ModelCostOverrides: "{}", FaultDomain: "stream-capacity-2"},
	}); err != nil {
		return err
	}
	for userIndex := 1; userIndex <= 128; userIndex++ {
		token := identityschema.Token{UserId: 10000 + userIndex, Key: fixtureKey(userIndex, group), Name: group, Group: group, Status: 1, UnlimitedQuota: true, ExpiredTime: -1}
		var existing identityschema.Token
		loadErr = db.Where("key=?", token.Key).First(&existing).Error
		if errors.Is(loadErr, gorm.ErrRecordNotFound) {
			if err = db.Create(&token).Error; err != nil {
				return err
			}
		} else if loadErr != nil {
			return loadErr
		} else if existing.UserId != token.UserId || existing.Group != group {
			return fmt.Errorf("capacity fixture token %d belongs to unexpected identity/group", userIndex)
		}
	}
	fmt.Println("prepared isolated capacity fixture: sb-openai-cap2, 2 channels x1 concurrency, 128 tokens")
	return nil
}

func checkFixtures() error {
	db, err := fixtureDB()
	if err != nil {
		return err
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	var database string
	if err = db.Raw("SELECT current_database()").Scan(&database).Error; err != nil {
		return err
	}
	if database != fixtureDatabaseName {
		return fmt.Errorf("unexpected fixture database %q", database)
	}
	for _, query := range []string{
		"SELECT current_database() AS database, inet_server_addr() AS address, current_setting('port') AS port, current_setting('max_connections') AS max_connections",
		"SELECT status, count(*) FROM billing.reservations GROUP BY status ORDER BY status",
		"SELECT count(*) AS ledger_entries FROM billing.ledger_entries",
		"SELECT count(*) AS pending_events, COALESCE(round(extract(epoch FROM now()-min(created_at))::numeric,2),0) AS oldest_pending_seconds FROM billing.outbox_events WHERE status = 'pending'",
		`SELECT type, channel_id, model_name, count(*) AS requests, sum(quota) AS quota FROM logs WHERE type=2 GROUP BY type,channel_id,model_name ORDER BY model_name,channel_id`,
		"SELECT state, wait_event_type, count(*) FROM pg_stat_activity WHERE datname = 'codego_stream_route_20261016' GROUP BY state,wait_event_type",
	} {
		var rows []map[string]any
		if err = db.Raw(query).Scan(&rows).Error; err != nil {
			return err
		}
		encoded, marshalErr := json.Marshal(rows)
		if marshalErr != nil {
			return marshalErr
		}
		fmt.Printf("%s\n%s\n", query, encoded)
	}
	checks := []struct{ name, query string }{
		{"negative_snapshots", "SELECT count(*) FROM billing.balance_snapshots WHERE available_balance < 0 OR reserved_balance < 0"},
		{"balance_conservation", "SELECT count(*) FROM billing.balance_snapshots WHERE available_balance + reserved_balance + consumed_total <> granted_total"},
		{"settled_without_settlement", "SELECT count(*) FROM billing.reservations r LEFT JOIN billing.settlements s USING(reservation_id) WHERE r.status='settled' AND s.settlement_id IS NULL"},
		{"reservation_balance", "SELECT count(*) FROM billing.balance_snapshots b WHERE b.reserved_balance <> COALESCE((SELECT sum(r.reserved_amount) FROM billing.reservations r WHERE r.account_id=b.account_id AND r.status='open'),0)"},
		{"settlement_total", "SELECT count(*) FROM billing.balance_snapshots b WHERE b.consumed_total <> COALESCE((SELECT sum(s.actual_amount) FROM billing.settlements s JOIN billing.reservations r USING(reservation_id) WHERE r.account_id=b.account_id AND r.status='settled'),0)"},
		{"foreign_pool_tokens", `SELECT count(*) FROM tokens t LEFT JOIN marketplace.route_pools p ON t."group"='market:pool:'||p.id WHERE t."group" LIKE 'market:pool:%' AND (p.id IS NULL OR p.owner_user_id<>t.user_id)`},
		{"external_upstreams", "SELECT count(*) FROM channels WHERE base_url <> 'http://127.0.0.1:18181' OR channel_scope <> 'official'"},
		{"fair_channel_caps", "SELECT count(*) FROM channels WHERE id NOT IN (31001,31002) AND marketplace_max_concurrency <> 0"},
	}
	for _, check := range checks {
		var count int64
		if err = db.Raw(check.query).Scan(&count).Error; err != nil {
			return err
		}
		fmt.Printf("fixture invariant %s: %d\n", check.name, count)
		if count != 0 {
			return fmt.Errorf("fixture invariant %s failed: %d", check.name, count)
		}
	}
	for _, countCheck := range []struct {
		model any
		where string
		want  int64
	}{
		{&identityschema.User{}, "id BETWEEN 10001 AND 10128 AND username LIKE 'streambench%'", 128},
		{&identityschema.Token{}, "user_id BETWEEN 10001 AND 10128 AND name LIKE 'sb-%'", 2944},
		{&gatewayschema.Channel{}, "name LIKE 'stream-%' OR name IN ('sb-chat-to-responses','sb-responses-to-chat','sb-ws-sse','sb-ws-native')", 3174},
		{&gatewayschema.RoutePool{}, "name LIKE 'sb-%'", 10},
		{&marketplaceschema.RoutePool{}, "owner_user_id BETWEEN 10001 AND 10128 AND name LIKE 'sb-%'", 768},
	} {
		var count int64
		if err = db.Model(countCheck.model).Where(countCheck.where).Count(&count).Error; err != nil {
			return err
		}
		fmt.Printf("fixture shape %T: %d\n", countCheck.model, count)
		if count != countCheck.want {
			return fmt.Errorf("fixture %T count=%d, want=%d", countCheck.model, count, countCheck.want)
		}
	}
	fmt.Println("fixture shape and accounting invariants PASS")
	return nil
}
