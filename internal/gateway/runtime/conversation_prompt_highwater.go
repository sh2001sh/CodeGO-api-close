package runtime

import (
	"strings"
	"sync"
	"time"

	"github.com/samber/hot"
	"github.com/sh2001sh/new-api/dto"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	"github.com/sh2001sh/new-api/internal/platform/cachex"
)

const (
	conversationPromptHighWaterNamespace  = "new-api:conversation_prompt_high_water:v1"
	conversationPromptHighWaterMinimumTTL = 24 * time.Hour
)

var (
	conversationPromptHighWaterOnce  sync.Once
	conversationPromptHighWaterCache *cachex.HybridCache[int]
)

// ObserveConversationPromptHighWater records only the largest upstream prompt
// token count seen for an affinity-scoped GPT conversation. It intentionally
// does not retain request or response content.
func ObserveConversationPromptHighWater(statsCtx ChannelAffinityStatsContext, model string, usage *dto.Usage) {
	if !isGPTModel(model) || usage == nil || statsCtx.TTLSeconds <= 0 {
		return
	}
	promptTokens := usagePromptTokens(usage)
	if promptTokens <= 0 {
		return
	}

	entryKey := conversationPromptHighWaterEntryKey(statsCtx, model)
	if entryKey == "" {
		return
	}
	lock := channelAffinityUsageCacheStatsLock(entryKey)
	lock.Lock()
	defer lock.Unlock()

	cache := getConversationPromptHighWaterCache()
	previous, found, err := cache.Get(entryKey)
	if err != nil {
		return
	}
	if found && previous > promptTokens {
		promptTokens = previous
	}
	_ = cache.SetWithTTL(entryKey, promptTokens, conversationPromptHighWaterTTL(statsCtx))
}

// ConversationPromptHighWater returns the observed upstream prompt-token
// high-water mark for a GPT affinity context. The cache key contains only
// rule, group, model, and an existing hashed affinity fingerprint.
func ConversationPromptHighWater(statsCtx ChannelAffinityStatsContext, model string) int {
	entryKey := conversationPromptHighWaterEntryKey(statsCtx, model)
	if entryKey == "" {
		return 0
	}
	value, found, err := getConversationPromptHighWaterCache().Get(entryKey)
	if err != nil || !found {
		return 0
	}
	return value
}

func ResetConversationPromptHighWaterForTest() error {
	if conversationPromptHighWaterCache != nil {
		if err := conversationPromptHighWaterCache.Purge(); err != nil {
			return err
		}
	}
	conversationPromptHighWaterOnce = sync.Once{}
	conversationPromptHighWaterCache = nil
	return nil
}

func conversationPromptHighWaterEntryKey(statsCtx ChannelAffinityStatsContext, model string) string {
	baseKey := channelAffinityUsageCacheEntryKey(statsCtx.RuleName, statsCtx.UsingGroup, statsCtx.KeyFingerprint)
	model = strings.ToLower(strings.TrimSpace(model))
	if baseKey == "" || !isGPTModel(model) {
		return ""
	}
	return baseKey + "\n" + model
}

func getConversationPromptHighWaterCache() *cachex.HybridCache[int] {
	conversationPromptHighWaterOnce.Do(func() {
		capacity, _ := channelAffinityCacheCapacityAndTTL()
		conversationPromptHighWaterCache = cachex.NewHybridCache[int](cachex.HybridCacheConfig[int]{
			Namespace: cachex.Namespace(conversationPromptHighWaterNamespace),
			Redis:     platformcache.RDB,
			RedisEnabled: func() bool {
				return platformcache.RedisEnabled && platformcache.RDB != nil
			},
			RedisCodec: cachex.IntCodec{},
			Memory: func() *hot.HotCache[string, int] {
				return hot.NewHotCache[string, int](hot.LRU, capacity).
					WithTTL(conversationPromptHighWaterMinimumTTL).
					WithJanitor().
					Build()
			},
		})
	})
	return conversationPromptHighWaterCache
}

func conversationPromptHighWaterTTL(statsCtx ChannelAffinityStatsContext) time.Duration {
	ttl := time.Duration(statsCtx.TTLSeconds) * time.Second
	if ttl < conversationPromptHighWaterMinimumTTL {
		return conversationPromptHighWaterMinimumTTL
	}
	return ttl
}
