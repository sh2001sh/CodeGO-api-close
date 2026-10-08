-- Nullable observations preserve unknown historical/estimated performance.
-- Existing completion_tokens remains accounting data, including estimates.
-- generation_ms is populated only for a successful streaming response with
-- upstream-reported completion tokens; TTFT measures the final attempt only.
ALTER TABLE v3_audit.request_audits
    ADD COLUMN ttft_ms double precision,
    ADD COLUMN generation_ms double precision,
    ADD CONSTRAINT request_audits_ttft_ms_check
        CHECK (ttft_ms IS NULL OR (ttft_ms > 0 AND ttft_ms < 'Infinity'::double precision AND request_type = 'stream')),
    ADD CONSTRAINT request_audits_generation_ms_check
        CHECK (generation_ms IS NULL OR (
            generation_ms > 0 AND generation_ms < 'Infinity'::double precision
            AND ttft_ms IS NOT NULL AND completion_tokens > 0 AND status = 'success'));

COMMENT ON COLUMN v3_audit.request_audits.ttft_ms IS
    'Measured final streaming attempt: upstream send to first genuine data output, milliseconds; NULL means unknown.';
COMMENT ON COLUMN v3_audit.request_audits.generation_ms IS
    'Measured first genuine output to upstream stream termination before billing, milliseconds; only successful non-estimated token usage; NULL means unavailable.';
