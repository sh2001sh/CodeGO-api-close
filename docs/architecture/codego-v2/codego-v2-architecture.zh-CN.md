# CodeGo v2 架构设计书

## 1. 文档目的

本文档是 CodeGo 项目的 v2 架构设计书，用于定义项目未来 3 到 5 年的目标架构、系统边界、核心模块、数据模型、运行模型、治理方式与迁移路线。本文档不是抽象的技术愿景文档，而是面向实施、评审、拆解、排期和跨团队协同的架构基线文档。其目标是为 CodeGo 建立一套可长期演进、可稳定交付、可承载复杂计费和异步任务场景、可支持多供应商大模型接入、可保持审计与运营可控性的系统方案。

本文档针对的是已经完成品牌重构、业务面显著扩展、核心交易链路复杂度持续上升的 CodeGo 平台，而不是早期的通用代理项目。CodeGo 未来不再被定义为“单纯的 OpenAI 兼容网关”，而是一个集成了统一 AI 接入、账号体系、套餐与订阅、钱包与积分、异步任务编排、工作流结算、桌面端接入、运营后台和审计能力的 AI 平台型系统。

本文档的架构目标有五个：

1. 让核心交易链路具备长期稳定的一致性基础，重点是计费、订阅额度、钱包额度、令牌额度、异步任务终态和日志审计。
2. 让系统可以继续扩展 provider、模型、业务域和客户端形态，而不会持续放大跨模块耦合和补丁式修复成本。
3. 让系统可以在保持当前交付速度的同时，逐步演进到更清晰的控制面、执行面和工作流面，而不是一步走向高复杂度的全量微服务。
4. 让安全、审计、观测和风控成为基础设施能力，而不是散落在 controller、service 和 relay 中的附属逻辑。
5. 让架构从一开始就具备可拆分、可部署、可治理的边界，即使起步仍采用单仓和有限进程部署。

## 2. 设计原则

### 2.1 业务优先于技术炫技

CodeGo 的主要复杂度不在于页面渲染，也不在于基础 CRUD，而在于多供应商接入、异步任务、流式输出、额度体系与支付订阅。因此架构设计必须围绕真实复杂度展开，不能为了追求“云原生”“微服务”“事件驱动”“Serverless”等技术标签而引入对当前团队和业务并不友好的复杂度。

### 2.2 一致性优先于弹性扩展

对于 CodeGo 而言，最昂贵的错误不是偶发响应稍慢，而是：

- 用户额度扣错
- 订阅额度漏扣或重复扣
- 异步任务终态后未退款或重复退款
- 流式请求中断后结算不一致
- 提供商返回和平台内部使用量记录不一致

因此，v2 架构把一致性作为第一优先级，把弹性扩展作为第二优先级。只有在保证账本与工作流一致性的基础上，才允许进一步服务拆分和高并发优化。

### 2.3 模块边界必须先于部署边界

如果没有清晰的模块边界，直接拆服务只会把进程内混乱变成网络间混乱。CodeGo v2 必须先定义 bounded context、领域模型、职责边界和依赖方向，再决定哪些模块应该独立部署、哪些模块应该仍然共进程运行。

### 2.4 写路径集中，读路径解耦

主交易链路必须尽量短、尽量确定、尽量可验证。运营查询、图表、排行榜、日志检索、后台聚合统计等功能不能持续压在主交易表和主写链路上。v2 架构要求对读路径进行分离，读模型通过事件、outbox 或异步投影生成。

### 2.5 适配器只做协议适配，不承载核心业务规则

Provider 适配层在过去很容易不断吸收各种计费、状态机、流式输出、参数修复和特殊逻辑，最终导致问题难以收口。v2 要求 provider adaptor 只负责：

- 请求映射
- 响应解析
- provider 特有能力声明
- provider 特有结果补充信息

核心业务规则如计费、任务终态补偿、额度调整、审计记录、统一错误模型，都必须回到统一内核。

### 2.6 先模块化单体，后有限服务化

CodeGo v2 的推荐路径不是全量微服务，而是：

- 单仓
- 模块化单体代码结构
- 少量独立二进制
- 单主库起步
- 通过 outbox、workflow 和账本建立长期可扩展基础

只有在流量、组织规模或合规要求真正推动时，才按模块继续物理拆分。

## 3. 当前系统问题总结

### 3.1 当前系统的客观状态

当前 CodeGo 的代码已经远超早期“AI 代理项目”的复杂度范畴。它同时承担了以下角色：

