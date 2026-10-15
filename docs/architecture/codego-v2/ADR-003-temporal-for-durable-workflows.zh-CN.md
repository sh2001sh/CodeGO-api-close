# ADR-003 使用 Temporal 编排长事务和异步副作用

## 状态

Accepted

## 背景

CodeGo 存在大量跨步骤、跨时间、跨外部系统的流程，例如异步视频任务、支付成功后权益发放、订阅周期重置、请求执行后结算。传统 controller + service + timer 的方式难以保证恢复性和幂等。

## 决策

CodeGo v2 对长事务和异步副作用流程采用 Temporal 编排。Temporal 负责推进流程状态，PostgreSQL 负责持久化业务事实。

第一阶段工作流包括：

- `AsyncTaskWorkflow`
- `OrderFulfillmentWorkflow`
- `SubscriptionResetWorkflow`
- `RequestSettlementWorkflow`

## 后果

正面：

- 流程中断后可恢复
- 重试、补偿、超时可标准化
- 终态一致性显著提升

负面：

- 增加新的基础设施依赖
- 需要团队掌握 workflow 设计方式

## 落地约束

- workflow 不替代数据库
- activity 必须幂等
- 终态写操作必须有唯一幂等键
