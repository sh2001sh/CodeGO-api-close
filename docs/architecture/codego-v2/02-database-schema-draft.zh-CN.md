# CodeGo v2 数据库表结构草案

## 1. 文档目的

本文档用于给 CodeGo v2 提供第一版数据库结构草案。目标不是立即产出最终 SQL，而是给架构实施阶段提供：

- schema 划分建议
- 核心表结构建议
- 主键与幂等键设计建议
- 状态字段建议
- 索引建议
- 读写职责建议

本文档采用 PostgreSQL 作为目标数据库进行设计。

## 2. 数据建模原则

### 2.1 主交易事实优先

涉及身份、账本、任务、订单、请求执行的表必须面向交易事实设计，而不是优先面向后台列表页设计。

### 2.2 审计可重建

所有关键业务动作必须能够通过历史记录重建，不允许关键状态只保留最终值而无过程。

### 2.3 幂等是第一约束

所有可能因重试、回调、重复轮询导致重复执行的动作，都必须有明确幂等键。

### 2.4 读模型与交易表分离

复杂聚合查询不应长期直接依赖交易表，允许在同库独立 schema 中建立 read model 表。

## 3. Schema 划分

建议 schema：

- `identity`
- `gateway`
- `billing`
- `workflow`
- `commerce`
- `audit`
- `readmodel`
- `ops`

## 4. identity schema

### 4.1 identity.users

字段建议：

- `id` bigint pk
- `username` varchar(64) unique not null
- `email` varchar(255) unique null
- `phone` varchar(32) null
- `status` varchar(32) not null
- `role` varchar(32) not null
- `group_code` varchar(64) not null
- `display_name` varchar(128) null
- `avatar_url` text null
- `password_hash` text null
- `language` varchar(16) null
- `settings_json` jsonb not null default '{}'
- `created_at` timestamptz not null
- `updated_at` timestamptz not null
- `deleted_at` timestamptz null

索引建议：

- unique(`username`)
- unique(`email`) where email is not null
- index(`group_code`)
- index(`status`)

### 4.2 identity.sessions

- `session_id` uuid pk
- `user_id` bigint not null
- `session_type` varchar(32) not null
- `source` varchar(32) not null
- `ip` inet null
- `user_agent` text null
- `status` varchar(32) not null
- `expires_at` timestamptz not null
- `created_at` timestamptz not null
- `revoked_at` timestamptz null

索引建议：

- index(`user_id`, `status`)
- index(`expires_at`)

### 4.3 identity.oauth_bindings

- `id` bigint pk
- `user_id` bigint not null
- `provider` varchar(64) not null
- `provider_user_id` varchar(255) not null
- `provider_email` varchar(255) null
- `meta_json` jsonb not null default '{}'
- `created_at` timestamptz not null

唯一索引：

- unique(`provider`, `provider_user_id`)
- unique(`user_id`, `provider`)

### 4.4 identity.device_authorizations

- `id` bigint pk
- `user_id` bigint not null
- `device_type` varchar(32) not null
- `device_name` varchar(255) not null
- `scopes_json` jsonb not null
- `status` varchar(32) not null
- `auth_code_hash` text null
- `expires_at` timestamptz not null
- `approved_at` timestamptz null
- `created_at` timestamptz not null

## 5. gateway schema

### 5.1 gateway.provider_channels

- `id` bigint pk
- `provider_code` varchar(64) not null
- `channel_name` varchar(128) not null
- `base_url` text not null
- `proxy_url` text null
- `credential_ref` varchar(255) not null
- `status` varchar(32) not null
- `priority` int not null default 0
- `weight` int not null default 100
- `region_code` varchar(32) null
- `capabilities_json` jsonb not null default '{}'
- `settings_json` jsonb not null default '{}'
- `created_at` timestamptz not null
- `updated_at` timestamptz not null

索引建议：

- index(`provider_code`, `status`)
- index(`priority`)

### 5.2 gateway.model_catalog

- `id` bigint pk
- `canonical_model` varchar(128) not null
- `display_name` varchar(255) not null
- `family_code` varchar(64) not null
- `protocol_code` varchar(64) not null
- `status` varchar(32) not null
- `metadata_json` jsonb not null default '{}'
- `created_at` timestamptz not null
- `updated_at` timestamptz not null

唯一索引：

- unique(`canonical_model`)

### 5.3 gateway.model_aliases

- `id` bigint pk
- `canonical_model` varchar(128) not null
- `alias_model` varchar(128) not null
- `source` varchar(64) not null
- `created_at` timestamptz not null

唯一索引：