- OpenAI 兼容 API 网关
- 多 provider 路由器
- 订阅和钱包平台
- 异步视频/图像/任务编排平台
- 令牌管理与配额管理平台
- 桌面端与小程序接入平台
- 后台运营与系统设置平台
- 审计、日志与性能数据平台

这意味着它已经是一个 AI 平台型应用，而不是单一服务。

### 3.2 当前问题的根因

当前系统暴露出的许多问题，本质根因并非技术栈过旧，而是核心边界没有从平台化角度重新划定。具体表现如下。

#### 3.2.1 写路径分散

额度变更可能发生在：

- 同步请求预扣费
- 流式请求完成结算
- 请求失败退款
- 异步任务终态退款
- 异步任务差额结算
- webhook 支付回调
- 管理后台手工操作
- 订阅重置任务

这些操作散落在多个 service、controller 和 task 轮询逻辑中，导致同一类业务结果无法通过一个统一内核解释。

#### 3.2.2 任务状态机与计费状态机分离但耦合

异步任务的状态由 provider 返回、轮询循环、超时清理、用户查询实时刷新等多个入口驱动。计费又依赖任务状态是否进入终态、是否有真实使用量、是否需要差额结算。这意味着：

- 状态变化并不是单点发生
- 计费并不是单点结算
- 需要大量 CAS 和补偿逻辑防止重复执行

该模型继续扩展会显著提高出错率。

#### 3.2.3 Provider 扩展成本持续上升

每新增一个 provider，往往不只是增加一个请求映射器，而是会牵连：

- 参数校验差异
- 流式协议差异
- 终态数据差异
- 计费返回差异
- 任务状态查询差异
- 音频/图像/视频特殊返回差异

如果这些差异不被约束在 adapter contract 内，系统就会继续通过补丁增长。

#### 3.2.4 安全能力缺少统一出口层

SSRF 校验、session cookie、安全跳转、敏感 webhook、防止断连后继续写流等逻辑虽然已经逐步加强，但它们还不是统一基础设施能力。这意味着安全逻辑仍然容易：

- 被新代码绕过
- 在某个子链路漏补
- 因为开发习惯不统一而重复实现

#### 3.2.5 读写混用

日志、报表、后台查询、管理面批量操作、统计页面和主交易路径共享大量相同数据库表和模型。随着数据量增长，这将带来：

- 查询干扰写入
- 聚合统计成本上升
- 主表结构被后台需求反向驱动

#### 3.2.6 前端业务域膨胀

前端技术不旧，但业务域已经非常多，且同时包含：

- 公共站点
- 用户控制台
- 系统后台
- 复杂表格系统
- 多端接入引导

如果不进一步按产品域与 shell 边界整理，未来会遇到构建体积、权限耦合和组件复用边界不清的问题。

## 4. v2 目标定义

CodeGo v2 的目标不是做成“最时髦”的系统，而是做成“未来五年内最稳、最可扩展、最可审计、最适合团队维护”的系统。为此，定义以下目标。

### 4.1 平台级目标

- 支持多 provider、多模型、多协议、多客户端接入
- 支持钱包、令牌、订阅、套餐、促销等多种额度与资金源
- 支持同步请求、流式请求、异步任务三种执行模式
- 支持运营、审计、对账、风控、安全治理
- 支持多进程部署与水平扩展

### 4.2 技术级目标

- 核心写路径显式建模
- 工作流显式建模
- 账本显式建模
- provider 能力显式建模
- 事件与读模型显式建模
- 安全策略显式建模

### 4.3 演进级目标

- 第一天可用
- 第一年不需要推翻
- 三年后仍可继续拆分
- 五年内不必因架构错误而重建基础层

## 5. 总体架构

CodeGo v2 采用“单仓多二进制、模块化平台”的总体架构。逻辑上分为五个平面。

### 5.1 Control Plane

控制面负责平台配置与管理，不直接承担高并发模型请求流量。其职责包括：

- 身份认证、角色权限、会话管理
- 用户资料、组织与分组
- 渠道、provider、模型目录管理
- 价格策略、倍率策略、风控规则
- 钱包、订阅、套餐配置
- 桌面端、小程序、后台设置
- 管理员操作、手工调账、审计审批

控制面可以独立部署为 `control-api`，并在长远上维持相对低频迭代、强权限、强审计的产品形态。

### 5.2 Gateway Plane

