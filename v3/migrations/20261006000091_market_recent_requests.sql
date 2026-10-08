-- The public six-hour request strip reads only selected marketplace channels.
-- Exclude internal probes from the index and retain request_id for deduplication.
CREATE INDEX request_audits_market_recent_idx
    ON v3_audit.request_audits (final_channel_id, started_at DESC, request_id)
    INCLUDE (status)
    WHERE counted_in_success_rate;
