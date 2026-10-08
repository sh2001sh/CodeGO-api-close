-- One inbox for transactional notices. Source changes and inbox events commit
-- together; LISTEN/NOTIFY invalidates open clients without periodic reads.
CREATE TABLE v3_identity.notifications (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES v3_identity.users(id),
    dedupe_key text NOT NULL UNIQUE,
    category text NOT NULL CHECK (category IN ('market','billing','review','rewards','system')),
    kind text NOT NULL,
    title_key text NOT NULL,
    body_key text NOT NULL,
    data jsonb NOT NULL DEFAULT '{}',
    action_url text NOT NULL CHECK (action_url ~ '^/[^/]' AND action_url !~ '[[:cntrl:]]'),
    created_at timestamptz NOT NULL DEFAULT now(),
    read_at timestamptz
);
CREATE INDEX notifications_user_list_idx ON v3_identity.notifications(user_id,created_at DESC,id DESC);
CREATE INDEX notifications_user_unread_idx ON v3_identity.notifications(user_id,id) WHERE read_at IS NULL;

CREATE FUNCTION v3_identity.publish_notification_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_notify('v3_notifications', NEW.user_id::text);
    RETURN NULL;
END;
$$;
CREATE TRIGGER notification_change AFTER INSERT OR UPDATE OF read_at ON v3_identity.notifications
FOR EACH ROW EXECUTE FUNCTION v3_identity.publish_notification_change();

CREATE FUNCTION v3_identity.capture_multiplier_notice() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        INSERT INTO v3_identity.notifications(user_id,dedupe_key,category,kind,title_key,body_key,data,action_url,created_at,read_at)
        VALUES(NEW.user_id,'multiplier:'||NEW.id,'market','multiplier_changed','notifications.multiplierChanged','notifications.multiplierChangedBody',
            jsonb_build_object('channel_id',NEW.channel_id::text,'previous_multiplier_ppm',NEW.previous_ppm::text,'multiplier_ppm',NEW.multiplier_ppm::text,'cleared',NEW.cleared),'/channel-market',NEW.created_at,NEW.read_at)
        ON CONFLICT(dedupe_key) DO NOTHING;
    ELSE
        UPDATE v3_identity.notifications SET read_at=NEW.read_at WHERE dedupe_key='multiplier:'||NEW.id AND read_at IS DISTINCT FROM NEW.read_at;
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER multiplier_inbox AFTER INSERT OR UPDATE OF read_at ON v3_channelmarket.multiplier_notices
FOR EACH ROW EXECUTE FUNCTION v3_identity.capture_multiplier_notice();

CREATE FUNCTION v3_identity.capture_lucky_notice() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        INSERT INTO v3_identity.notifications(user_id,dedupe_key,category,kind,title_key,body_key,data,action_url,created_at,read_at)
        SELECT NEW.user_id,'lucky:'||NEW.id,'rewards','lucky_reward','notifications.luckyReward','notifications.luckyRewardBody',
            jsonb_build_object('reward_id',r.id::text,'final_reward_credits',r.final_reward_credits::text),'/billing',NEW.created_at,NEW.read_at
        FROM v3_commerce.subscription_lucky_rewards r WHERE r.id=NEW.reward_id
        ON CONFLICT(dedupe_key) DO NOTHING;
    ELSE
        UPDATE v3_identity.notifications SET read_at=NEW.read_at WHERE dedupe_key='lucky:'||NEW.id AND read_at IS DISTINCT FROM NEW.read_at;
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER lucky_inbox AFTER INSERT OR UPDATE OF read_at ON v3_commerce.subscription_lucky_reward_notifications
FOR EACH ROW EXECUTE FUNCTION v3_identity.capture_lucky_notice();

-- Existing read/unread state is retained, with exact monetary values as strings.
INSERT INTO v3_identity.notifications(user_id,dedupe_key,category,kind,title_key,body_key,data,action_url,created_at,read_at)
SELECT user_id,'multiplier:'||id,'market','multiplier_changed','notifications.multiplierChanged','notifications.multiplierChangedBody',
    jsonb_build_object('channel_id',channel_id::text,'previous_multiplier_ppm',previous_ppm::text,'multiplier_ppm',multiplier_ppm::text,'cleared',cleared),'/channel-market',created_at,read_at
FROM v3_channelmarket.multiplier_notices ON CONFLICT(dedupe_key) DO NOTHING;
INSERT INTO v3_identity.notifications(user_id,dedupe_key,category,kind,title_key,body_key,data,action_url,created_at,read_at)
SELECT n.user_id,'lucky:'||n.id,'rewards','lucky_reward','notifications.luckyReward','notifications.luckyRewardBody',
    jsonb_build_object('reward_id',r.id::text,'final_reward_credits',r.final_reward_credits::text),'/billing',n.created_at,n.read_at
FROM v3_commerce.subscription_lucky_reward_notifications n JOIN v3_commerce.subscription_lucky_rewards r ON r.id=n.reward_id
ON CONFLICT(dedupe_key) DO NOTHING;

CREATE FUNCTION v3_identity.mirror_notification_read() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.read_at IS DISTINCT FROM OLD.read_at THEN
        IF NEW.dedupe_key LIKE 'multiplier:%' THEN
            UPDATE v3_channelmarket.multiplier_notices SET read_at=NEW.read_at WHERE id=split_part(NEW.dedupe_key,':',2)::bigint AND user_id=NEW.user_id AND read_at IS DISTINCT FROM NEW.read_at;
        ELSIF NEW.dedupe_key LIKE 'lucky:%' THEN
            UPDATE v3_commerce.subscription_lucky_reward_notifications SET read_at=NEW.read_at,updated_at=now() WHERE id=split_part(NEW.dedupe_key,':',2)::bigint AND user_id=NEW.user_id AND read_at IS DISTINCT FROM NEW.read_at;
        END IF;
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER inbox_source_read AFTER UPDATE OF read_at ON v3_identity.notifications
FOR EACH ROW EXECUTE FUNCTION v3_identity.mirror_notification_read();

