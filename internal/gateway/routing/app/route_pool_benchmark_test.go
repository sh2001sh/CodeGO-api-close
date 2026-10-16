package app

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
)

// Includes a fresh advisory-health read, scoring and selection each round.
// The memory health backend isolates CPU/allocations from external Redis RTT;
// cross-instance concurrency I/O and database candidate loading are excluded.
func BenchmarkRoutePoolCandidateRound(b *testing.B) {
	redisEnabled := platformcache.RedisEnabled
	platformcache.RedisEnabled = false
	b.Cleanup(func() { platformcache.RedisEnabled = redisEnabled })
	const model = "gpt-route-pool-benchmark"
	pool := gatewayschema.RoutePool{MultiplierWeight: 35, TTFTWeight: 25, CacheWeight: 15, SuccessWeight: 25}
	for _, size := range []int{32, 256, 1024} {
		for _, override := range []bool{false, true} {
			b.Run(fmt.Sprintf("members=%d/overrides=%t", size, override), func(b *testing.B) {
				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				ctx.Set(string(constant.ContextKeyUserId), 42)
				candidates := make([]gatewaystore.RoutePoolCandidate, size)
				active := make(map[int]int, size)
				for i := range candidates {
					id := 1_000_000 + i
					member := gatewayschema.RoutePoolMember{ChannelID: id, CostMultiplier: 1 + float64(i%20)/100,
						FaultDomain: fmt.Sprintf("provider:%d", i%32), ModelCostOverrides: "{}"}
					if override {
						member.ModelCostOverrides = `{"gpt-route-pool-benchmark":0.85,"gpt-other":1.2}`
					}
					candidates[i] = gatewaystore.RoutePoolCandidate{Member: member, Channel: &gatewayschema.Channel{Id: id, Type: 1}}
					active[id] = i % 4
					gatewayruntime.RecordChannelSuccess(id, model, time.Duration(200+i%100)*time.Millisecond, gatewayruntime.RequestTypeChatLongStream)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					resetRouteHealthRequestCache(ctx)
					healthy, probes, lastResort := buildRoutePoolCandidateSets(ctx, candidates, model, gatewayruntime.RequestTypeChatLongStream, time.Now(), pool)
					applyRoutePoolConcurrencyPenaltySnapshot(active, healthy, probes, lastResort)
					applyRoutePoolTTFTPenalty(healthy, model)
					healthy = routePoolPreferredHealthTier(healthy)
					if selected := chooseRoutePoolHealthyCandidate(healthy, 0); selected == nil {
						b.Fatal("no selected candidate")
					}
				}
			})
		}
	}
}
