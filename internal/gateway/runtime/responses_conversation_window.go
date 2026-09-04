package runtime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/samber/hot"
	"github.com/sh2001sh/new-api/constant"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	"github.com/sh2001sh/new-api/internal/platform/cachex"
)

const responsesConversationCandidateKey = "responses_conversation_window_candidate"

const responsesConversationCacheReadTimeout = 40 * time.Millisecond

type responsesConversationWindowEntry struct {
	PrefixHashes  []string `json:"prefix_hashes"`
	ResponseID    string   `json:"response_id"`
	ChannelID     int      `json:"channel_id"`
	MultiKeyIndex int      `json:"multi_key_index"`
}

type responsesConversationCandidate struct {
	mu            sync.Mutex
	cacheKey      string
	inputHashes   []string
	channelID     int
	multiKeyIndex int
	responseID    string
	outputHashes  []string
}

var (
	responsesConversationCacheOnce sync.Once
	responsesConversationCache     *cachex.HybridCache[responsesConversationWindowEntry]
)

func responsesConversationWindowTTL() time.Duration {
	seconds := constant.ResponsesConversationWindowSeconds
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func getResponsesConversationCache() *cachex.HybridCache[responsesConversationWindowEntry] {
	responsesConversationCacheOnce.Do(func() {
		responsesConversationCache = cachex.NewHybridCache[responsesConversationWindowEntry](cachex.HybridCacheConfig[responsesConversationWindowEntry]{
			Namespace:  cachex.Namespace("new-api:responses_conversation_window:v1"),
			Redis:      platformcache.RDB,
			RedisCodec: cachex.JSONCodec[responsesConversationWindowEntry]{},
			RedisEnabled: func() bool {
				return platformcache.RedisEnabled && platformcache.RDB != nil
			},
			Memory: func() *hot.HotCache[string, responsesConversationWindowEntry] {
				return hot.NewHotCache[string, responsesConversationWindowEntry](hot.LRU, 512).
					WithTTL(10 * time.Minute).
					WithJanitor().
					Build()
			},
		})
	})
	return responsesConversationCache
}

// PrepareResponsesConversationWindow replaces a repeated full-history prefix
// with previous_response_id plus the new input suffix. It only operates on
// native stored Responses conversations and keeps the original body for a
// transparent stale-state fallback.
func PrepareResponsesConversationWindow(c *gin.Context, info *RelayInfo, body []byte) (optimized []byte, fallback []byte, applied bool, err error) {
	if c == nil || info == nil || len(body) == 0 || responsesConversationWindowTTL() <= 0 {
		return body, nil, false, nil
	}
	var payload map[string]json.RawMessage
	if err = json.Unmarshal(body, &payload); err != nil {
		return body, nil, false, nil
	}
	if !rawJSONBool(payload["store"]) || len(bytes.TrimSpace(payload["previous_response_id"])) > 0 {
		return body, nil, false, nil
	}
	promptKey := rawJSONStringValue(payload["prompt_cache_key"])
	if promptKey == "" {
		return body, nil, false, nil
	}
	var input []json.RawMessage
	if err = json.Unmarshal(payload["input"], &input); err != nil || len(input) == 0 {
		return body, nil, false, nil
	}
	hashes := hashResponsesItems(input)
	cacheKey := responsesConversationCacheKey(info.UserId, info.OriginModelName, promptKey)
	c.Set(responsesConversationCandidateKey, &responsesConversationCandidate{
		cacheKey: cacheKey, inputHashes: hashes, channelID: info.ChannelId, multiKeyIndex: info.ChannelMultiKeyIndex,
	})
	entry, found, cacheErr := getResponsesConversationCache().GetWithTimeout(cacheKey, responsesConversationCacheReadTimeout)
	if cacheErr != nil || !found || entry.ResponseID == "" || entry.ChannelID != info.ChannelId || entry.MultiKeyIndex != info.ChannelMultiKeyIndex {
		return body, nil, false, nil
	}
	if len(entry.PrefixHashes) >= len(hashes) || !equalStringPrefix(hashes, entry.PrefixHashes) {
		return body, nil, false, nil
	}
	payload["input"], err = json.Marshal(input[len(entry.PrefixHashes):])
	if err != nil {
		return body, nil, false, err
	}
	payload["previous_response_id"], _ = json.Marshal(entry.ResponseID)
	optimized, err = json.Marshal(payload)
	if err != nil {
		return body, nil, false, err
	}
	return optimized, body, true, nil
}

// RecordResponsesConversationWindow advances the short-lived conversation
// anchor only after a completed upstream response has been observed.
func RecordResponsesConversationWindow(c *gin.Context, info *RelayInfo, raw []byte) {
	if c == nil || info == nil || len(raw) == 0 || responsesConversationWindowTTL() <= 0 {
		return
	}
	type responsePayload struct {
		ID     string            `json:"id"`
		Output []json.RawMessage `json:"output"`
	}
	type responseEvent struct {
		Type     string          `json:"type"`
		Item     json.RawMessage `json:"item"`
		Response json.RawMessage `json:"response"`
	}
	value, found := c.Get(responsesConversationCandidateKey)
	if !found {
		return
	}
	candidate, ok := value.(*responsesConversationCandidate)
	if !ok || candidate == nil || candidate.cacheKey == "" || candidate.channelID != info.ChannelId || candidate.multiKeyIndex != info.ChannelMultiKeyIndex {
		return
	}

	var event responseEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return
	}
	var response responsePayload
	if event.Type == "" {
		if json.Unmarshal(raw, &response) != nil {
			return
		}
	} else if len(event.Response) > 0 && json.Unmarshal(event.Response, &response) != nil {
		return
	}

	candidate.mu.Lock()
	defer candidate.mu.Unlock()
	if response.ID != "" {
		candidate.responseID = response.ID
	}
	if event.Type == "response.output_item.done" && len(event.Item) > 0 {
		candidate.outputHashes = append(candidate.outputHashes, hashResponsesItems([]json.RawMessage{event.Item})...)
	}
	completed := event.Type == "" || event.Type == "response.completed"
	if !completed {
		return
	}
	responseID := response.ID
	if responseID == "" {
		responseID = candidate.responseID
	}
	if responseID == "" {
		return
	}
	outputHashes := candidate.outputHashes
	if len(response.Output) > 0 {
		outputHashes = hashResponsesItems(response.Output)
	}
	prefix := append(append([]string(nil), candidate.inputHashes...), outputHashes...)
	_ = getResponsesConversationCache().SetWithTTL(candidate.cacheKey, responsesConversationWindowEntry{
		PrefixHashes: prefix, ResponseID: responseID, ChannelID: info.ChannelId, MultiKeyIndex: info.ChannelMultiKeyIndex,
	}, responsesConversationWindowTTL())
}

func responsesConversationCacheKey(userID int, model, promptKey string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s", userID, model, promptKey)))
	return hex.EncodeToString(sum[:])
}

func hashResponsesItems(items []json.RawMessage) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		var compact bytes.Buffer
		if json.Compact(&compact, item) != nil {
			compact.Write(item)
		}
		sum := sha256.Sum256(compact.Bytes())
		result = append(result, hex.EncodeToString(sum[:]))
	}
	return result
}

func equalStringPrefix(all, prefix []string) bool {
	if len(prefix) > len(all) {
		return false
	}
	for i := range prefix {
		if all[i] != prefix[i] {
			return false
		}
	}
	return true
}

func rawJSONStringValue(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}

func rawJSONBool(raw json.RawMessage) bool {
	var value bool
	_ = json.Unmarshal(raw, &value)
	return value
}
