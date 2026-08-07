# Main Goal
Prevent GPT stream-scanner worker leaks and cancellation delays from exhausting routing candidates after upstream disconnects, especially on channels 67 and 32.

# Current Work
- Commit `a913c56e8` is deployed to production and introduced safe pre-semantic retries plus sanitized terminal 503 behavior.
- Local follow-up refactors `ScanResponse` so scanner, data delivery, and SSE ping lifecycles have explicit cancellation and completion signals.
- Any handler, ping, or worker failure now wakes the main flow and closes the upstream response body, interrupting a blocked `Scanner.Scan` immediately instead of waiting for the idle timeout.
- The data worker owns all downstream writes. The ping worker only queues a bounded ping signal, eliminating the former detached ping-write goroutine and write-mutex retention risk.
- Normal `[DONE]`/EOF still drains already-read frames; abnormal ends cancel stream pacing and data workers.
- Stream pacing and flushing now consume the request-scoped worker cancellation context.
- Added regression tests for normal frame draining, idle-timeout cancellation, and handler-stop interruption of a blocked upstream scanner.

# Verification
- `go test ./internal/gateway/... -count=1` passed.
- `go test ./internal/gateway/stream -count=5` passed.
- `git diff --check` passed.
- Full-repository test still has the pre-existing `internal/commerce/transport/http: TestGetSubscriptionOrderStatusReturnsOrderPayload` expectation failure; unrelated to this gateway change.

# Next Actions
- Commit only the stream lifecycle files and push to trigger the gateway image build.
- Deploy only `new-api-v2-gateway`, retaining the existing rollback container.
- Observe production for at least 15 minutes: no `timeout waiting for goroutines to exit`, channels 67/32 502/504, candidate exhaustion/503, retry recovery chains, and gateway restarts.

# Blockers
- No active implementation blocker.