执行面负责所有在线请求的实际入口与转发，是平台的低延迟核心。职责包括：

- API 协议接入
- 请求身份识别
- 参数规范化
- 基础安全校验
- 路由与 provider 选择
- 供应商请求执行
- 流式响应生命周期管理
- 使用量证据采集
- 调用结果标准化

执行面要尽量瘦，避免承载复杂的后台配置和非在线业务逻辑。它可以独立部署为 `gateway-api`，并通过水平扩容承载高并发。

### 5.3 Workflow Plane

工作流面负责一切跨时间、多阶段、副作用显著的流程，尤其是异步任务、回调处理、对账和补偿流程。职责包括：

- 异步任务提交后工作流实例创建
- provider 轮询与终态判断
- 超时、重试、指数退避
- 任务终态后的账本结算与补偿
- 支付回调与订单状态推进
- 周期性重置、清理与派生任务

工作流面建议独立为 `workflow-worker`，使用持久化工作流引擎实现，避免继续用松散的定时轮询和 service 调用拼装复杂状态机。

### 5.4 Ledger Plane

账本面是整个系统的交易事实中心。所有额度与资金变化必须通过账本实现。职责包括：

- 预留（reservation）
- 结算（settlement）
- 退款（refund）
- 调账（adjustment）
- 订阅周期重置生成的额度事件
- 钱包、令牌、订阅三种账户类型统一抽象

账本面不面向终端用户暴露复杂接口，而是作为内部交易系统，由 `ledger-worker` 和 `control-api` 控制。它是 v2 的绝对关键模块。

### 5.5 Read Model Plane

读模型面负责所有查询优化和运营分析，包括：

- 使用日志查询
- 趋势统计
- 后台报表
- 审计检索
- 渠道使用概览
- 订阅和钱包财务视图

读模型通过 outbox 事件或 CDC 异步生成，避免对主交易表形成持续高压。

## 6. 部署形态

### 6.1 推荐的一期部署形态

v2 的推荐起步形态为四个二进制：

- `control-api`
- `gateway-api`
- `workflow-worker`
- `ledger-worker`

配套基础设施：

- PostgreSQL
- Redis
- Temporal
- S3 或 MinIO
- Prometheus + Grafana
- OpenTelemetry Collector

这不是“微服务化全拆”，而是有限服务化。其优点是：

- 在线网关与后台配置解耦
- 工作流与账本可独立扩展
- 控制面发布不会影响高并发执行面
- 后续如需拆更多服务，基础边界已经具备

### 6.2 不推荐的一期方案

以下方案不推荐作为 v2 一期形态：

- 全量单进程保留所有能力
- 一开始拆成十几个微服务
- 一开始拆库拆表拆团队
- 一开始强依赖 Kafka 等重型消息基础设施

原因是这些路径要么保留了当前问题，要么过度放大了组织和维护复杂度。

## 7. 领域边界设计

CodeGo v2 采用 bounded context 划分业务域。以下边界一经确定，应作为代码组织、数据库建模、服务 API 和治理规范的长期依据。

### 7.1 Identity Context

职责：

- 用户
- 认证
- 会话
- OAuth 绑定
- 2FA / passkey
- 客户端授权设备

关键实体：

- User
- Session
- OAuthBinding
- AuthChallenge
- DeviceAuthorization

不负责：

- 钱包余额
- 订阅规则
- 请求执行

### 7.2 Gateway Routing Context

职责：

- 模型名与 provider 能力匹配
- 渠道选择
- 路由策略
- fallback 策略
- 区域与代理策略

关键实体：

- ProviderChannel
- ModelCatalogEntry
- RoutingPolicy
- RoutePlan

不负责：

- 具体执行
- 计费扣减
- 任务结算

### 7.3 Provider Execution Context

职责：

- 对接 provider
- 标准化请求与响应
- 标准化流式输出
- 采集 provider 结果证据

关键实体：

- ExecutionRequest
- ExecutionAttempt
- ProviderCapability
- StreamChunkEvidence

不负责：

- 业务价格规则
- 钱包和订阅变更
- 持久化任务状态机

### 7.4 Billing Ledger Context

职责：

- 额度账户
- 账本流水
- 预留
- 结算
- 退款
- 调账

关键实体：

- BillingAccount
- LedgerEntry
- Reservation
- Settlement
- Adjustment

不负责：

- UI 套餐配置页面
- provider 协议转换

### 7.5 Async Tasks Context

