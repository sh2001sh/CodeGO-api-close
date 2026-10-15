-- CodeGo v2 initial migration draft
-- Target: PostgreSQL
-- Phase: bootstrap schemas + core billing/workflow/gateway tables

BEGIN;

CREATE SCHEMA IF NOT EXISTS billing;
CREATE SCHEMA IF NOT EXISTS gateway;
CREATE SCHEMA IF NOT EXISTS workflow;
CREATE SCHEMA IF NOT EXISTS readmodel;
CREATE SCHEMA IF NOT EXISTS audit;
CREATE SCHEMA IF NOT EXISTS platform;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS billing.accounts (
    account_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_type VARCHAR(32) NOT NULL,
    owner_type VARCHAR(32) NOT NULL,
    owner_id VARCHAR(128) NOT NULL,
    currency VARCHAR(16) NOT NULL DEFAULT 'USD',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (owner_type, owner_id, currency)
);

CREATE TABLE IF NOT EXISTS billing.ledger_entries (
    entry_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID NOT NULL REFERENCES billing.accounts(account_id),
    entry_type VARCHAR(32) NOT NULL,
    direction VARCHAR(16) NOT NULL,
    amount NUMERIC(20, 8) NOT NULL,
    balance_delta NUMERIC(20, 8) NOT NULL,
    currency VARCHAR(16) NOT NULL,
    reference_type VARCHAR(64) NOT NULL,
    reference_id VARCHAR(128) NOT NULL,
    reason_code VARCHAR(64) NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (account_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_account_created_at
    ON billing.ledger_entries(account_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_reference
    ON billing.ledger_entries(reference_type, reference_id);

CREATE TABLE IF NOT EXISTS billing.reservations (
    reservation_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID NOT NULL REFERENCES billing.accounts(account_id),
    request_id VARCHAR(128) NOT NULL,
    workflow_id UUID,
    status VARCHAR(32) NOT NULL,
    reserved_amount NUMERIC(20, 8) NOT NULL,
    settled_amount NUMERIC(20, 8) NOT NULL DEFAULT 0,
    refunded_amount NUMERIC(20, 8) NOT NULL DEFAULT 0,
    currency VARCHAR(16) NOT NULL,
    reason_code VARCHAR(64) NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    expires_at TIMESTAMPTZ,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (account_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_reservations_request_id
    ON billing.reservations(request_id);

CREATE TABLE IF NOT EXISTS billing.settlements (
    settlement_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reservation_id UUID NOT NULL REFERENCES billing.reservations(reservation_id),
    account_id UUID NOT NULL REFERENCES billing.accounts(account_id),
    usage_evidence_id UUID,
    actual_amount NUMERIC(20, 8) NOT NULL,
    delta_amount NUMERIC(20, 8) NOT NULL,
    status VARCHAR(32) NOT NULL,
    reason_code VARCHAR(64) NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (reservation_id, idempotency_key)
);

CREATE TABLE IF NOT EXISTS billing.refunds (
    refund_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID NOT NULL REFERENCES billing.accounts(account_id),
    reservation_id UUID REFERENCES billing.reservations(reservation_id),
    settlement_id UUID REFERENCES billing.settlements(settlement_id),
    reference_type VARCHAR(64) NOT NULL,
    reference_id VARCHAR(128) NOT NULL,
    amount NUMERIC(20, 8) NOT NULL,
    status VARCHAR(32) NOT NULL,
    reason_code VARCHAR(64) NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (account_id, idempotency_key)
);

CREATE TABLE IF NOT EXISTS gateway.route_plans (
    route_plan_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id VARCHAR(128) NOT NULL UNIQUE,
    account_id UUID REFERENCES billing.accounts(account_id),
    requested_model VARCHAR(128) NOT NULL,
    resolved_provider VARCHAR(64),
    resolved_model VARCHAR(128),
    stream BOOLEAN NOT NULL DEFAULT FALSE,
    policy_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS gateway.request_executions (
    request_execution_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id VARCHAR(128) NOT NULL UNIQUE,
    route_plan_id UUID REFERENCES gateway.route_plans(route_plan_id),
    account_id UUID REFERENCES billing.accounts(account_id),
    request_kind VARCHAR(32) NOT NULL,
    stream BOOLEAN NOT NULL DEFAULT FALSE,
    terminal_state VARCHAR(32),
    provider_code VARCHAR(64),
    model_name VARCHAR(128),
    usage_evidence_id UUID,
    reservation_id UUID REFERENCES billing.reservations(reservation_id),
    settlement_id UUID REFERENCES billing.settlements(settlement_id),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_request_executions_account_started_at
    ON gateway.request_executions(account_id, started_at DESC);

CREATE TABLE IF NOT EXISTS gateway.execution_attempts (
    attempt_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_execution_id UUID NOT NULL REFERENCES gateway.request_executions(request_execution_id),
    provider_code VARCHAR(64) NOT NULL,
    channel_id BIGINT,
    model_name VARCHAR(128) NOT NULL,
    attempt_no INTEGER NOT NULL,
    status VARCHAR(32) NOT NULL,
    failure_code VARCHAR(64),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ,
    UNIQUE (request_execution_id, attempt_no)
);

CREATE TABLE IF NOT EXISTS workflow.task_workflows (
    workflow_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    public_task_id UUID NOT NULL UNIQUE,
    request_id VARCHAR(128),
    account_id UUID REFERENCES billing.accounts(account_id),
    provider_code VARCHAR(64) NOT NULL,
    channel_id BIGINT,
    reservation_id UUID REFERENCES billing.reservations(reservation_id),
    task_kind VARCHAR(64) NOT NULL,
    temporal_workflow_id VARCHAR(255),
    temporal_run_id VARCHAR(255),
    status VARCHAR(32) NOT NULL,
    terminal_state VARCHAR(32),
    timeout_at TIMESTAMPTZ,
    result_url TEXT,
    result_meta JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS workflow.task_snapshots (
    snapshot_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID NOT NULL REFERENCES workflow.task_workflows(workflow_id),
    provider_state VARCHAR(64) NOT NULL,
    provider_progress INTEGER,
    raw_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    result_url TEXT,
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_task_snapshots_workflow_created_at
    ON workflow.task_snapshots(workflow_id, created_at DESC);

CREATE TABLE IF NOT EXISTS workflow.task_terminal_results (
    terminal_result_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID NOT NULL UNIQUE REFERENCES workflow.task_workflows(workflow_id),
    terminal_state VARCHAR(32) NOT NULL,
    settlement_status VARCHAR(32) NOT NULL,
    result_url TEXT,
    result_meta JSONB NOT NULL DEFAULT '{}'::jsonb,
    finalized_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS audit.audit_logs (
    audit_log_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_type VARCHAR(32) NOT NULL,
    actor_id VARCHAR(128) NOT NULL,
    action VARCHAR(128) NOT NULL,
    resource_type VARCHAR(64) NOT NULL,
    resource_id VARCHAR(128) NOT NULL,
    request_id VARCHAR(128),
    before_snapshot JSONB,
    after_snapshot JSONB,
    reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_actor_created_at
    ON audit.audit_logs(actor_type, actor_id, created_at DESC);

CREATE TABLE IF NOT EXISTS platform.outbox_events (
    outbox_event_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type VARCHAR(128) NOT NULL,
    aggregate_type VARCHAR(64) NOT NULL,
    aggregate_id VARCHAR(128) NOT NULL,
    request_id VARCHAR(128),
    trace_id VARCHAR(128),
    payload JSONB NOT NULL,
    publish_status VARCHAR(32) NOT NULL DEFAULT 'pending',
    publish_attempts INTEGER NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_outbox_events_publish_status_created_at
    ON platform.outbox_events(publish_status, created_at);

COMMIT;
