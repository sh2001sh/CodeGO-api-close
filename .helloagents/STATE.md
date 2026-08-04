# Main Goal
Reduce GPT first-token tail latency and prevent upstream capacity responses from reaching downstream users.

# Current Work
- Added model-level rapid cooldown for consecutive non-long-context 502/504/524 failures.
- Classified explicit upstream capacity messages as transient retryable failures.
- Automatic route pools skip cooling candidates and retain controlled half-open recovery probes.

# Key Context
- Two consecutive 502 failures cool a channel/model pair for 8 seconds; two consecutive 504/524 failures cool it for 15 seconds.
- One transient failure remains degraded, preserving normal traffic and avoiding unnecessary cooling.
- 429, model-unavailable, and long-context timeout behavior remains unchanged.
- Capacity messages such as `selected model is at capacity` retry before response delivery and use the short channel cooldown policy.
- Fault-domain behavior remains separate so channels retain redundancy where upstream fault domains differ.

# Verification
- `go test ./internal/gateway/runtime -count=1` passed.
- `go test ./internal/gateway/execution/app -count=1` passed.
- `go test ./internal/gateway/routing/app ./internal/gateway/transport/http -count=1` passed.
- Focused `go test ./internal/gateway/...` routing, health, and retry tests passed.

# Next Actions
- Commit the capacity classification, release as the next rc.34 version, then trigger and verify the multi-arch image build.

# Blockers
- None.
