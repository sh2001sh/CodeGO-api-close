# CodeGo v2 SQL 迁移草案

## 1. 文档目的

本文档提供 CodeGo v2 第一批数据库迁移的 SQL 草案示例。它不是最终可直接上线的完整 migration，但足以作为 DBA 与后端共同完善的初稿。

## 2. Schema 创建

```sql
create schema if not exists identity;
create schema if not exists gateway;
create schema if not exists billing;
create schema if not exists workflow;
create schema if not exists commerce;
create schema if not exists audit;
create schema if not exists readmodel;
create schema if not exists ops;
```

## 3. billing 核心表

### 3.1 billing.accounts

```sql
create table if not exists billing.accounts (
    account_id uuid primary key,
    account_type varchar(32) not null,
    owner_type varchar(32) not null,
    owner_id bigint not null,
    quota_unit varchar(32) not null,
    status varchar(32) not null,
    version bigint not null default 0,
    meta_json jsonb not null default '{}'::jsonb,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create index if not exists idx_billing_accounts_owner
    on billing.accounts(owner_type, owner_id);

create index if not exists idx_billing_accounts_type_status
    on billing.accounts(account_type, status);
```

### 3.2 billing.balance_snapshots

```sql
create table if not exists billing.balance_snapshots (
    account_id uuid primary key references billing.accounts(account_id),
    available_balance bigint not null default 0,
    reserved_balance bigint not null default 0,
    consumed_total bigint not null default 0,
    refunded_total bigint not null default 0,
    granted_total bigint not null default 0,
    updated_at timestamptz not null default now()
);
```

### 3.3 billing.ledger_entries

```sql
create table if not exists billing.ledger_entries (
    entry_id uuid primary key,
    account_id uuid not null references billing.accounts(account_id),
    reference_type varchar(32) not null,
    reference_id varchar(128) not null,
    entry_type varchar(32) not null,
    direction varchar(16) not null,
    amount bigint not null,
    balance_after bigint null,
    idempotency_key varchar(255) not null,
    reason_code varchar(64) not null,
    reason_detail text null,
    operator_type varchar(32) not null,
    operator_id varchar(128) null,
    created_at timestamptz not null default now()
);

create unique index if not exists uq_billing_ledger_entries_idempotency
    on billing.ledger_entries(idempotency_key);

create index if not exists idx_billing_ledger_entries_account_created
    on billing.ledger_entries(account_id, created_at desc);

create index if not exists idx_billing_ledger_entries_reference
    on billing.ledger_entries(reference_type, reference_id);
```

### 3.4 billing.reservations

```sql
create table if not exists billing.reservations (
    reservation_id uuid primary key,
    request_id uuid null,
    workflow_id uuid null,
    account_id uuid not null references billing.accounts(account_id),
    reserved_amount bigint not null,
    status varchar(32) not null,
    idempotency_key varchar(255) not null,
    expires_at timestamptz null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create unique index if not exists uq_billing_reservations_idempotency
    on billing.reservations(idempotency_key);

create index if not exists idx_billing_reservations_request
    on billing.reservations(request_id);

create index if not exists idx_billing_reservations_workflow
    on billing.reservations(workflow_id);
```

### 3.5 billing.settlements

```sql
create table if not exists billing.settlements (
    settlement_id uuid primary key,
    reservation_id uuid not null references billing.reservations(reservation_id),
    usage_evidence_id uuid null,
    actual_amount bigint not null,
    delta_amount bigint not null,
    status varchar(32) not null,
    idempotency_key varchar(255) not null,
    settled_at timestamptz not null default now(),
    created_at timestamptz not null default now()
);

create unique index if not exists uq_billing_settlements_idempotency
    on billing.settlements(idempotency_key);

create unique index if not exists uq_billing_settlements_reservation
    on billing.settlements(reservation_id);
```

## 4. gateway 核心表

### 4.1 gateway.request_executions

