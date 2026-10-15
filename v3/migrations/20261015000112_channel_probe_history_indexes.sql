CREATE INDEX IF NOT EXISTS channelmarket_verification_history_idx
    ON v3_channelmarket.verification_runs(channel_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS channelmarket_verification_observations_idx
    ON v3_channelmarket.verification_runs(channel_id, completed_at)
    WHERE status IN ('passed', 'failed');
