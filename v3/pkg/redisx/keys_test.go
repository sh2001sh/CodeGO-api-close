package redisx

import (
	"strings"
	"testing"
)

// Key names are a cross-process contract: gateways, workers and Lua scripts
// must agree on them byte for byte. Two names colliding, or one escaping the
// v3: namespace, would mix data between unrelated features or with v2.
func TestKeyNamesAreNamespacedAndDistinct(t *testing.T) {
	keys := map[string]string{
		"KeyConcurrencyPrefix":    KeyConcurrencyPrefix,
		"KeyUserRPMPrefix":        KeyUserRPMPrefix,
		"KeyUserRPMRequestPrefix": KeyUserRPMRequestPrefix,
		"ChannelInvalidate":       ChannelInvalidate,
		"ChannelSnapshot":         ChannelSnapshot,
		"KeySnapshotPrefix":       KeySnapshotPrefix,
		"KeyAPIKeyPrefix":         KeyAPIKeyPrefix,
		"KeyAPIKeyUserPrefix":     KeyAPIKeyUserPrefix,
		"KeyAPIKeyIDPrefix":       KeyAPIKeyIDPrefix,
		"KeyAPIKeyTombPrefix":     KeyAPIKeyTombPrefix,
		"KeyBalancePrefix":        KeyBalancePrefix,
		"KeyReservationPrefix":    KeyReservationPrefix,
		"KeyReservationOpen":      KeyReservationOpen,
		"KeyPostingOpen":          KeyPostingOpen,
		"StreamBillingEvents":     StreamBillingEvents,
	}
	seen := make(map[string]string, len(keys))
	for name, key := range keys {
		if !strings.HasPrefix(key, "v3:") {
			t.Errorf("%s = %q is outside the v3: namespace", name, key)
		}
		if other, dup := seen[key]; dup {
			t.Errorf("%s and %s share the value %q", name, other, key)
		}
		seen[key] = name
	}
	if GroupLedger == "" {
		t.Error("GroupLedger must not be empty")
	}
}

// The API key L2 entry is KeyAPIKeyPrefix + hex(sha256). The sub-prefixes for
// the user set, the id map and tombstones also start with KeyAPIKeyPrefix, so
// they must not be parseable as a hex digest, or an invalidation scan over
// the prefix would treat them as profiles.
func TestAPIKeySubPrefixesCannotLookLikeHashes(t *testing.T) {
	for _, sub := range []string{KeyAPIKeyUserPrefix, KeyAPIKeyIDPrefix, KeyAPIKeyTombPrefix} {
		rest := strings.TrimPrefix(sub, KeyAPIKeyPrefix)
		if rest == sub {
			t.Fatalf("%q is not under %q", sub, KeyAPIKeyPrefix)
		}
		if !strings.ContainsAny(rest, ":") || strings.Trim(rest, "0123456789abcdef:") == "" {
			t.Errorf("sub-prefix %q could collide with a hex digest key", sub)
		}
	}
}