```sql
create table if not exists gateway.request_executions (
    request_id uuid primary key,
    trace_id varchar(128) not null,
    user_id bigint not null,
    token_id bigint null,
    route_plan_id uuid null,
    request_mode varchar(32) not null,
    relay_format varchar(32) not null,
    origin_model varchar(128) not null,
    resolved_model varchar(128) not null,
    provider_code varchar(64) null,
    channel_id bigint null,
    status varchar(32) not null,
    status_reason varchar(128) null,
    request_headers_json jsonb not null default '{}'::jsonb,
    request_meta_json jsonb not null default '{}'::jsonb,
    started_at timestamptz null,
    completed_at timestamptz null,
    created_at timestamptz not null default now()
);

create index if not exists idx_gateway_request_executions_user_created
    on gateway.request_executions(user_id, created_at desc);

create index if not exists idx_gateway_request_executions_status_created
    on gateway.request_executions(status, created_at desc);
```

## 5. workflow 核心表

### 5.1 workflow.task_workflows

```sql
create table if not exists workflow.task_workflows (
    workflow_id uuid primary key,
    public_task_id varchar(128) not null,
    upstream_task_id varchar(255) null,
    request_id uuid null,
    provider_code varchar(64) not null,
    channel_id bigint not null,
    task_kind varchar(64) not null,
    state varchar(32) not null,
    billing_reservation_id uuid null,
    settlement_id uuid null,
    timeout_at timestamptz null,
    retry_policy_json jsonb not null default '{}'::jsonb,
    meta_json jsonb not null default '{}'::jsonb,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create unique index if not exists uq_workflow_task_workflows_public_task_id
    on workflow.task_workflows(public_task_id);

create index if not exists idx_workflow_task_workflows_state_updated
    on workflow.task_workflows(state, updated_at desc);
```

### 5.2 workflow.task_snapshots

```sql
create table if not exists workflow.task_snapshots (
    snapshot_id uuid primary key,
    workflow_id uuid not null references workflow.task_workflows(workflow_id),
    provider_state varchar(64) not null,
    provider_progress varchar(64) null,
    failure_reason text null,
    result_url text null,
    raw_payload jsonb not null,
    recorded_at timestamptz not null default now()
);

create index if not exists idx_workflow_task_snapshots_workflow_recorded
    on workflow.task_snapshots(workflow_id, recorded_at desc);
```

## 6. audit outbox

```sql
create table if not exists audit.outbox_events (
    event_id uuid primary key,
    aggregate_type varchar(64) not null,
    aggregate_id varchar(128) not null,
    event_type varchar(128) not null,
    payload jsonb not null,
    headers jsonb not null default '{}'::jsonb,
    status varchar(32) not null,
    retry_count int not null default 0,
    next_retry_at timestamptz null,
    created_at timestamptz not null default now(),
    dispatched_at timestamptz null
);

create index if not exists idx_audit_outbox_status_retry
    on audit.outbox_events(status, next_retry_at);
```

## 7. 初始化账户迁移思路

### 7.1 用户钱包

逻辑：

1. 扫描旧用户表
2. 为每个用户创建 wallet account
3. 把旧 quota 写成 `grant_credit` 初始化流水
4. 生成 balance snapshot

伪 SQL 思路：

```sql
-- 实际执行建议用脚本而不是纯 SQL
```

原因：

- 需要生成 UUID
- 需要写两张表
- 需要幂等保护

### 7.2 Token 额度

为每个 token 创建 token account，并将 `remain_quota` 与 `used_quota` 映射成初始化快照。

### 7.3 订阅额度

为每个活跃订阅创建 subscription account，并将 `amount_total` 与 `amount_used` 投影到 snapshot。

## 8. 兼容字段保留策略

迁移早期建议保留旧字段：

- user.quota
- token.remain_quota
- subscription.amount_used
- task.quota

这些字段在双写阶段继续维护，直到新账本对账稳定。

## 9. 回填脚本建议

建议使用 Go 编写一次性 migration tool，而不是复杂 SQL：

- `cmd/migrate-v2-accounts`
- `cmd/migrate-v2-tasks`
- `cmd/rebuild-v2-snapshots`

## 10. 总结

本 SQL 草案的作用是让 CodeGo v2 的数据库升级从“想法”进入“可执行讨论”阶段。真正上线前，应再补：

- 外键策略取舍
- 大表分区策略
- 索引压测
- 在线迁移回滚方案
