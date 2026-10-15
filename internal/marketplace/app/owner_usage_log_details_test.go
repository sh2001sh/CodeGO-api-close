package app

import (
	"testing"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
)

func TestOwnerUsageLogDetailsExposeCacheTokens(t *testing.T) {
	item := OwnerUsageLogItem{}
	applyOwnerUsageLogDetails(&item, auditschema.Log{Other: `{"cache_tokens":120,"cache_write_tokens":45}`})
	if item.CacheReadTokens != 120 || item.CacheWriteTokens != 45 {
		t.Fatalf("cache tokens = (%d, %d), want (120, 45)", item.CacheReadTokens, item.CacheWriteTokens)
	}

	item = OwnerUsageLogItem{}
	applyOwnerUsageLogDetails(&item, auditschema.Log{Other: `{}`})
	if item.CacheReadTokens != 0 || item.CacheWriteTokens != 0 {
		t.Fatalf("missing cache fields must remain zero: %+v", item)
	}
}
