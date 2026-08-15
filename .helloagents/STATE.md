# Main Goal
Complete marketplace channel publishing, API key selection, status visibility, and shared editing.

# Current Status
- Marketplace source is selected from: Codex Plus, Codex Pro, CC-Max, CC-Kiro, CC其它, 国产模型.
- Fixed source selections are approved immediately and do not require administrator review.
- Internal group names use source plus a six-character group suffix, for example `Codex-Plus-ae381d`; user ID, multiplier, and routing version are excluded.
- Existing legacy groups are reconciled on startup and verified legacy channels are upgraded to the new naming and publication flow.
- Detector v2 validates the upstream model list, confirms every declared model exists, and performs one minimal real inference request using the provider protocol.
- Detection success automatically creates or synchronizes the internal gateway channel and publishes it as active; failure records the exact reason and keeps it unavailable.
- API key creation can directly select active/passed public marketplace groups and the owner's own private groups.
- Marketplace owners may use their own groups; settlement remains 5% platform commission and 95% owner income.
- Marketplace declared models are merged into sidebar group status, independent of official pricing records.
- Channel creation and editing use the same form. Owners and administrators can edit protocol, source, models, credentials, multiplier, visibility, capacity, and maintenance window.
- Administrator governance no longer requires a review reason or manual approval.
- SQLite marketplace ranking's reserved `group` column is quoted correctly.
- Local service is running at `http://127.0.0.1:3000` with SQLite and no Redis.

# Verification
- `bun run typecheck` passed.
- Targeted Marketplace/Keys/sidebar ESLint passed.
- `bun run build` passed.
- Marketplace, gateway group-status, and API-key controller tests passed.
- Detector protocol, declared-model validation, source naming, direct API-key selection, group-status model merge, and SQLite ranking tests passed.
- `git diff --check` passed except an existing `.env.example` LF/CRLF warning.
- Local database confirms `Codex-Plus-ae381d`, `active`, `passed`, `public`, source `approved`, detector `2.0.0`.
- Detector summary: `模型列表与实际推理检测通过，渠道已自动上架`.
- `GET /marketplace` returned HTTP 200.

# Next Actions
- Refresh the signed-in browser and verify the third-party group appears in API key creation and sidebar group status.

# Blockers
- No screenshot automation was available in this session.
