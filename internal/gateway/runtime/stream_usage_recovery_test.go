package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStreamUsageRecoveryCancellationAndBounds(t *testing.T) {
	t.Run("before acceptance", func(t *testing.T) {
		parent, cancel := context.WithCancel(context.Background())
		r := NewStreamUsageRecovery(parent, time.Second, time.Second)
		defer r.Close()
		cancel()
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
			t.Fatal("unaccepted upstream was not cancelled")
		}
		require.False(t, r.IsDraining())
	})
	t.Run("accepted grace expires", func(t *testing.T) {
		parent, cancel := context.WithCancel(context.Background())
		defer cancel()
		r := NewStreamUsageRecovery(parent, time.Second, 30*time.Millisecond)
		defer r.Close()
		r.Accept()
		cancel()
		require.True(t, r.BeginDrain())
		require.NoError(t, r.Context().Err())
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
			t.Fatal("recovery grace did not stop upstream")
		}
		_, _, meta := r.Snapshot()
		require.Equal(t, "recovery_timeout", meta["end_reason"])
	})
	t.Run("absolute deadline", func(t *testing.T) {
		r := NewStreamUsageRecovery(context.Background(), 20*time.Millisecond, time.Second)
		defer r.Close()
		r.Accept()
		<-r.Context().Done()
		_, _, meta := r.Snapshot()
		require.Equal(t, "max_duration", meta["end_reason"])
	})
	t.Run("capacity and idempotent close", func(t *testing.T) {
		var active []*StreamUsageRecovery
		defer func() {
			for _, r := range active {
				r.Close()
			}
		}()
		for i := 0; i < cap(streamUsageRecoverySlots)+1; i++ {
			r := NewStreamUsageRecovery(context.Background(), time.Minute, time.Minute)
			r.Accept()
			active = append(active, r)
			require.Equal(t, i < cap(streamUsageRecoverySlots), r.BeginDrain())
		}
		_, _, meta := active[len(active)-1].Snapshot()
		require.True(t, meta["attempted"].(bool))
		require.Equal(t, "recovery_capacity_exceeded", meta["end_reason"])
		active[0].Close()
		active[0].Close()
		r := NewStreamUsageRecovery(context.Background(), time.Minute, time.Minute)
		defer r.Close()
		r.Accept()
		require.True(t, r.BeginDrain(), "Close must release the recovery slot exactly once")
	})
}

func TestStreamUsageRecoveryIgnoresPlaceholdersAndBoundsText(t *testing.T) {
	r := NewStreamUsageRecovery(context.Background(), time.Minute, time.Minute)
	defer r.Close()
	r.Observe(`{"type":"response.created","response":{"usage":{"input_tokens":123,"output_tokens":0}}}`)
	r.Observe(`{"type":"response.failed","response":{}}`)
	usage, _, _ := r.Snapshot()
	require.Nil(t, usage, "creation metadata is not final usage")
	r.Observe(`{"type":"response.reasoning_text.delta","delta":"` + strings.Repeat("a", 1<<20) + `"}`)
	r.Observe(`{"type":"response.output_text.delta","delta":"overflow"}`)
	_, text, meta := r.Snapshot()
	require.Len(t, text, 1<<20)
	require.True(t, meta["observed_text_truncated"].(bool))
	require.NotContains(t, meta, "text", "recovery logs must not contain model content")
}

func TestStreamUsageRecoveryChatFinishedUsageWithoutDone(t *testing.T) {
	r := NewStreamUsageRecovery(context.Background(), time.Minute, time.Minute)
	defer r.Close()
	r.Observe(`{"choices":[{"delta":{"reasoning_content":"partial"}}],"usage":{"total_tokens":9,"completion_tokens":1}}`)
	usage, _, _ := r.Snapshot()
	require.Nil(t, usage, "intermediate usage is not authoritative")
	r.Observe(`{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"input_tokens":123,"output_tokens":900,"total_tokens":1023,"input_tokens_details":{"cached_tokens":20},"output_tokens_details":{"reasoning_tokens":850}}}`)
	usage, _, meta := r.Snapshot()
	require.NotNil(t, usage)
	require.Equal(t, 123, usage.PromptTokens)
	require.Equal(t, 900, usage.CompletionTokens)
	require.Equal(t, 850, usage.CompletionTokenDetails.ReasoningTokens)
	require.Equal(t, 20, usage.PromptTokensDetails.CachedTokens)
	require.True(t, meta["final_usage_received"].(bool))
}
