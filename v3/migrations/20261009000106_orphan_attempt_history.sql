-- Preserve original attempts whose historical request audit is absent, without
-- manufacturing a parent or weakening the live attempt audit foreign key.
CREATE TABLE v3_audit.orphan_request_attempt_history (
    attempt_id text PRIMARY KEY CHECK (attempt_id <> ''),
    request_id text NOT NULL CHECK (request_id <> ''),
    source_record jsonb NOT NULL CHECK ((
        jsonb_typeof(source_record) = 'object'
        AND source_record->>'attempt_id' = attempt_id
        AND source_record->>'request_id' = request_id
    ) IS TRUE)
);
