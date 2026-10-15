# CodeGo v2 第四层研发 Backlog

## 1. 文档目的

本文档把前三层与第四层实施包转换为可以排期的研发任务。每项任务都必须能独立验收、独立回滚、独立合并。

## 2. Epic 总览

| Epic | 名称 | 目标 |
| --- | --- | --- |
| E1 | 数据基础设施 | 建立 PostgreSQL schema、迁移体系、Outbox、审计基础 |
| E2 | 账本内核 | 建立账户、流水、预留、结算、退款、手动调整 |
| E3 | 网关执行内核 | 建立 request execution、route plan、统一 stream 生命周期 |
| E4 | 工作流内核 | 建立 Temporal worker、异步任务工作流、订阅重置工作流 |
| E5 | 管理与控制面 | 建立订单、商品、支付、管理员重置订阅配额 |
| E6 | 治理与观测 | 建立 tracing、metrics、对账、异常巡检 |

## 3. E1 数据基础设施

### T1.1 建立 schema 与迁移目录

- 产出：
  - `billing`
  - `gateway`
  - `workflow`
  - `readmodel`
  - `audit`
  - `platform`
- 验收：
  - 本地 PostgreSQL 可完整执行 migration
  - 重复执行时无破坏性错误

### T1.2 建立 outbox 事件表

- 验收：
  - 能写入事件
  - 能按未发布状态扫描
  - 能记录发布失败与重试次数

### T1.3 建立审计日志表

- 验收：
  - 管理员高风险操作可带 before/after
  - 支持按 actor 和资源检索

## 4. E2 账本内核

### T2.1 账户映射建立

- 范围：
  - user account
  - token account
  - subscription account
- 验收：
  - 任一计费主体都能映射到账本账户

### T2.2 账本流水应用服务

- 范围：
  - credit
  - debit
  - adjustment
  - refund
- 验收：
  - 同一幂等键重复提交不重复生效

### T2.3 reservation / settlement / refund

- 验收：
  - 成功请求可走 reservation -> settlement
  - 失败请求可走 reservation -> refund/release
  - 对账报表可看出差异金额

### T2.4 管理员手动重置订阅配额

- 验收：
  - 生成单独 ledger entry
  - 记录审计日志
  - 更新 period usage projection

## 5. E3 网关执行内核

### T3.1 request execution 建模

- 验收：
  - 每个请求都有 `request_id`
  - 可关联 route plan、provider attempt、usage evidence

### T3.2 max_tokens 全链路校验

- 范围：
  - API 入参校验
  - provider adapter 校验
  - fallback 前重新校验
  - stream/sync 一致性
- 验收：
  - 非法值在入口被拒绝
  - provider 上限超限时有明确错误码

### T3.3 非 OpenAI 流式链路断连停写

- 验收：
  - 客户端断开后 provider stream 停止继续写
  - 避免 goroutine 泄漏和无效 token 消耗

### T3.4 protected fetch 全路径接入

- 范围：
  - download
  - callback fetch
  - media proxy
  - webhook validation helper
- 验收：
  - 私网、回环、link-local、保留地址默认拒绝

## 6. E4 工作流内核

### T4.1 建立 workflow-worker 二进制

- 验收：
  - Worker 可启动并注册 workflow/activity

### T4.2 AsyncTaskWorkflow 第一版

- 验收：
  - submit / poll / terminal / settle / refund 闭环可跑通

### T4.3 SubscriptionResetWorkflow 第一版

- 验收：
  - 周期重置和管理员手动重置都能通过 workflow 编排

### T4.4 终态幂等与补偿

- 验收：
  - 重复 finalize 不会重复 settlement/refund

## 7. E5 管理与控制面

### T5.1 商品与定价控制面 API

- 验收：
  - 支持创建、查询、上下架

### T5.2 订单和支付回调

- 验收：
  - 支付回调通过幂等键去重
  - paid 与 benefit grant 可追踪

### T5.3 管理员订阅重置页面

- 验收：
  - 可选择订阅
  - 可填写原因
  - 可查看审计记录

## 8. E6 治理与观测

### T6.1 OTel 链路追踪

- 验收：
  - request -> provider -> workflow -> ledger 可串起 trace

### T6.2 计费一致性对账任务

- 验收：
  - 每日输出差异报告
  - 能定位 reservation 与 settlement 不一致记录

### T6.3 任务终态异常巡检

- 验收：
  - 超时未关闭 workflow 可报警

## 9. 推荐迭代顺序

### Sprint 1

- T1.1
- T1.2
- T2.1
- T3.1

### Sprint 2

- T2.2
- T2.3
- T3.2
- T3.3

### Sprint 3

- T3.4
- T4.1
- T4.2

### Sprint 4

- T4.3
- T4.4
- T5.2

### Sprint 5

- T5.1
- T5.3
- T6.1
- T6.2
- T6.3

## 10. Definition of Done

每个任务合并前必须满足：

- 代码已进入对应 bounded context
- 有最小可运行验证
- 幂等、审计、错误码已补齐
- 关键路径有 feature flag 或灰度开关
- 文档与 schema 已同步更新

## 11. 结论

这份 backlog 不是泛化待办，而是从 CodeGo v2 的目标架构直接反推的实施任务。按这个 backlog 执行，可以把第四层实施包真正转化为开发计划。