CREATE FUNCTION v3_identity.capture_order_notice() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.state IS DISTINCT FROM OLD.state AND NEW.state IN ('paid','refunded','failed') THEN
        INSERT INTO v3_identity.notifications(user_id,dedupe_key,category,kind,title_key,body_key,data,action_url)
        VALUES(NEW.user_id,'order:'||NEW.id||':'||NEW.state,'billing','order_'||NEW.state,'notifications.order.'||NEW.state,'notifications.orderBody',
            jsonb_build_object('order_id',NEW.id::text,'amount_minor',NEW.amount_minor::text,'currency',NEW.currency,'state',NEW.state),'/billing')
        ON CONFLICT(dedupe_key) DO NOTHING;
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER order_inbox AFTER UPDATE OF state ON v3_commerce.orders
FOR EACH ROW EXECUTE FUNCTION v3_identity.capture_order_notice();

CREATE FUNCTION v3_identity.capture_shop_review_notice() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.review_status='pending' AND NEW.review_status IN ('approved','rejected') THEN
        INSERT INTO v3_identity.notifications(user_id,dedupe_key,category,kind,title_key,body_key,data,action_url)
        VALUES(NEW.owner_user_id,'shop-review:'||NEW.id||':'||txid_current(),'review','shop_review',
            'notifications.shop.'||NEW.review_status,'notifications.shopReviewBody',
            jsonb_build_object('shop_id',NEW.id::text,'status',NEW.review_status,'reason',NEW.review_reason),'/my-channels')
        ON CONFLICT(dedupe_key) DO NOTHING;
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER shop_review_inbox AFTER UPDATE OF review_status ON v3_channelmarket.shops
FOR EACH ROW EXECUTE FUNCTION v3_identity.capture_shop_review_notice();

CREATE FUNCTION v3_identity.capture_bargain_notice() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status IS DISTINCT FROM OLD.status AND NEW.status IN ('accepted','rejected') THEN
        INSERT INTO v3_identity.notifications(user_id,dedupe_key,category,kind,title_key,body_key,data,action_url)
        VALUES(NEW.user_id,'bargain:'||NEW.id||':'||NEW.status,'market','bargain_resolved',
            'notifications.bargain.'||NEW.status,'notifications.bargainBody',
            jsonb_build_object('group_id',NEW.group_id,'status',NEW.status,'note',NEW.resolution_note,'proposed_ppm',NEW.proposed_ppm::text),'/channel-market')
        ON CONFLICT(dedupe_key) DO NOTHING;
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER bargain_inbox AFTER UPDATE OF status ON v3_channelmarket.bargain_requests
FOR EACH ROW EXECUTE FUNCTION v3_identity.capture_bargain_notice();

CREATE FUNCTION v3_identity.capture_bargain_request_notice() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE owner_id bigint; public_id text;
BEGIN
    SELECT owner_user_id,public_channel_id INTO owner_id,public_id FROM v3_channelmarket.groups WHERE id=NEW.group_id;
    IF NEW.status='pending' AND owner_id IS NOT NULL AND owner_id<>NEW.user_id THEN
        INSERT INTO v3_identity.notifications(user_id,dedupe_key,category,kind,title_key,body_key,data,action_url)
        VALUES(owner_id,'bargain-request:'||NEW.id,'market','bargain_requested','notifications.bargainRequested','notifications.bargainRequestedBody',
            jsonb_build_object('request_id',NEW.id,'group_id',NEW.group_id,'channel_id',public_id,'proposed_ppm',NEW.proposed_ppm::text),'/my-channels')
        ON CONFLICT(dedupe_key) DO NOTHING;
    END IF;
    RETURN NULL;
END;
$$;
CREATE TRIGGER bargain_request_inbox AFTER INSERT ON v3_channelmarket.bargain_requests
FOR EACH ROW EXECUTE FUNCTION v3_identity.capture_bargain_request_notice();

CREATE FUNCTION v3_identity.capture_channel_review_notice() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE field_name text; review_state text; public_id text;
BEGIN
    IF NEW.scope<>'marketplace' OR NEW.owner_user_id IS NULL THEN RETURN NULL; END IF;
    FOREACH field_name IN ARRAY ARRAY['name','source_label'] LOOP
        review_state:=NEW.settings->'market'->>(field_name||'_status');
        IF OLD.settings->'market'->>(field_name||'_status')='pending' AND review_state IN ('approved','rejected') THEN
            SELECT public_channel_id INTO public_id FROM v3_channelmarket.groups WHERE channel_id=NEW.id;
            INSERT INTO v3_identity.notifications(user_id,dedupe_key,category,kind,title_key,body_key,data,action_url)
            VALUES(NEW.owner_user_id,'channel-review:'||NEW.id||':'||field_name||':'||txid_current(),'review','channel_review',
                'notifications.channel.'||review_state,'notifications.channelReviewBody',
                jsonb_build_object('channel_id',coalesce(public_id,NEW.id::text),'field',field_name,'status',review_state,
                    'reason',coalesce(NEW.settings->'market'->>(field_name||'_review_reason'),'')),'/my-channels')
            ON CONFLICT(dedupe_key) DO NOTHING;
        END IF;
    END LOOP;
    RETURN NULL;
END;
$$;
CREATE TRIGGER channel_review_inbox AFTER UPDATE OF settings ON v3_catalog.channels
FOR EACH ROW EXECUTE FUNCTION v3_identity.capture_channel_review_notice();
