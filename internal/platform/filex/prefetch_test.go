package filex

import (
	"encoding/base64"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/types"
)

func TestPrefetchFileSourcesDeduplicatesAndSharesRequestCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)

	// 1x1 transparent PNG; keeping this inline makes the test independent of
	// network access and exercises the same base64 decode/cache path used for
	// inline images in chat requests.
	data := base64.StdEncoding.EncodeToString([]byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	})
	sourceA := types.NewBase64FileSource(data, "image/png")
	sourceB := types.NewBase64FileSource(data, "image/png")

	PrefetchFileSources(ctx, []types.FileSource{sourceA, sourceB}, "test")
	if !sourceA.HasCache() {
		t.Fatal("expected first distinct source to be prefetched")
	}
	if sourceB.HasCache() {
		t.Fatal("duplicate source should be served from request cache on demand")
	}
	if _, err := LoadFileSource(ctx, sourceB, "test"); err != nil {
		t.Fatalf("expected duplicate source to hit request cache: %v", err)
	}
}
