-- One immutable migration boundary, including an explicit all-history choice.
CREATE TABLE v3_audit.history_retention (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    cutoff timestamptz
);
CREATE FUNCTION v3_audit.reject_history_cutoff_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'migration history cutoff is immutable';
END;
$$;
CREATE TRIGGER history_cutoff_immutable BEFORE UPDATE OR DELETE ON v3_audit.history_retention
    FOR EACH ROW EXECUTE FUNCTION v3_audit.reject_history_cutoff_change();

-- Compact consumption totals keep Key lifetime usage after ordinary logs expire.
CREATE TABLE v3_billing.retired_usage_totals (
    user_id bigint NOT NULL,
    key_id bigint NOT NULL,
    amount bigint NOT NULL CHECK (amount >= 0),
    PRIMARY KEY(user_id, key_id)
);

CREATE INDEX batch_items_retention_request_idx
    ON v3_channelmarket.batch_test_items(request_id)
    WHERE request_id <> '';

CREATE INDEX usage_logs_retention_idx ON v3_billing.usage_logs(created_at,id);
CREATE INDEX audit_events_retention_idx ON v3_audit.events(created_at,id) WHERE event_type NOT IN (1,3,6);
CREATE INDEX request_audits_retention_idx ON v3_audit.request_audits(created_at,request_id);