- unique(`alias_model`)

### 5.4 gateway.route_plans

- `route_plan_id` uuid pk
- `request_id` uuid not null
- `user_id` bigint not null
- `origin_model` varchar(128) not null
- `resolved_model` varchar(128) not null
- `plan_json` jsonb not null
- `decision_reason_json` jsonb not null
- `created_at` timestamptz not null

索引：

- index(`request_id`)
- index(`user_id`, `created_at`)

### 5.5 gateway.request_executions

- `request_id` uuid pk
- `trace_id` varchar(128) not null
- `user_id` bigint not null
- `token_id` bigint null
- `route_plan_id` uuid null
- `request_mode` varchar(32) not null
- `relay_format` varchar(32) not null
- `origin_model` varchar(128) not null
- `resolved_model` varchar(128) not null
- `provider_code` varchar(64) null
- `channel_id` bigint null
- `status` varchar(32) not null
- `status_reason` varchar(128) null
- `request_headers_json` jsonb not null default '{}'
- `request_meta_json` jsonb not null default '{}'
- `started_at` timestamptz null
- `completed_at` timestamptz null
- `created_at` timestamptz not null

索引：

- index(`user_id`, `created_at`)
- index(`status`, `created_at`)
- index(`provider_code`, `created_at`)
- index(`trace_id`)

### 5.6 gateway.execution_attempts

- `attempt_id` uuid pk
- `request_id` uuid not null
- `attempt_no` int not null
- `provider_code` varchar(64) not null
- `channel_id` bigint not null
- `status` varchar(32) not null
- `error_code` varchar(64) null
- `error_message` text null
- `latency_ms` int null
- `usage_hint_json` jsonb not null default '{}'
- `created_at` timestamptz not null
- `finished_at` timestamptz null

唯一索引：

- unique(`request_id`, `attempt_no`)

## 6. billing schema

### 6.1 billing.accounts

- `account_id` uuid pk
- `account_type` varchar(32) not null
- `owner_type` varchar(32) not null
- `owner_id` bigint not null
- `quota_unit` varchar(32) not null
- `status` varchar(32) not null
- `version` bigint not null default 0
- `meta_json` jsonb not null default '{}'
- `created_at` timestamptz not null
- `updated_at` timestamptz not null

索引：

- index(`owner_type`, `owner_id`)
- index(`account_type`, `status`)

### 6.2 billing.balance_snapshots

- `account_id` uuid pk
- `available_balance` bigint not null default 0
- `reserved_balance` bigint not null default 0
- `consumed_total` bigint not null default 0
- `refunded_total` bigint not null default 0
- `granted_total` bigint not null default 0
- `updated_at` timestamptz not null

### 6.3 billing.ledger_entries

- `entry_id` uuid pk
- `account_id` uuid not null
- `reference_type` varchar(32) not null
- `reference_id` varchar(128) not null
- `entry_type` varchar(32) not null
- `direction` varchar(16) not null
- `amount` bigint not null
- `balance_after` bigint null
- `idempotency_key` varchar(255) not null
- `reason_code` varchar(64) not null
- `reason_detail` text null
- `operator_type` varchar(32) not null
- `operator_id` varchar(128) null
- `created_at` timestamptz not null

唯一索引：

- unique(`idempotency_key`)

索引：

- index(`account_id`, `created_at`)
- index(`reference_type`, `reference_id`)
- index(`entry_type`, `created_at`)

### 6.4 billing.reservations

- `reservation_id` uuid pk
- `request_id` uuid null
- `workflow_id` uuid null
- `account_id` uuid not null
- `reserved_amount` bigint not null
- `status` varchar(32) not null
- `idempotency_key` varchar(255) not null
- `expires_at` timestamptz null
- `created_at` timestamptz not null
- `updated_at` timestamptz not null

唯一索引：

- unique(`idempotency_key`)

索引：

- index(`request_id`)
- index(`workflow_id`)
- index(`status`, `expires_at`)

### 6.5 billing.settlements

- `settlement_id` uuid pk
- `reservation_id` uuid not null
- `usage_evidence_id` uuid null
- `actual_amount` bigint not null
- `delta_amount` bigint not null
- `status` varchar(32) not null
- `idempotency_key` varchar(255) not null
- `settled_at` timestamptz not null
- `created_at` timestamptz not null

唯一索引：

- unique(`idempotency_key`)
- unique(`reservation_id`)

### 6.6 billing.adjustments

