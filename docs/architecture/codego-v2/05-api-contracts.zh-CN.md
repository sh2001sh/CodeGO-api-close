# CodeGo v2 API 契约草案

## 1. 文档目的

本文档用于定义 CodeGo v2 的接口边界。目标不是给出最终 OpenAPI 文件，而是明确：

- 哪些接口属于控制面
- 哪些接口属于执行面
- 哪些接口属于内部工作流或账本接口
- 请求响应最小字段要求
- 幂等、鉴权、审计和错误模型约束

## 2. API 分层原则

### 2.1 控制面 API

面向：

- Web 控制台
- 管理后台
- 桌面控制接口

特点：

- 强鉴权
- 强审计
- 业务配置与查询为主

### 2.2 网关面 API

面向：

- 客户端 SDK
- OpenAI 兼容调用方
- 流式请求与异步任务提交方

特点：

- 低延迟
- 协议兼容
- 高并发

### 2.3 内部工作流 API

面向：

- workflow worker
- ledger worker
- provider execution internal service

特点：

- 不对外暴露
- 幂等与可回放优先

## 3. 通用约束

### 3.1 请求头

所有 v2 内部标准接口建议支持：

- `X-Request-Id`
- `X-Trace-Id`
- `X-Idempotency-Key`
- `X-Actor-Type`
- `X-Actor-Id`

### 3.2 错误模型

统一错误结构：

```json
{
  "error": {
    "code": "string",
    "message": "string",
    "type": "business|validation|security|provider|infra",
    "retryable": false,
    "correlation_id": "string"
  }
}
```

### 3.3 幂等模型

以下接口必须要求幂等键：

- 订单创建
- 权益发放
- reservation
- settlement
- refund
- admin adjustment
- 高风险 callback 处理

## 4. 控制面 API 契约

### 4.1 Identity API

#### POST `/api/control/identity/register`

请求：

- `username`
- `email`
- `password`
- `verification_code`
- `invite_code`

响应：

- `user_id`
- `status`
- `requires_login`

#### POST `/api/control/identity/login`

请求：

- `login`
- `password`
- `otp_code`

响应：

- `session_token`
- `user`
- `security_flags`

### 4.2 Commerce API

#### GET `/api/control/commerce/products`

响应：

- product list
- pricing
- product status

#### POST `/api/control/commerce/orders`

请求：

- `product_id`
- `payment_provider`
- `client_context`

头：

- `X-Idempotency-Key`

响应：

- `order_id`
- `payment_attempt`
- `next_action`

#### POST `/api/control/commerce/orders/{order_id}/mark-paid`

内部或 webhook 编排接口。

请求：

- `provider_event_id`
- `provider_payload`
- `paid_amount`
- `paid_at`

响应：

- `order_id`
- `payment_status`
- `business_status`

### 4.3 Subscription Admin API

#### POST `/api/control/subscriptions/{subscription_id}/reset-quota`

请求：

- `advance_reset_time`
- `reason`

响应：

- `subscription_id`
- `reset_entry_id`
- `new_period_usage`

必须审计：

- actor
- before
- after
- reason

## 5. 网关面 API 契约

### 5.1 Chat Completion API

#### POST `/v2/chat/completions`

行为：

- 接收标准兼容请求
- 归一化
- 生成 route plan
- 生成 request execution
- 创建 reservation
- 执行 provider 调用
- 生成 usage evidence
- 结算

内部必需字段：

- `request_id`
- `trace_id`
- `route_plan_id`

响应头建议：

- `X-CodeGo-Request-Id`
- `X-CodeGo-Provider`
- `X-CodeGo-Model`

### 5.2 Stream API

流式接口与同步接口契约保持一致，但增加：

- stream lifecycle semantics
- `event: usage`
- `event: done`

统一事件建议：

- `message`
- `usage`
- `done`
- `error`

### 5.3 Async Task Submit API

#### POST `/v2/tasks/video`

请求：

- 标准化任务参数
- `model`
- `duration`
- `size`
- `metadata`

响应：

- `public_task_id`
- `workflow_id`
- `status`
- `reservation_id`

### 5.4 Async Task Query API

#### GET `/v2/tasks/{public_task_id}`

响应：

- `public_task_id`
- `status`
- `progress`
- `result_url`
- `terminal_reason`
- `updated_at`

## 6. 内部账本 API 契约

### 6.1 Create Reservation

#### POST `/internal/billing/reservations`

请求：

- `request_id`
- `workflow_id`
- `account_id`
- `reserved_amount`
- `reason_code`
- `idempotency_key`

响应：

- `reservation_id`
- `status`
- `reserved_amount`

### 6.2 Settle Reservation

#### POST `/internal/billing/settlements`

请求：

- `reservation_id`
- `actual_amount`
- `usage_evidence_id`
- `reason_code`
- `idempotency_key`

响应：

- `settlement_id`
- `delta_amount`
- `status`

### 6.3 Refund Reference

#### POST `/internal/billing/refunds`

请求：

- `reference_type`
- `reference_id`
- `reason_code`
- `idempotency_key`

响应：

- `refund_entry_ids`
- `status`

## 7. 内部工作流 API 契约

### 7.1 Create Task Workflow

#### POST `/internal/workflows/tasks`

请求：

- `public_task_id`
- `provider`
- `channel_id`
- `reservation_id`
- `task_kind`
- `submit_payload`

响应：

- `workflow_id`
- `state`

### 7.2 Record Task Snapshot

#### POST `/internal/workflows/tasks/{workflow_id}/snapshots`

请求：

- `provider_state`
- `provider_progress`
- `raw_payload`
- `result_url`
- `failure_reason`

响应：

- `snapshot_id`

### 7.3 Finalize Task

#### POST `/internal/workflows/tasks/{workflow_id}/finalize`

请求：

- `terminal_state`
- `result_url`
- `result_meta`
- `settlement_hint`

响应：

- `workflow_id`
- `terminal_state`
- `settlement_status`

## 8. 事件契约建议

### 8.1 `billing.reservation.created`

payload:

- `reservation_id`
- `account_id`
- `request_id`
- `workflow_id`
- `reserved_amount`

### 8.2 `billing.settlement.completed`

payload:

- `settlement_id`
- `reservation_id`
- `actual_amount`
- `delta_amount`
- `reference_type`
- `reference_id`

### 8.3 `task.workflow.terminal`

payload:

- `workflow_id`
- `public_task_id`
- `terminal_state`
- `result_url`
- `settlement_status`

## 9. 鉴权模型建议

### 9.1 控制面

- Cookie / Session
- Admin / Root RBAC
- 高风险操作二次验证

### 9.2 网关面

- Bearer token
- Token read/write scopes
- User quota state snapshot

### 9.3 内部 API

- mTLS 或 internal service token
- service-to-service scope

## 10. API 实施顺序

建议优先落地内部 API：

1. billing internal API
2. workflow internal API
3. gateway execution internal contract
4. 控制面新 API
5. 对外兼容层更新

## 11. 总结

CodeGo v2 的 API 契约必须体现模块边界，而不是继续让 controller 成为一切逻辑的入口。先把内部契约定清楚，外部兼容 API 才能长期稳定。
