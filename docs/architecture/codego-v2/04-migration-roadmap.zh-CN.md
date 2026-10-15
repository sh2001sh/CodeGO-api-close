# CodeGo v2 迁移实施路线图

## 1. 文档目的

本文档给出 CodeGo v2 从当前系统迁移到目标架构的实施路线图。目标不是抽象分阶段，而是提供可执行、可排期、可验收、可回滚的迁移计划。

## 2. 迁移总原则

### 2.1 不停机优先

只要不是极端必要，不采用大停机窗口完成迁移。优先使用：

- 双写
- 旁路验证
- 灰度切换
- shadow workflow

### 2.2 先建立新真相源，再迁业务入口

例如迁账本时，不是先把所有扣费逻辑删掉，而是先建立：

- 账户表
- reservation/settlement 表
- outbox

再让入口逐步切到新体系。

### 2.3 每一阶段必须可独立验收

每个阶段必须有：

- 目标
- 产出
- 风险
- 监控指标
- 回滚策略

## 3. 阶段总览

建议分四个阶段：

- Phase 0：准备期
- Phase 1：核心内核落地
- Phase 2：运行时拆分
- Phase 3：读模型与治理完善

## 4. Phase 0：准备期（2 周）

### 4.1 目标

- 不改主业务入口
- 完成架构基线、目录目标、表设计草案与 ADR
- 建立新模块目录骨架

### 4.2 任务清单

第 1 周：

- 建立 `docs/architecture/codego-v2/`
- 确认 bounded context owner
- 确认 PostgreSQL schema 方案
- 建立 `internal/platform`、`internal/billing`、`internal/workflow`、`internal/gateway`

第 2 周：

- 增加 outbox 表
- 增加 tracing correlation 方案
- 增加架构依赖检查脚本
- 增加 idempotency key 规范

### 4.3 验收标准

- 文档完整
- 新目录存在
- outbox 表可建
- 不影响现网功能

### 4.4 风险

- 团队对新边界理解不一致
- 新目录建了但旧习惯未变

### 4.5 回滚

准备期基本无需回滚，属于增量建设。

## 5. Phase 1：核心内核落地（6 到 8 周）

这是最关键的一阶段。

### 5.1 目标

- 建立账本内核
- 建立任务工作流内核
- 建立统一 stream 生命周期
- 建立 protected fetch
- 建立 provider contract

### 5.2 子阶段 1：账本内核（2 到 3 周）

#### 目标

- 建 `billing.accounts`
- 建 `billing.ledger_entries`
- 建 `billing.reservations`
- 建 `billing.settlements`
- 提供账本应用服务

#### 迁移策略

- 先为 user / token / subscription 创建账户映射
- 用初始化流水反映当前余额
- 旧余额字段保留
- 新请求开始旁路写账本流水

#### 验收

- 所有请求都可生成 reservation 记录
- settlement 与 refund 可重复调用而不重复生效
- 旧余额与新快照差异可对账

### 5.3 子阶段 2：统一请求执行记录（1 到 2 周）

#### 目标

- 建 `gateway.request_executions`
- 建 `gateway.execution_attempts`
- 建 `gateway.route_plans`

#### 迁移策略

- 现有入口在不改变响应的情况下记录 execution
- route decision 先以旁路模式输出

#### 验收

- 可按 request_id 追踪一条请求的执行路径

### 5.4 子阶段 3：统一流式生命周期（1 周）

#### 目标

- 所有 provider stream 最终收口到统一 stream writer
- 统一 disconnect handling
- 统一 usage footer 语义

#### 迁移策略

- 先抽统一接口
- 逐 provider 替换

#### 验收

- 所有流式 provider 通过统一断连测试

### 5.5 子阶段 4：protected fetch（1 周）

#### 目标

- 把用户可控 URL 拉取统一到 `platform-security/fetch`

#### 验收

- download、webhook、video proxy、通知类拉取路径全部接入统一校验

### 5.6 子阶段 5：任务工作流内核（2 周）

#### 目标

- 建 `workflow.task_workflows`
- 建 `workflow.task_snapshots`
- 建 `workflow.task_terminal_results`
- 统一异步任务终态与退款结算

#### 迁移策略

- 旧轮询逻辑继续跑
- 新 workflow 旁路记录与对比
- 确认结果一致后切主

#### 验收

- 异步任务终态可追踪
- 超时、退款、补扣路径统一

## 6. Phase 2：运行时拆分（4 到 6 周）

### 6.1 目标

- 从单进程拆出 `gateway-api`
- 拆出 `workflow-worker`
- 保留 `control-api` 承载控制面
- 新增 `ledger-worker`

### 6.2 子阶段 1：gateway-api（2 周）