职责：

- 异步任务生命周期
- 工作流编排
- provider 轮询
- 终态判定
- 工作流补偿

关键实体：

- TaskWorkflow
- TaskExecution
- TaskSnapshot
- TaskTerminalResult

不负责：

- 钱包操作
- 用户认证

### 7.6 Commerce Context

职责：

- 订阅计划
- 套餐
- 订单
- 支付回调
- 升级/续费/转换规则

关键实体：

- Product
- SubscriptionPlan
- Order
- PaymentAttempt
- BenefitGrant

### 7.7 Audit & Observability Context

职责：

- 审计日志
- 使用日志
- 管理员行为
- 风控证据
- 链路追踪
- 性能指标

关键实体：

- AuditEvent
- UsageLog
- AdminActionLog
- MetricSample
- TraceCorrelation

### 7.8 Admin Ops Context

职责：

- 后台操作编排
- 渠道检测
- 批量修复
- 运维任务
- 安全开关

关键实体：

- AdminCommand
- MaintenanceJob
- ConfigChange

## 8. 核心运行单元设计

### 8.1 control-api

职责：

- 提供后台与用户控制台管理 API
- 管理配置项、渠道、模型、订阅、订单、活动
- 提供用户自助 API
- 提供审计查询入口

设计要求：

- 强 RBAC
- 强审计
- 不承载大流量推理请求
- 不承担长时工作流执行

### 8.2 gateway-api

职责：

- 提供 OpenAI 兼容接口与其它兼容协议入口
- 生成 request execution 记录
- 执行 provider 调用
- 驱动 reservation
- 输出标准化响应

设计要求：

- 无业务后台复杂逻辑
- 尽量无状态
- 支持水平扩容
- 所有流式写入统一出口

### 8.3 workflow-worker

职责：

- 运行持久化工作流
- 调度异步任务轮询
- 接收支付/回调事件
- 处理终态补偿与触发结算

设计要求：

- 具备幂等性
- 可重放
- 失败可恢复
- 状态机清晰

### 8.4 ledger-worker

职责：

- 异步执行账本派生动作
- 聚合账户余额快照
- 驱动读模型投影
- 支持审计校验与对账

设计要求：

- 严格幂等
- 允许重建读模型
- 不直接暴露用户接口

## 9. 数据架构设计

### 9.1 总体原则

- 主交易写路径以 PostgreSQL 为准
- 业务账户、任务、订单和账本在一个主库内维护事务一致性
- Redis 只做缓存、限流和短期状态，不作为交易事实源
- 读模型可以复用同一库中的独立 schema，也可逐步迁出

### 9.2 建议 schema 划分

建议在同一 PostgreSQL 实例中引入 schema 边界：

- `identity`
- `gateway`
- `billing`
- `workflow`
- `commerce`
- `audit`
- `readmodel`
- `ops`

这样做的目的不是为了炫耀数据库设计，而是让边界在代码之外也具备可见性，便于未来迁移、权限隔离和运维治理。

### 9.3 账户与余额模型

CodeGo v2 不建议继续把“当前余额字段”当作唯一真相，而应该采用“账本 + 余额快照”的双层模型。

#### 9.3.1 BillingAccount

每一种可消费资金源都应该映射为标准化账户。建议结构如下：

- `account_id`
- `account_type`
  - wallet
  - token
  - subscription
  - promo
- `owner_type`
  - user
  - token
  - subscription_instance
- `owner_id`
- `quota_unit`
  - unified_quota
  - credits
  - token_quota
- `status`
- `version`
- `created_at`
- `updated_at`

#### 9.3.2 LedgerEntry

账本流水是唯一事实记录。建议字段：

- `entry_id`
- `account_id`
- `request_id`
- `task_id`
- `order_id`
- `entry_type`
  - reserve
  - settle
  - refund
  - adjust
  - grant
  - expire
  - reset
- `direction`
  - debit
  - credit
- `amount`
- `balance_after`
- `idempotency_key`
- `reason_code`
- `reason_detail`
- `created_at`
- `operator_type`
- `operator_id`

#### 9.3.3 BalanceSnapshot

为优化查询，可维护余额快照，但它必须从账本派生，而不是反过来成为真相。

字段：

- `account_id`
- `available_balance`
- `reserved_balance`
- `consumed_total`
- `refunded_total`
- `updated_at`

### 9.4 请求执行模型

#### 9.4.1 RequestExecution

