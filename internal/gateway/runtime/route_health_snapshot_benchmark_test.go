package runtime

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
)

func BenchmarkLoadRouteHealthSnapshot(b *testing.B) {
	redisEnabled := platformcache.RedisEnabled
	platformcache.RedisEnabled = false
	b.Cleanup(func() { platformcache.RedisEnabled = redisEnabled })
	for _, size := range []int{32, 256, 1024} {
		b.Run(fmt.Sprintf("members=%d", size), func(b *testing.B) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Set(string(constant.ContextKeyUserId), 42)
			ids := make([]int, size)
			domains := make([]string, size)
			for i := range ids {
				ids[i] = 1_100_000 + i
				domains[i] = fmt.Sprintf("provider:%d", i%32)
				RecordChannelSuccess(ids[i], "gpt-snapshot-benchmark", 250*time.Millisecond, RequestTypeChatLongStream)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if snapshot, err := LoadRouteHealthSnapshot(ctx, ids, domains, "gpt-snapshot-benchmark", RequestTypeChatLongStream); err != nil || len(snapshot.Channels) != size {
					b.Fatalf("snapshot channels=%d err=%v", len(snapshot.Channels), err)
				}
			}
		})
	}
}
