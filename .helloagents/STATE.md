# Main Goal
Keep CodeGo relay requests available during upstream recovery, without imposing a local text response-header timeout or turning recoverable failures into downstream 503s.

# Current Status
- Commit `386b0e463` is deployed on the production gateway. Text response-header timeout is disabled; TCP/TLS setup remains bounded and image header timeout remains 120 seconds.
- Production inspection confirms `RELAY_RESPONSE_HEADER_TIMEOUT` is absent. Gateway is not resource-bound, but Guard is currently using about 2 CPU cores and 2.6 GiB of its 3 GiB limit.
- Live 503 analysis found two cases: a failed Responses stream before semantic output can exhaust selection after its fault domain is excluded; and channel `73` currently returns a real `403 insufficient balance`. Its logs report it as the only selectable route for `gpt-5.6-sol` in that request group.
- Commit `56080137e` is pushed and its image build is running: legacy groups now detect enabled ability fallbacks when no route pool exists; pre-semantic Responses 502/504/524 can make one original-channel recovery attempt only after normal selection fails; a cancelled downstream write is marked client-gone before health processing.
- A follow-up patch is ready: text `STREAMING_FIRST_BYTE_TIMEOUT` and `STREAMING_LONG_CONTEXT_FIRST_BYTE_TIMEOUT` default to disabled, so only the upstream decides when a response that has connected but has no semantic output should end. Explicit nonzero values still opt into local enforcement; images and total stream duration are unchanged.
- Focused Go tests passed for gateway runtime, execution, transport, stream, store, and bootstrap.

# Next Actions
- Commit and push the timeout-default repair, wait for its gateway image, deploy it, then sample 15 minutes of 503s and first-byte traces.
- Keep channel 73's provider balance issue separate from transport reliability: fund/disable it or add a same-group model fallback; code cannot eliminate a 503 where no route can serve the requested model.

# Blockers
- None.
