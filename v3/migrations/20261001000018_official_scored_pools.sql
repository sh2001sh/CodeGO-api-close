-- Named source pools retain disabled/deleted siblings; native unnamed pools
-- still have one row per (group, model), including channel-market projections.
ALTER TABLE v3_catalog.route_pools
    ADD COLUMN name text NOT NULL DEFAULT '',
    ADD COLUMN model_scope text NOT NULL DEFAULT '',
    ADD COLUMN auto_discover boolean NOT NULL DEFAULT false,
    ADD COLUMN multiplier_weight integer NOT NULL DEFAULT 35 CHECK (multiplier_weight BETWEEN 0 AND 100),
    ADD COLUMN ttft_weight integer NOT NULL DEFAULT 25 CHECK (ttft_weight BETWEEN 0 AND 100),
    ADD COLUMN cache_weight integer NOT NULL DEFAULT 15 CHECK (cache_weight BETWEEN 0 AND 100),
    ADD COLUMN success_weight integer NOT NULL DEFAULT 25 CHECK (success_weight BETWEEN 0 AND 100),
    ADD COLUMN deleted_at timestamptz;
ALTER TABLE v3_catalog.route_pools DROP CONSTRAINT route_pools_strategy_check;
ALTER TABLE v3_catalog.route_pools ADD CONSTRAINT route_pools_strategy_check
    CHECK (strategy IN ('round_robin','fill_first','weighted','scored'));
ALTER TABLE v3_catalog.route_pools DROP CONSTRAINT route_pools_group_name_model_key;
CREATE UNIQUE INDEX route_pools_native_scope ON v3_catalog.route_pools(group_name,model) WHERE name='';
CREATE UNIQUE INDEX route_pools_live_scored_group ON v3_catalog.route_pools(group_name)
    WHERE strategy='scored' AND enabled AND deleted_at IS NULL;
ALTER TABLE v3_catalog.route_pool_members
    ADD COLUMN legacy_id bigint UNIQUE,
    ADD COLUMN cost_multiplier numeric NOT NULL DEFAULT 1 CHECK (cost_multiplier > 0 AND cost_multiplier NOT IN ('NaN'::numeric,'Infinity'::numeric)),
    ADD COLUMN model_cost_overrides jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(model_cost_overrides)='object'),
    ADD COLUMN fault_domain text NOT NULL DEFAULT '',
    ADD COLUMN enabled boolean NOT NULL DEFAULT true,
    ADD COLUMN deleted_at timestamptz;
