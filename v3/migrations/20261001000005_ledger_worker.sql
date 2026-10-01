-- Billing events the ledger worker could not post (malformed, unknown
-- account). They are acknowledged on the stream so one bad event cannot block
-- the rest, and kept here for manual repair.
CREATE TABLE v3_billing.dead_letters (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now(),
    stream_id  text        NOT NULL,
    reason     text        NOT NULL,
    payload    jsonb       NOT NULL
);

CREATE INDEX dead_letters_created_idx ON v3_billing.dead_letters (created_at);
