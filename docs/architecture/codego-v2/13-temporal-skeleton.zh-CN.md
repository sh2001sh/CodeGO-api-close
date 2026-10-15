# CodeGo v2 Temporal Skeleton 实施包

## 1. 文档目的

本文档不是继续解释为什么要引入 Temporal，而是把 CodeGo v2 第一版可实施骨架直接定出来。开发时可以按本文档创建包结构、接口、worker 启动器、workflow 和 activity。

## 2. 目标目录

建议在未来重组后的仓库中形成如下结构：

```text
cmd/
  workflow-worker/
    main.go
internal/
  workflow/
    app/
      orchestrator.go
    temporal/
      bootstrap.go
      worker_registry.go
      workflows/
        async_task_workflow.go
        request_settlement_workflow.go
        order_fulfillment_workflow.go
        subscription_reset_workflow.go
      activities/
        billing_activities.go
        gateway_activities.go
        task_activities.go
        order_activities.go
      contracts/
        workflow_inputs.go
        workflow_outputs.go
        activity_errors.go
```

## 3. Worker 启动器骨架

### 3.1 Bootstrap 责任

- 读取 Temporal 地址、namespace、task queue
- 初始化 logger、tracer、db、provider registry
- 注册 workflow 和 activity
- 以优雅停机方式启动 worker

### 3.2 推荐环境变量

```text
TEMPORAL_HOSTPORT
TEMPORAL_NAMESPACE
TEMPORAL_TASK_QUEUE_TASKS
TEMPORAL_TASK_QUEUE_BILLING
TEMPORAL_TASK_QUEUE_ORDERS
TEMPORAL_TASK_QUEUE_SUBSCRIPTIONS
```

### 3.3 启动器示例

```go
type WorkerBootstrap struct {
    Client temporalclient.Client
    Logger *zap.Logger
    DB     *sql.DB
    Deps   WorkflowDependencies
}

func (b *WorkerBootstrap) RegisterAll(w worker.Worker) {
    w.RegisterWorkflow(workflows.AsyncTaskWorkflow)
    w.RegisterWorkflow(workflows.RequestSettlementWorkflow)
    w.RegisterWorkflow(workflows.OrderFulfillmentWorkflow)
    w.RegisterWorkflow(workflows.SubscriptionResetWorkflow)

    w.RegisterActivity(b.Deps.TaskActivities)
    w.RegisterActivity(b.Deps.BillingActivities)
    w.RegisterActivity(b.Deps.GatewayActivities)
    w.RegisterActivity(b.Deps.OrderActivities)
}
```

## 4. Workflow 合约

### 4.1 AsyncTaskWorkflow

输入：

- `workflow_version`
- `public_task_id`
- `request_id`
- `account_id`
- `provider_code`
- `channel_id`
- `reservation_id`
- `task_kind`
- `submit_payload`
- `timeout_at`

输出：

- `terminal_state`
- `settlement_status`
- `result_url`
- `failure_reason`

核心流程：

1. `SubmitAsyncTask`
2. `RecordTaskWorkflow`
3. 循环 `PollAsyncTaskStatus`
4. 每轮 `RecordTaskSnapshot`
5. 终态后 `FinalizeTaskTerminalState`
6. 成功则 `SettleAsyncTask`
7. 失败或超时则 `RefundAsyncTask`
8. `PublishTaskWorkflowTerminalEvent`

### 4.2 RequestSettlementWorkflow

输入：

- `workflow_version`
- `request_id`
- `account_id`
- `route_plan_id`
- `request_payload`
- `funding_policy`

输出：

- `execution_status`
- `reservation_id`
- `settlement_id`
- `usage_evidence_id`

核心流程：

1. `CreateRequestExecution`
2. `CreateReservation`
3. `ExecuteProviderRequest`
4. `CollectUsageEvidence`
5. `CreateSettlement`
6. `PublishRequestSettledEvent`

### 4.3 OrderFulfillmentWorkflow

核心流程：

1. `CreateOrderRecord`
2. `WaitPaymentCallbackSignal`
3. `ValidatePaymentCallback`
4. `MarkOrderPaid`
5. `GrantOrderBenefits`
6. `PublishOrderPaidEvent`

### 4.4 SubscriptionResetWorkflow