- `adjustment_id` uuid pk
- `account_id` uuid not null
- `request_id` uuid null
- `workflow_id` uuid null
- `amount` bigint not null
- `direction` varchar(16) not null
- `reason_code` varchar(64) not null
- `reason_detail` text null
- `approved_by` varchar(128) null
- `created_at` timestamptz not null

## 7. workflow schema

### 7.1 workflow.task_workflows

- `workflow_id` uuid pk
- `public_task_id` varchar(128) not null
- `upstream_task_id` varchar(255) null
- `request_id` uuid null
- `provider_code` varchar(64) not null
- `channel_id` bigint not null
- `task_kind` varchar(64) not null
- `state` varchar(32) not null
- `billing_reservation_id` uuid null
- `settlement_id` uuid null
- `timeout_at` timestamptz null
- `retry_policy_json` jsonb not null default '{}'
- `meta_json` jsonb not null default '{}'
- `created_at` timestamptz not null
- `updated_at` timestamptz not null

唯一索引：

- unique(`public_task_id`)

索引：

- index(`upstream_task_id`)
- index(`state`, `updated_at`)
- index(`provider_code`, `state`)

### 7.2 workflow.task_snapshots

- `snapshot_id` uuid pk
- `workflow_id` uuid not null
- `provider_state` varchar(64) not null
- `provider_progress` varchar(64) null
- `failure_reason` text null
- `result_url` text null
- `raw_payload` jsonb not null
- `recorded_at` timestamptz not null

索引：

- index(`workflow_id`, `recorded_at desc`)

### 7.3 workflow.task_terminal_results

- `workflow_id` uuid pk
- `terminal_state` varchar(32) not null
- `final_result_url` text null
- `final_meta_json` jsonb not null default '{}'
- `finished_at` timestamptz not null
- `settlement_status` varchar(32) not null

### 7.4 workflow.workflow_runs

用于记录 workflow engine 侧的运行信息。

- `run_id` uuid pk
- `workflow_name` varchar(128) not null
- `business_workflow_id` uuid not null
- `status` varchar(32) not null
- `retry_count` int not null default 0
- `last_error` text null
- `started_at` timestamptz not null
- `finished_at` timestamptz null

## 8. commerce schema

### 8.1 commerce.products

- `product_id` uuid pk
- `product_type` varchar(32) not null
- `product_code` varchar(128) not null
- `title` varchar(255) not null
- `status` varchar(32) not null
- `price_amount` numeric(18,6) not null
- `currency` varchar(16) not null
- `meta_json` jsonb not null default '{}'
- `created_at` timestamptz not null
- `updated_at` timestamptz not null

唯一索引：

- unique(`product_code`)

### 8.2 commerce.subscription_plans

- `plan_id` uuid pk
- `product_id` uuid not null
- `quota_reset_period` varchar(32) not null
- `period_quota` bigint not null
- `total_quota` bigint not null
- `duration_unit` varchar(16) not null
- `duration_value` int not null
- `supports_reset_opportunity` boolean not null default false
- `rules_json` jsonb not null default '{}'

### 8.3 commerce.orders

- `order_id` uuid pk
- `user_id` bigint not null
- `product_id` uuid not null
- `product_type` varchar(32) not null
- `payment_provider` varchar(32) not null
- `payment_status` varchar(32) not null
- `business_status` varchar(32) not null
- `amount` numeric(18,6) not null
- `currency` varchar(16) not null
- `idempotency_key` varchar(255) not null
- `external_trade_no` varchar(255) null
- `meta_json` jsonb not null default '{}'
- `created_at` timestamptz not null
- `paid_at` timestamptz null
- `updated_at` timestamptz not null

唯一索引：

- unique(`idempotency_key`)
- unique(`payment_provider`, `external_trade_no`) where external_trade_no is not null

### 8.4 commerce.payment_attempts

- `payment_attempt_id` uuid pk
- `order_id` uuid not null
- `provider` varchar(32) not null
- `status` varchar(32) not null
- `provider_payload` jsonb not null default '{}'
- `callback_idempotency_key` varchar(255) null
- `created_at` timestamptz not null
- `updated_at` timestamptz not null

### 8.5 commerce.benefit_grants

- `grant_id` uuid pk
- `order_id` uuid not null
- `benefit_type` varchar(32) not null
- `target_account_id` uuid not null
- `quota_amount` bigint not null
- `status` varchar(32) not null
- `effective_from` timestamptz null
- `effective_to` timestamptz null
- `created_at` timestamptz not null

## 9. audit schema

### 9.1 audit.audit_events

