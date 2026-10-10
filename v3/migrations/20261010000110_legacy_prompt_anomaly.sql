-- A witnessed V2 cache subtraction can leave a signed prompt statistic.
-- Keep the original evidence and charge; new traffic still defaults to strict
-- nonnegative tokens. The migration validates the cache evidence before opting in.
ALTER TABLE v3_audit.events
    ADD COLUMN legacy_prompt_anomaly boolean NOT NULL DEFAULT false,
    DROP CONSTRAINT events_prompt_tokens_check,
    ADD CONSTRAINT events_prompt_tokens_check CHECK (
        (prompt_tokens >= 0 AND NOT legacy_prompt_anomaly)
        OR (prompt_tokens < 0 AND legacy_prompt_anomaly AND event_type = 2)
    );
ALTER TABLE v3_billing.usage_logs
    ADD COLUMN legacy_prompt_anomaly boolean NOT NULL DEFAULT false,
    DROP CONSTRAINT usage_logs_prompt_tokens_check,
    ADD CONSTRAINT usage_logs_prompt_tokens_check CHECK (
        (prompt_tokens >= 0 AND NOT legacy_prompt_anomaly)
        OR (prompt_tokens < 0 AND legacy_prompt_anomaly)
    );
COMMENT ON COLUMN v3_billing.usage_logs.legacy_prompt_anomaly IS
    'V2 cache subtraction anomaly: raw signed prompt statistic retained; charge is unchanged. Not a measured negative token count.';