核心流程：

1. `FindResettableSubscriptions`
2. `CreateResetLedgerEntries`
3. `ResetUsageProjection`
4. `PublishResetAuditEvents`

## 5. Activity 分组

### 5.1 BillingActivities

- `CreateReservation`
- `CreateSettlement`
- `RefundReference`
- `CreateResetLedgerEntries`
- `RefreshAccountSnapshot`

### 5.2 TaskActivities

- `SubmitAsyncTask`
- `PollAsyncTaskStatus`
- `RecordTaskWorkflow`
- `RecordTaskSnapshot`
- `FinalizeTaskTerminalState`
- `ProjectTaskResult`

### 5.3 GatewayActivities

- `CreateRequestExecution`
- `ExecuteProviderRequest`
- `CollectUsageEvidence`
- `PublishRequestSettledEvent`

### 5.4 OrderActivities

- `CreateOrderRecord`
- `ValidatePaymentCallback`
- `MarkOrderPaid`
- `GrantOrderBenefits`
- `PublishOrderPaidEvent`

## 6. Workflow 输入输出结构建议

```go
type AsyncTaskWorkflowInput struct {
    WorkflowVersion string
    PublicTaskID    string
    RequestID       string
    AccountID       string
    ProviderCode    string
    ChannelID       int64
    ReservationID   string
    TaskKind        string
    SubmitPayload   json.RawMessage
    TimeoutAt       time.Time
}

type AsyncTaskWorkflowOutput struct {
    TerminalState    string
    SettlementStatus string
    ResultURL        string
    FailureReason    string
}
```

## 7. Workflow 选项建议

### 7.1 AsyncTaskWorkflow

- `WorkflowExecutionTimeout`: 24h
- `WorkflowRunTimeout`: 24h
- `WorkflowTaskTimeout`: 15s
- `RetryPolicy`: 仅对可重试错误生效

### 7.2 Activity 默认重试

- `InitialInterval`: 2s
- `BackoffCoefficient`: 2.0
- `MaximumInterval`: 1m
- `MaximumAttempts`: 5

### 7.3 非重试错误

必须分类为：

- 参数非法
- 账户不存在
- 配额不足
- provider 明确拒绝
- 幂等冲突且语义不一致

## 8. Signal 和 Query 约束

### 8.1 推荐 Signal

- `payment_callback_received`
- `manual_retry_requested`
- `manual_cancel_requested`

### 8.2 推荐 Query

- `current_state`
- `last_provider_snapshot`
- `reservation_status`
- `settlement_status`

## 9. 幂等与补偿

### 9.1 Activity 幂等键

每个 activity 都必须显式接收幂等字段，至少包含：

- `request_id`
- `workflow_id`
- `step_name`
- `attempt_group`

### 9.2 补偿策略

- 创建了 reservation 但 provider 未执行成功：释放 reservation
- 异步任务已提交但终态失败：执行 refund
- 已标记 paid 但权益发放失败：继续补偿 grant，不回滚 paid

## 10. 可观测性约束

所有 workflow/activity 日志必须带：

- `workflow_id`
- `run_id`
- `request_id`
- `public_task_id`
- `account_id`
- `provider_code`

所有 workflow 都应注册 Search Attributes：

- `RequestID`
- `PublicTaskID`
- `AccountID`
- `ProviderCode`
- `WorkflowType`
- `TerminalState`

## 11. 开发落地顺序

1. 先落 `AsyncTaskWorkflow`
2. 再落 `OrderFulfillmentWorkflow`
3. 再落 `SubscriptionResetWorkflow`
4. 最后把同步请求逐步迁入 `RequestSettlementWorkflow`

## 12. 与现有代码的接入方式

- 第一阶段不替换所有旧逻辑
- 先由现有 service/controller 在关键入口调用 workflow client
- workflow 成为新副作用的唯一推进器
- 旧轮询器与新 workflow 只允许一个持有终态写权限

## 13. 交付建议

第四层实施完成后，下一步应直接创建：

- `internal/workflow/temporal/contracts`
- `internal/workflow/temporal/workflows`
- `internal/workflow/temporal/activities`
- `cmd/workflow-worker`

本文档已经足以让开发人员开始写第一版 Temporal worker，不需要再回到概念方案阶段。