每个在线请求都应该有自己的执行记录。建议字段：

- `request_id`
- `client_request_id`
- `user_id`
- `token_id`
- `route_plan_id`
- `request_mode`
  - sync
  - stream
  - async_submit
- `relay_format`
- `origin_model`
- `resolved_model`
- `provider`
- `channel_id`
- `status`
  - received
  - validated
  - reserved
  - executing
  - completed
  - failed
  - canceled
- `started_at`
- `completed_at`
- `trace_id`

#### 9.4.2 RequestUsageEvidence

使用量证据必须与业务结算分开存储。建议字段：

- `request_id`
- `provider_usage_raw`
- `normalized_prompt_tokens`
- `normalized_completion_tokens`
- `normalized_total_tokens`
- `stream_chunk_count`
- `pricing_basis`
- `evidence_confidence`
- `recorded_at`

这样后续才能做：

- 审计
- 差异分析
- 账单争议排查
- 模型替换风险分析

### 9.5 异步任务模型

#### 9.5.1 TaskWorkflow

任务主记录必须更多地表达工作流状态，而不仅是 UI 状态。建议字段：

- `workflow_id`
- `public_task_id`
- `upstream_task_id`
- `request_id`
- `provider`
- `channel_id`
- `task_kind`
- `state`
  - created
  - submitted
  - queued
  - running
  - succeeded
  - failed
  - timeout
  - compensated
- `billing_reservation_id`
- `settlement_id`
- `retry_policy`
- `timeout_at`
- `created_at`
- `updated_at`

#### 9.5.2 TaskSnapshot

保存 provider 视角下的原始状态快照：

- `snapshot_id`
- `workflow_id`
- `provider_state`
- `provider_progress`
- `raw_payload`
- `result_url`
- `failure_reason`
- `recorded_at`

#### 9.5.3 TaskResultMaterialization

面向用户展示的结果聚合：

- `workflow_id`
- `display_status`
- `progress_percent`
- `result_asset_url`
- `result_metadata`
- `finished_at`

### 9.6 商业与支付模型

商业模型建议统一成“产品、订单、权益发放”三层，而不是支付直接改余额。

#### 9.6.1 Product

- subscription_plan
- topup_package
- blind_box_pack
- campaign_bundle

#### 9.6.2 Order

字段：

- `order_id`
- `user_id`
- `product_id`
- `product_type`
- `payment_provider`
- `payment_status`
- `business_status`
- `amount`
- `currency`
- `idempotency_key`
- `created_at`
- `paid_at`

#### 9.6.3 BenefitGrant

字段：

- `grant_id`
- `order_id`
- `benefit_type`
- `target_account_id`
- `quota_amount`
- `effective_from`
- `effective_to`
- `status`

## 10. 事件架构设计

### 10.1 为什么必须事件化

CodeGo 当前最容易继续出错的地方，在于：

- 写入动作完成了，但日志未写
- 订单支付成功了，但权益发放未完成
- 异步任务终态变了，但结算未完成
- 流式请求结束了，但审计视图未更新

要解决这些问题，v2 必须引入标准 outbox 事件模型。

### 10.2 事件发布原则

- 所有影响下游读模型或异步动作的主写操作，在同一事务内写入 outbox
- 业务事务先提交，事件异步投递
- 所有消费者必须幂等

### 10.3 建议核心事件

- `request.execution.created`
- `request.execution.completed`
- `billing.reservation.created`
- `billing.settlement.completed`
- `billing.refund.completed`
- `task.workflow.created`
- `task.workflow.transitioned`
- `task.workflow.terminal`
- `commerce.order.paid`
- `commerce.benefit.granted`
- `subscription.quota.reset`
- `admin.config.changed`

### 10.4 Outbox 表结构建议

- `event_id`
- `aggregate_type`
- `aggregate_id`
- `event_type`
- `payload`
- `headers`
- `status`
  - pending
  - dispatched
  - failed
- `retry_count`
- `next_retry_at`
- `created_at`
- `dispatched_at`

## 11. 工作流架构设计

### 11.1 采用持久化工作流的原因

CodeGo 的异步任务、支付回调、订阅重置、本地清理任务、批量修复任务，本质上都属于跨时间、跨重试、多副作用的流程。继续用：

- goroutine
- cron
- service 直接相互调用
- 手工 CAS

去承载所有流程，维护成本会越来越高。v2 建议使用 Temporal 作为工作流引擎，理由如下：

