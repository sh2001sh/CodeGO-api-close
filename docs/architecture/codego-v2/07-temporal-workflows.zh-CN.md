# CodeGo v2 Temporal 工作流实施文档

## 1. 文档目的

本文档定义 CodeGo v2 中使用 Temporal 落地的核心工作流。重点不是介绍 Temporal，而是说明 CodeGo 中哪些流程应该建成 workflow、各 workflow 的输入输出、activity 列表、重试策略和补偿逻辑。

## 2. 工作流总览

建议第一期实现四类 workflow：

- `RequestSettlementWorkflow`
- `AsyncTaskWorkflow`
- `OrderFulfillmentWorkflow`
- `SubscriptionResetWorkflow`

## 3. RequestSettlementWorkflow

### 3.1 适用场景

- 同步请求
- 流式请求

### 3.2 输入

- `request_id`
- `user_id`
- `token_id`
- `route_plan`
- `funding_policy`
- `request_payload`

### 3.3 输出

- `execution_status`
- `reservation_id`
- `settlement_id`
- `usage_evidence_id`

### 3.4 Activities

1. `CreateRequestExecution`
2. `CreateReservation`
3. `ExecuteProviderRequest`
4. `CollectUsageEvidence`
5. `CreateSettlement`
6. `PublishAuditEvents`

### 3.5 重试策略

- provider execute：有限重试，按 route plan fallback
- settlement：必须重试直到确认终态

### 3.6 补偿

若 provider 失败且 reservation 已创建：

- `ReleaseReservation`

## 4. AsyncTaskWorkflow

### 4.1 适用场景

- 视频生成
- 图片异步生成
- 其它有 submit + poll + terminal 的任务

### 4.2 输入

- `public_task_id`
- `request_id`
- `provider`
- `channel_id`
- `submit_payload`
- `reservation_id`

### 4.3 输出

- `terminal_state`
- `settlement_status`
- `result_url`

### 4.4 Activities

1. `SubmitAsyncTask`
2. `RecordTaskWorkflow`
3. `PollAsyncTaskStatus`
4. `RecordTaskSnapshot`
5. `FinalizeTaskTerminalState`
6. `SettleAsyncTask`
7. `RefundAsyncTask`
8. `ProjectTaskResult`

### 4.5 状态演化

- created
- submitted
- queued
- running
- succeeded / failed / timeout
- compensated（如需要）

### 4.6 重试策略

- poll：定时重试，支持指数退避
- finalize：必须幂等
- settlement/refund：必须幂等

### 4.7 超时策略

工作流内需持有：

- task timeout deadline
- max poll interval
- max retry count

### 4.8 补偿

- submit 成功但最终失败：refund
- submit 未知结果：进入人工核查或 retry-safe 状态

## 5. OrderFulfillmentWorkflow

### 5.1 输入

- `order_id`
- `product_id`
- `payment_provider`
- `amount`

### 5.2 Activities

1. `CreateOrderRecord`
2. `WaitPaymentCallback`
3. `ValidatePaymentCallback`
4. `MarkOrderPaid`
5. `GrantOrderBenefits`
6. `PublishOrderEvents`

### 5.3 补偿

- mark paid 成功但 grant 失败：继续重试 grant
- 不允许通过撤回 paid 来掩盖 grant 失败

## 6. SubscriptionResetWorkflow

### 6.1 适用场景

- 日/周/月订阅重置
- 自定义周期重置

### 6.2 Activities

1. `FindResettableSubscriptions`
2. `CreateResetLedgerEntries`
3. `ResetUsageProjection`
4. `PublishResetAuditEvents`

## 7. Workflow 输入输出 DTO 建议

所有 workflow 输入结构都必须：

- versioned
- 可序列化
- 不依赖运行时上下文

建议示例：

```go
type AsyncTaskWorkflowInput struct {
    WorkflowVersion string
    PublicTaskID    string
    RequestID       string
    ProviderCode    string
    ChannelID       int64
    ReservationID   string
    TaskKind        string
    SubmitPayload   json.RawMessage
}
```

## 8. Activity 设计原则

- 每个 activity 尽量短小
- 所有 activity 必须幂等
- Activity 失败必须可解释为：
  - retryable
  - non-retryable
  - requires manual intervention

## 9. Search Attributes 建议

为便于查询 Temporal workflow，建议注册以下 search attributes：

- `RequestID`
- `PublicTaskID`
- `UserID`
- `ProviderCode`
- `WorkflowType`
- `ReservationID`
- `SettlementStatus`

## 10. 与数据库的关系

Temporal 不是数据库替代品。CodeGo 中：

- Workflow runtime 状态在 Temporal
- 业务事实仍落 PostgreSQL

Temporal 负责“流程推进”，PostgreSQL 负责“业务事实持久化”。

## 11. 迁移建议

第一阶段不要求全部逻辑迁入 Temporal。建议顺序：

1. AsyncTaskWorkflow
2. OrderFulfillmentWorkflow
3. RequestSettlementWorkflow
4. SubscriptionResetWorkflow

原因：

- 异步任务当前收益最大
- 订单与权益发放适合工作流化

## 12. 总结

Temporal 工作流将把 CodeGo 当前分散在定时器、轮询器、controller 和 service 中的多阶段副作用流程收口为显式状态机。它不是性能优化工具，而是一致性和可恢复性工具。
