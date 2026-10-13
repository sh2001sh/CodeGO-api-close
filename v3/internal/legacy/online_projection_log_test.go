package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestOnlineLogOmittedMetadataKeepsExactProjection(t *testing.T) {
	p := &onlineProjector{users: map[int64]bool{7: true}, mappings: map[fundingAccountKey]int64{{"user", "wallet", 7}: 17}, promptAnomalySchema: true}
	for _, tc := range []struct {
		name   string
		kind   int
		prompt int64
		other  string
	}{
		{"wide_object", 2, 23, `{"cached_tokens":23,"precise":9007199254740993,"payload":"` + strings.Repeat("中文\\\"", 8192) + `"}`},
		{"witnessed_underflow", 2, -5, `{"usage_semantic":"anthropic","cache_creation_tokens":10}`},
		{"array_metadata", 1, 23, `[null,9007199254740993,{"nested":true}]`},
		{"empty_metadata", 0, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{"id": 51, "user_id": 7, "created_at": 1700000000, "type": tc.kind, "content": "保留原文", "quota": 17, "prompt_tokens": tc.prompt, "completion_tokens": 41, "other": tc.other, "request_id": "original-request"})
			if err != nil {
				t.Fatal(err)
			}
			l, err := decodeHistoryLog(raw)
			if err != nil {
				t.Fatal(err)
			}
			// Use the previous full-struct scalar projection as the reference;
			// preserve typed dates, exact integers, and the validated raw metadata.
			want, err := onlineProjectionFields(l, map[string]string{"type": "event_type", "model_name": "model", "quota": "amount", "use_time": "duration_seconds", "token_id": "key_id", "group": "group_name"}, []string{"other", "Metadata", "CachedTokens"}, map[string]historyTime{"created_at": l.CreatedAt})
			if err != nil {
				t.Fatal(err)
			}
			want["metadata"] = l.Metadata
			if err := historyPromptAnomalyFields(l, true, want); err != nil {
				t.Fatal(err)
			}
			got, err := p.project(context.Background(), onlineSpec{name: "logs"}, raw, nil)
			if err != nil || len(got) == 0 || !reflect.DeepEqual(got[0].Values, want) {
				t.Fatalf("scalar projection changed: %v", err)
			}
			if tc.kind == 2 {
				if len(got) != 2 || got[1].Values["amount"] != l.Amount || got[1].Values["cached_tokens"] != l.CachedTokens || got[1].Values["legacy_prompt_anomaly"] != l.LegacyPromptAnomaly {
					t.Fatal("usage money/cache/anomaly changed")
				}
			} else if len(got) != 1 {
				t.Fatal("non-usage event gained usage")
			}
		})
	}
	if _, err := p.project(context.Background(), onlineSpec{name: "logs"}, json.RawMessage(`{"id":51,"type":2,"user_id":7,"other":"invalid-json"}`), nil); err == nil {
		t.Fatal("invalid metadata accepted")
	}
}

func BenchmarkOnlineLogOmittedMetadataProjection(b *testing.B) {
	for _, size := range []int{2048, 8192} {
		metadata, err := json.Marshal(map[string]any{"cached_tokens": 23, "precise": int64(9007199254740993), "payload": strings.Repeat("x", size)})
		if err != nil {
			b.Fatal(err)
		}
		l := historyLog{ID: 51, UserID: 7, Type: 2, Other: string(metadata), Metadata: metadata, CachedTokens: 23}
		for _, optimized := range []bool{false, true} {
			b.Run(fmt.Sprintf("bytes=%d/omit_before_encoding=%t", size, optimized), func(b *testing.B) {
				value := l
				if optimized {
					value.Other, value.Metadata = "", nil
				}
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if _, err := onlineProjectionFields(value, map[string]string{"type": "event_type", "model_name": "model", "quota": "amount", "use_time": "duration_seconds", "token_id": "key_id", "group": "group_name"}, []string{"other", "Metadata", "CachedTokens"}, nil); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