- `event_id` uuid pk
- `actor_type` varchar(32) not null
- `actor_id` varchar(128) not null
- `action` varchar(128) not null
- `target_type` varchar(64) not null
- `target_id` varchar(128) not null
- `correlation_id` varchar(128) not null
- `before_json` jsonb null
- `after_json` jsonb null
- `meta_json` jsonb not null default '{}'
- `created_at` timestamptz not null

索引：

- index(`correlation_id`)
- index(`actor_type`, `actor_id`, `created_at`)
- index(`target_type`, `target_id`)

### 9.2 audit.usage_logs

- `usage_log_id` uuid pk
- `request_id` uuid null
- `workflow_id` uuid null
- `user_id` bigint not null
- `account_id` uuid null
- `provider_code` varchar(64) null
- `model_name` varchar(128) null
- `quota_amount` bigint not null
- `usage_json` jsonb not null default '{}'
- `created_at` timestamptz not null

### 9.3 audit.outbox_events

- `event_id` uuid pk
- `aggregate_type` varchar(64) not null
- `aggregate_id` varchar(128) not null
- `event_type` varchar(128) not null
- `payload` jsonb not null
- `headers` jsonb not null default '{}'
- `status` varchar(32) not null
- `retry_count` int not null default 0
- `next_retry_at` timestamptz null
- `created_at` timestamptz not null
- `dispatched_at` timestamptz null

索引：

- index(`status`, `next_retry_at`)
- index(`aggregate_type`, `aggregate_id`)

## 10. readmodel schema

### 10.1 readmodel.user_usage_daily

- `day` date not null
- `user_id` bigint not null
- `request_count` bigint not null
- `quota_total` bigint not null
- `success_count` bigint not null
- `failed_count` bigint not null
- `updated_at` timestamptz not null

主键：

- (`day`, `user_id`)

### 10.2 readmodel.channel_usage_daily

- `day` date not null
- `channel_id` bigint not null
- `provider_code` varchar(64) not null
- `request_count` bigint not null
- `quota_total` bigint not null
- `error_count` bigint not null
- `p95_latency_ms` int null
- `updated_at` timestamptz not null

主键：

- (`day`, `channel_id`)

### 10.3 readmodel.billing_account_views

- `account_id` uuid pk
- `account_type` varchar(32) not null
- `owner_label` varchar(255) not null
- `available_balance` bigint not null
- `reserved_balance` bigint not null
- `last_activity_at` timestamptz null
- `updated_at` timestamptz not null

## 11. 状态字段建议

### 11.1 request execution status

- `received`
- `validated`
- `reserved`
- `executing`
- `completed`
- `failed`
- `canceled`

### 11.2 reservation status

- `open`
- `settled`
- `released`
- `expired`

### 11.3 task workflow state

- `created`
- `submitted`
- `queued`
- `running`
- `succeeded`
- `failed`
- `timeout`
- `compensated`

### 11.4 order status

支付侧：

- `pending`
- `paid`
- `failed`
- `expired`
- `refunded`

业务侧：

- `created`
- `benefit_granted`
- `closed`
- `reversed`

## 12. 幂等键设计建议

关键幂等点：

- request reservation：`req:{request_id}:reserve:{account_id}`
- request settlement：`req:{request_id}:settle`
- task refund：`task:{workflow_id}:refund`
- task settlement：`task:{workflow_id}:settle`
- payment callback：`pay:{provider}:{external_trade_no}:{event_type}`
- admin adjustment：`adj:{adjustment_request_id}`

## 13. 数据迁移建议

### 13.1 旧余额迁移

迁移方法：

1. 为每个 user/token/subscription 创建标准账户
2. 以初始化 grant entry 的方式写入当前余额
3. 建立快照表

### 13.2 旧任务迁移

迁移方法：

1. 旧 task 生成 workflow 记录
2. 已终态任务直接写 terminal result
3. 未终态任务生成待恢复 workflow

### 13.3 旧日志迁移

旧 usage log 可迁为 readmodel 或 audit 原始历史，但不强制全部重构为账本事件。

## 14. 数据库实施顺序

建议顺序：

1. 建 `billing` schema
2. 建 `workflow` schema
3. 建 `gateway.request_executions`
4. 建 `audit.outbox_events`
5. 建 `readmodel` schema
6. 补 `identity` 与 `commerce` 扩展表

## 15. 总结

这份草案的目标不是一次性冻结所有字段，而是给 CodeGo v2 的数据库治理建立统一方向。后续正式落 SQL 时，应优先保证：

- 账本事实结构不动摇
- 工作流状态结构不动摇
- 事件 outbox 结构不动摇

其它字段可按实施过程迭代优化。
