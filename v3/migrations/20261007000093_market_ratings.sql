-- Keep migrated review rows in their original authority; only the editor moves.
-- Durable change events commit atomically with the review. NOTIFY is a wake-up,
-- never the delivery record; workers replay pending rows after restart.
CREATE TABLE v3_community.rating_event_outbox (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    channel_id text NOT NULL,
    owner_sub text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    available_at timestamptz NOT NULL DEFAULT now(),
    attempts integer NOT NULL DEFAULT 0,
    delivered_at timestamptz
);
CREATE INDEX rating_event_pending_idx ON v3_community.rating_event_outbox(available_at,id) WHERE delivered_at IS NULL;

CREATE FUNCTION v3_community.enqueue_rating_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE event_channel text; event_owner text; event_id bigint;
BEGIN
    IF TG_OP='UPDATE' AND NEW.stars=OLD.stars THEN RETURN NEW; END IF;
    IF TG_OP='DELETE' THEN event_channel:=OLD.channel_id; ELSE event_channel:=NEW.channel_id; END IF;
    SELECT owner.external_id INTO event_owner FROM v3_catalog.channels c
    JOIN v3_identity.users owner ON owner.id=c.owner_user_id
    WHERE c.scope='marketplace' AND c.status='enabled' AND owner.status='active' AND owner.deleted_at IS NULL
    AND c.settings->'community'->>'visibility'='public'
    AND c.settings->'community'->>'verification_status'='passed'
    AND c.settings->'community'->>'lifecycle_status' IN ('active','degraded')
    AND c.settings->'community'->>'id'=event_channel AND COALESCE(owner.external_id,'')<>'';
    IF event_owner IS NOT NULL THEN
        INSERT INTO v3_community.rating_event_outbox(channel_id,owner_sub) VALUES(event_channel,event_owner) RETURNING id INTO event_id;
        PERFORM pg_notify('v3_market_rating_changed',event_id::text);
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
CREATE TRIGGER channel_ratings_event AFTER INSERT OR UPDATE OR DELETE ON v3_community.channel_ratings
FOR EACH ROW EXECUTE FUNCTION v3_community.enqueue_rating_event();
