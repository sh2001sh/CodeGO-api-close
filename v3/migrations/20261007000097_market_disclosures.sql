-- Owner declarations are separate from verification and contain no credentials.
CREATE TABLE v3_channelmarket.disclosures (
    channel_id bigint PRIMARY KEY REFERENCES v3_catalog.channels(id),
    document jsonb NOT NULL CHECK (jsonb_typeof(document) = 'object'),
    updated_at timestamptz NOT NULL
);

CREATE INDEX request_audits_market_model_idx
    ON v3_audit.request_audits (final_channel_id, model, started_at DESC)
    WHERE counted_in_success_rate;
