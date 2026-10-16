package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func channelCacheTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&gatewayschema.Channel{}, &gatewayschema.Ability{}))
	oldDB, oldMemory, oldGroups, oldChannels := platformdb.DB, platformconfig.MemoryCacheEnabled, group2model2channels, channelsIDM
	platformdb.DB, platformconfig.MemoryCacheEnabled = db, true
	t.Cleanup(func() {
		platformdb.DB, platformconfig.MemoryCacheEnabled, group2model2channels, channelsIDM = oldDB, oldMemory, oldGroups, oldChannels
		sqlDB.Close()
	})
	return db
}

func TestChannelCacheKeepsLastSnapshotOnDatabaseFailure(t *testing.T) {
	db := channelCacheTestDB(t)
	require.NoError(t, db.Create(&gatewayschema.Channel{Id: 10, Key: "cache-test", Status: 1, Group: "restored", Models: "gpt-5"}).Error)
	InitChannelCache()
	require.True(t, IsChannelEnabledForGroupModel("restored", "gpt-5", 10))
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("fail_cache_refresh", func(tx *gorm.DB) {
		if tx.Statement.Table == "abilities" {
			tx.AddError(errors.New("injected database failure"))
		}
	}))
	InitChannelCache()
	require.True(t, IsChannelEnabledForGroupModel("restored", "gpt-5", 10), "a failed refresh must not publish an empty routing cache")
}

// Use a dedicated ephemeral Redis, never a production cache.
func TestChannelCacheRefreshesAcrossInstances(t *testing.T) {
	addr := os.Getenv("TEST_GATEWAY_REDIS_ADDR")
	if addr == "" {
		t.Skip("set TEST_GATEWAY_REDIS_ADDR to an isolated Redis fixture")
	}
	db := channelCacheTestDB(t)
	client := redis.NewClient(&redis.Options{Addr: addr})
	oldClient, oldEnabled := platformcache.RDB, platformcache.RedisEnabled
	platformcache.RDB, platformcache.RedisEnabled = client, true
	t.Cleanup(func() { platformcache.RDB, platformcache.RedisEnabled = oldClient, oldEnabled; client.Close() })
	channel := gatewayschema.Channel{Id: 11, Key: "cache-test", Status: 2, Group: "restored", Models: "gpt-5"}
	require.NoError(t, db.Create(&channel).Error)
	InitChannelCache()
	require.False(t, IsChannelEnabledForGroupModel("restored", "gpt-5", channel.Id))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); syncChannelCache(ctx, 60) }()
	t.Cleanup(func() { cancel(); <-done })
	require.Eventually(t, func() bool {
		n, err := client.PubSubNumSub(context.Background(), channelCacheChangesTopic).Result()
		return err == nil && n[channelCacheChangesTopic] > 0
	}, 3*time.Second, 10*time.Millisecond)
	require.NoError(t, db.Model(&channel).Update("status", 1).Error)
	NotifyChannelCacheChanged()
	require.Eventually(t, func() bool { return IsChannelEnabledForGroupModel("restored", "gpt-5", channel.Id) }, 2*time.Second, 10*time.Millisecond)
	t.Log("resume event refreshed routing before the 60-second periodic sync")
}