#### 任务

- 建 `cmd/gateway-api`
- 搬迁执行面相关依赖装配
- 将后台和系统设置路径留在 control-api
- 让网关 API 独立发布

#### 验收

- 在线请求完全由 gateway-api 承接
- 后台不影响网关发布

### 6.3 子阶段 2：workflow-worker（1 到 2 周）

#### 任务

- 引入 Temporal worker
- 将任务轮询与终态结算迁入 workflow worker

#### 验收

- 异步任务更新不再依赖主 API 进程中的定时循环

### 6.4 子阶段 3：ledger-worker（1 周）

#### 任务

- 处理 outbox 分发
- 处理 balance snapshot 刷新
- 处理读模型投影

#### 验收

- 账本投影可异步重建

## 7. Phase 3：读模型与治理完善（4 周）

### 7.1 目标

- 查询彻底从主交易路径解耦
- 审计、趋势、报表建立专用读模型
- 引入对账和异常探测

### 7.2 任务

- 建 `readmodel.user_usage_daily`
- 建 `readmodel.channel_usage_daily`
- 建 `readmodel.billing_account_views`
- 审计日志统一化
- 配额异常对账任务

### 7.3 验收

- 后台报表不再重压主交易表
- 可定期输出账本一致性检查结果

## 8. 按周建议排期

### Week 1

- 架构定稿
- ADR 建立
- 新模块目录骨架

### Week 2

- outbox
- tracing correlation
- idempotency 规范

### Week 3-4

- billing accounts
- ledger entries
- reservations

### Week 5

- settlements
- snapshots
- 基础对账脚本

### Week 6

- request executions
- execution attempts
- route plans

### Week 7

- unified stream writer
- disconnect test suite

### Week 8

- protected fetch 落地

### Week 9-10

- workflow tables
- task workflow 旁路运行

### Week 11

- gateway-api 独立入口

### Week 12

- workflow-worker 启用

### Week 13

- ledger-worker 启用

### Week 14-15

- readmodel 投影
- 审计查询聚合

### Week 16

- 全链路对账与收尾

## 9. 风险清单

### 9.1 账本和旧余额不一致

风险来源：

- 旧逻辑仍在直接改余额
- 新账本旁路写入遗漏

控制措施：

- 双写期建立每日对账
- 关键入口只允许一个写入口

### 9.2 任务工作流与旧轮询冲突

风险来源：

- 旧轮询和新 workflow 同时推进终态

控制措施：

- 明确主副切换窗口
- 使用 workflow ownership 标记
- 终态 settlement 加幂等键

### 9.3 provider 改造过慢

控制措施：

- 先改高价值 provider
- 保留旧 adaptor shim

### 9.4 团队习惯回退

控制措施：

- PR 模板要求标明模块归属
- Lint 检查跨模块依赖

## 10. 回滚策略

### 10.1 表结构层

新增表和 schema 原则上只增不删，支持停用不支持直接物理回滚。

### 10.2 执行层

- gateway-api 可切回旧主进程入口
- workflow-worker 可降级回旧轮询器
- ledger-worker 可先暂停，只保留账本写入

### 10.3 业务层

切主前保留 feature flag：

- `use_new_ledger`
- `use_new_task_workflow`
- `use_protected_fetch`
- `use_gateway_v2_stream`

## 11. 关键验收指标

### 11.1 计费一致性

- reservation 成功率
- settlement 幂等命中率
- 日对账差异数
- 非预期 refund 数

### 11.2 任务一致性

- 终态平均收敛时间
- 重复终态推进次数
- timeout 后未补偿数量

### 11.3 网关质量

- stream disconnect 停写成功率
- provider fallback 成功率
- usage evidence 记录覆盖率

### 11.4 运营质量

- 报表查询耗时
- audit 查询可用性
- 配置变更审计覆盖率

## 12. 实施包依赖关系

实施顺序依赖：

- 账本先于任务工作流切主
- request execution 先于 observability 强化
- protected fetch 先于 URL 拉取路径治理完成
- gateway-api 拆分先于大规模性能调优

## 13. 最终落地建议

CodeGo v2 的迁移不是“边写边自然演化”就能完成的，必须有明确项目化推进。建议把本路线图作为技术项目立项，并至少设置：

- 架构 owner
- 账本 owner
- workflow owner
- gateway owner
- 审计 owner

## 14. 总结

这份路线图的意义在于让 CodeGo v2 不再停留在概念架构层，而具备明确的实施路径。它既避免了大爆炸式重写，也避免了无方向的渐进补丁。只要严格按阶段推进，CodeGo 可以在不中断业务的前提下完成一次真正的底层升级。