- 工作流状态持久化
- 可重试
- 可回放
- 步骤可观察
- 补偿逻辑清晰
- 适合异步任务终态与计费协同

### 11.2 工作流分类

#### 11.2.1 Request Settlement Workflow

用于同步或流式请求的结算：

1. 创建 execution record
2. 创建 reservation
3. 调用 provider
4. 收集 usage evidence
5. 计算 settlement
6. 写账本
7. 发布审计事件

#### 11.2.2 Async Task Workflow

用于视频/图像等异步任务：

1. 提交 provider
2. 记录 upstream_task_id
3. 周期轮询
4. 处理 timeout / retry
5. 达到终态
6. 执行 settlement 或 refund
7. 物化结果

#### 11.2.3 Order Fulfillment Workflow

用于支付与权益发放：

1. 创建订单
2. 等待支付回调
3. 校验签名与状态
4. 标记 paid
5. 发放权益
6. 写账本 grant 事件
7. 发布用户可见状态

#### 11.2.4 Subscription Reset Workflow

用于周期性订阅额度重置：

1. 选中到期订阅实例
2. 创建 reset ledger entry
3. 写 usage reset 事件
4. 发布审计日志

### 11.3 工作流设计原则

- 每个 workflow 都必须有业务 ID 和幂等键
- 每个外部副作用步骤都必须显式建模
- 每个终态必须区分：
  - business failed
  - infrastructure failed
  - compensated
- 不允许把关键工作流状态只存在内存中

## 12. 计费与账本架构设计

### 12.1 目标

CodeGo v2 的计费系统要解决以下长期问题：

- 统一钱包、订阅、令牌三种来源
- 统一同步请求、流式请求、异步任务三种消费模式
- 支持预留与终态结算
- 支持退款、补扣、部分退款、调账
- 具备可审计性和可追溯性

### 12.2 Reservation 模型

预留表示“先锁定一部分消费能力”，并不等于最终消费。字段建议：

- `reservation_id`
- `request_id` 或 `workflow_id`
- `account_id`
- `reserved_amount`
- `status`
  - open
  - settled
  - released
  - expired
- `created_at`
- `expired_at`

### 12.3 Settlement 模型

结算表示把预留转化为真实消费。字段建议：

- `settlement_id`
- `reservation_id`
- `actual_amount`
- `delta_amount`
- `status`
- `usage_evidence_id`
- `settled_at`

### 12.4 退款模型

退款不应该是“把余额字段加回去”，而应该始终作为账本 credit entry。这样可以：

- 支持审计
- 支持多来源退款
- 支持部分退款
- 支持对账

### 12.5 统一消费过程

所有消费过程统一如下：

1. 计算候选费用
2. 生成 reservation
3. 执行请求或任务
4. 采集 usage evidence
5. 生成 settlement
6. 若失败或超时则 release/refund

### 12.6 账户优先级

账户顺序不能散落在业务逻辑里，应由规则引擎或策略配置控制。推荐模型：

- billing preference profile
- source priority list
- per-product funding policy

例如：

- 订阅优先
- 钱包优先
- 某些任务只能用订阅
- 某些模型不允许使用赠送额度

## 13. 网关执行架构设计

### 13.1 请求处理阶段

统一请求链路建议分为以下阶段：

1. ingress
2. auth
3. policy
4. normalize
5. validate
6. route plan
7. reserve
8. execute
9. evidence collect
10. settle
11. respond

这些阶段必须是显式的，而不能继续靠 controller 和 helper 隐式串联。

### 13.2 路由计划模型

在真正执行 provider 调用前，必须先生成 route plan。route plan 包含：

- 原始模型名
- 归一化模型名
- 候选渠道列表
- 候选 provider 列表
- fallback 顺序
- 强制映射规则
- 地域与代理策略
- 超时策略
- 流式策略

这样执行面才能在日志、审计与后续问题排查中自证“为什么选了这个 provider”。

### 13.3 流式输出统一层

所有流式协议最终必须收口到统一的 stream writer 生命周期。统一层负责：

- header 写入
- ping
- flush
- client disconnect 检测
- backpressure 感知
- 结束语义
- usage footer

provider adaptor 不允许直接自由操作底层 writer，最多通过统一接口提交 chunk。

### 13.4 Provider 适配器契约

建议定义统一接口：

- `NormalizeRequest`
- `ValidateRequest`
- `BuildProviderRequest`
- `ExecuteSync`
- `ExecuteStream`
- `SubmitAsync`
- `FetchAsyncStatus`
- `MapUsageEvidence`
- `DeriveFinalSettlementHint`

这样未来新 provider 的接入成本和审查成本会显著下降。

## 14. 安全架构设计

### 14.1 总体原则

安全不再作为零散补丁，而是平台基础层。v2 要求形成以下统一能力：

- 身份安全
- 会话安全
- 请求安全
- 外部抓取安全
- 回调安全
- 流式通道安全
- 管理操作安全

### 14.2 身份与会话

要求：

- Session Cookie 必须支持 secure / trusted origin 策略
- 高风险动作需要二次验证能力
- Passkey / 2FA / OAuth 绑定统一建模
- Device Authorization 明确授权范围和失效机制

### 14.3 SSRF 与 Protected Fetch

用户可控 URL 的获取必须统一走 protected fetch client。设计要求：

- URL 解析时校验
- redirect 时校验
- dial 前校验 host / port
- DNS 解析后校验实际 IP
- 私网与端口策略可配置
- fetch_setting 不得与 provider base URL 混用

建议把所有“用户可提交 URL 拉取”的路径统一接到 `security/fetch` 模块。

### 14.4 Webhook 安全

所有支付与外部回调：

- 必须有签名校验
- 必须有 idempotency key
- 必须与 order/workflow 关联
- 必须分业务成功与重复通知两类

### 14.5 管理端安全

管理员高风险操作必须具备：

- 审计日志
- 操作人标识
- 来源 IP
- 关键配置变更事件
- 可选审批流

### 14.6 数据脱敏

日志和审计系统必须内置数据分级与脱敏规则：

- API key
- provider response 中的大对象
- 音视频 base64
- 用户邮箱和手机号

## 15. 观测与审计架构设计

### 15.1 可观测性目标

CodeGo v2 必须让以下问题可以被快速回答：

- 某次请求为什么走了某 provider
- 某次扣费为什么是这个值
- 某次异步任务为什么退款了
- 某个 provider 哪段时间错误率升高
- 某个用户的消费与账本是否一致

### 15.2 指标

至少采集：

- 请求量
- 流式连接数
- provider 错误率
- provider P95/P99 延迟
- reservation / settlement 数量
- refund 数量
- workflow 重试次数
- webhook 重复通知次数
- 配额饱和和异常事件

### 15.3 Trace

所有核心链路共享：

- `trace_id`
- `request_id`
- `workflow_id`
- `reservation_id`
- `settlement_id`

这样才能贯通：

- 控制面
- 网关面
- 工作流
- 账本
- 审计

### 15.4 审计日志

审计日志必须区别于普通业务日志。建议单独模型：

- actor
- action
- target
- before
- after
- correlation_id
- recorded_at

## 16. 前端架构设计

### 16.1 总体判断

前端无需重写，不建议换框架。React + TanStack Router/Query + feature-based 目录结构可以继续使用。

### 16.2 长期前端形态

建议逻辑上分成三个 shell：

- Public Site
- User Console
- Admin Console

即使仍在同一个前端工程内，也要建立明确边界：

- 公共站点不依赖后台逻辑
- 用户控制台不依赖管理员组件
- 管理后台不混入普通用户上下文

### 16.3 设计系统

建立统一设计系统层：

- tokens
- layout primitives
- data table system
- form system
- feedback system
- auth gate

### 16.4 前端与后端契约

后端的 bounded context 应映射到前端 feature boundaries。前端不应该继续围绕“页面”组织 API，而应围绕：

- identity
- billing
- subscriptions
- tasks
- channels
- audit

## 17. API 架构设计

### 17.1 外部 API 分类

建议分为：

- `public api`
- `user api`
- `admin api`
- `gateway api`
- `desktop api`
- `internal ops api`

### 17.2 内部 API 原则

内部 API 不再共享“任意 controller 调任意 service”的方式，而应按 bounded context 暴露清晰应用服务接口。

### 17.3 兼容层

OpenAI 兼容接口仍保留，但应被定义为协议适配层，而不是全系统主模型。

## 18. Provider 与模型扩展设计

### 18.1 Provider Registry

建议引入 provider registry，记录：

- provider 名称
- 支持协议
- 支持模式
- 支持模型族
- 是否支持 stream
- 是否支持 async task
- usage 证据可靠度

### 18.2 Model Catalog

模型目录应独立于 provider：

- canonical model
- alias
- provider availability
- pricing policy binding
- feature flags

### 18.3 Capability Matrix

每个 provider / model 组合都应该有显式 capability：

- stream
- tool_call
- image
- audio
- async_video
- usage_provider_trusted
- supports_reasoning

## 19. 配置治理设计

### 19.1 配置分层

建议把配置分成：

- system config
- routing config
- pricing config
- billing policy
- security policy
- provider channel config

### 19.2 配置变更原则

- 高风险配置必须审计
- 支持 dry-run 校验
- 支持版本化
- 支持回滚

## 20. 迁移设计

### 20.1 迁移原则

- 不停机优先
- 双写或旁路验证优先
- 先引入新模型，不马上删除旧模型
- 先跑 shadow workflow，再切主路径

### 20.2 Phase 1：核心内核抽象

目标：

- 抽出 billing ledger 模型
- 抽出 workflow 抽象
- 抽出 provider contract
- 抽出 protected fetch
- 抽出统一 stream writer

### 20.3 Phase 2：运行时拆分

目标：

- 独立 `gateway-api`
- 独立 `workflow-worker`
- 控制面继续在 `control-api`
- 账本以 worker 形式运行

### 20.4 Phase 3：读模型和审计完善

目标：

- outbox 全面启用
- 日志和报表转 read model
- 建立审计与对账流程

### 20.5 兼容旧数据

旧的任务、余额、订阅使用量记录必须通过迁移脚本补齐到：

- 账户表
- 账本初始化 entry
- workflow 表
- execution 表

## 21. 非功能性目标

### 21.1 性能

- gateway p95 保持在合理范围
- stream 首包时间可观测
- workflow 可承载高任务量

### 21.2 可用性

- provider 故障不影响控制面
- 后台发布不影响在线请求
- 工作流失败可恢复

### 21.3 可维护性

- 模块可独立评审
- 目录与边界一致
- 接口契约稳定

### 21.4 可审计性

- 所有高价值业务事件都有 correlation id
- 所有额度变化可重建
- 所有任务终态可溯源

## 22. 组织与工程治理建议

### 22.1 代码仓治理

推荐仍使用单仓，但必须配套：

- context 目录边界
- lint 规则
- 架构依赖检查
- ADR 文档

### 22.2 变更准入

涉及以下模块的变更必须带设计说明或 ADR：

- billing
- workflow
- provider contract
- security
- audit

### 22.3 测试策略

- provider adaptor contract tests
- ledger invariant tests
- workflow integration tests
- idempotency tests
- stream disconnect tests

## 23. 风险与权衡

### 23.1 为什么不是全量微服务

因为全量微服务在当前阶段会过早引入：

- 分布式事务复杂度
- 跨服务调试成本
- 团队协作成本
- 过早基础设施负担

而这些都直接压在 CodeGo 最敏感的计费和任务链路上。

### 23.2 为什么不是继续大单体

因为继续单进程堆功能，只会让：

- 核心一致性问题更难收口
- 发布与扩容耦合
- 高并发请求和后台管理互相影响
- 安全和审计边界继续模糊

### 23.3 为什么选择有限服务化 + 模块化单体

这是对当前业务复杂度、团队规模、演进成本和长期稳定性的平衡点。

## 24. 最终结论

CodeGo v2 的最佳长期架构不是传统大单体，也不是一开始就全量微服务，而是：

**以 DDD 为边界、以账本为一致性核心、以持久化工作流为副作用编排核心、以控制面/执行面分离为运行模型、以单仓多二进制为落地形式的模块化平台架构。**

这套架构满足以下要求：

- 能长期使用
- 能逐步落地
- 能承载复杂计费
- 能控制异步任务复杂度
- 能继续扩展 provider 和产品域
- 能为未来进一步拆分保留空间

CodeGo v2 的长期目标形态应当固定为：

- 单仓
- 四核心进程
- PostgreSQL + Redis + Temporal
- 账本式额度系统
- 工作流式异步任务系统
- 统一 provider 契约
- 统一流式生命周期
- 统一安全抓取出口
- 读模型解耦

如果该设计书被采纳，后续应继续补充以下配套文档：

- 模块依赖图
- 数据库 ER 图
- 账本模型字段字典
- 工作流定义清单
- provider adaptor contract 规范
- API 分层规范
- 迁移执行 runbook

以上文档可作为 CodeGo v2 立项、评审、拆解和实施阶段的架构基线。
