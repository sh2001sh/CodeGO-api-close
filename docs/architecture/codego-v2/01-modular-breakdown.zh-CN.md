# CodeGo v2 模块拆分清单

## 1. 文档目的

本文档用于将主架构设计书中的逻辑平面和 bounded context 进一步下沉为可实施的模块拆分清单。目标不是抽象描述“有哪些模块”，而是明确每个模块：

- 负责什么
- 不负责什么
- 输入与输出是什么
- 与哪些模块交互
- 哪些对象归它所有
- 哪些调用方式被允许
- 哪些跨界访问被禁止

本文档是后续代码重组、数据库 schema 划分、接口审查、团队协作和项目排期的第一层实施基线。

## 2. 模块拆分原则

### 2.1 先定所有权，再定调用方式

一个模块的本质不是代码目录，而是某一类业务真相由谁负责。CodeGo v2 的模块拆分优先回答“谁拥有事实源”，而不是“谁提供工具函数”。例如：

- 用户身份真相由 `identity` 持有
- 路由策略真相由 `gateway-routing` 持有
- 额度变更真相由 `billing-ledger` 持有
- 任务终态工作流真相由 `async-tasks` 持有

### 2.2 模块之间只能通过应用服务和事件交互

模块间禁止直接共享底层 model 结构体进行任意读写。允许的交互方式有两种：

- 同步调用应用服务接口
- 异步订阅事件

不允许：

- 跨模块直接访问数据库表
- 跨模块直接复用未抽象的内部 helper
- 在 provider adaptor 中越界修改账本或任务终态

### 2.3 模块规模以“可独立演进”为目标

每个模块既不能过大，导致内部职责混乱，也不能过碎，导致调用链过深。对 CodeGo 而言，最佳粒度是 bounded context 级，而不是把每个功能按钮都拆成一个微模块。

## 3. 模块总览

CodeGo v2 建议拆成八个核心业务模块和两个基础平台模块：

- `identity`
- `gateway-routing`
- `provider-execution`
- `billing-ledger`
- `async-tasks`
- `commerce`
- `audit-observability`
- `admin-ops`
- `platform-runtime`
- `platform-security`

其中前八个是业务或业务基础模块，后两个是平台基础模块。

## 4. identity 模块

### 4.1 模块目标

负责所有与“谁在访问系统”相关的事实与策略，是控制面的基础模块。

### 4.2 职责

- 用户注册、登录、登出
- Session 与设备授权
- OAuth / 小程序 / 桌面端认证绑定
- 2FA / Passkey / Backup Codes
- 角色与权限
- 用户基础状态

### 4.3 不负责

- 钱包余额
- 订阅额度
- provider 路由
- 异步任务状态

### 4.4 核心实体

- `User`
- `UserCredential`
- `Session`
- `PasskeyCredential`
- `OAuthBinding`
- `DeviceAuthorization`
- `RoleAssignment`

### 4.5 对外提供的应用服务

- `RegisterUser`
- `AuthenticatePassword`
- `StartOAuthLogin`
- `BindOAuthAccount`
- `CreateSession`
- `RevokeSession`
- `StartPasskeyRegistration`
- `VerifySecondFactor`
- `ListAuthorizedDevices`

### 4.6 输入

- 用户注册请求
- 登录请求
- OAuth 回调
- 安全验证请求
- 设备授权审批

### 4.7 输出

- 用户身份对象
- 会话信息
- 授权结果
- 审计事件

### 4.8 与其它模块的交互

- 向 `audit-observability` 发出身份事件
- 向 `commerce` 提供用户识别信息
- 向 `gateway-routing` 提供用户组与策略标签

### 4.9 实施注意事项

- 当前项目中与用户、OAuth、Passkey、桌面授权相关的 controller 与 service 需要整合到该模块
- 用户设置中与计费偏好相关字段应逐步迁移为由 `commerce` 或 `billing-ledger` 提供解释，但用户本人资料仍归 `identity`

## 5. gateway-routing 模块

### 5.1 模块目标

负责“请求应该如何被路由”，而不负责“请求如何执行”。它是网关执行前的策略决策层。

### 5.2 职责

- 模型名标准化
- provider 与 channel 能力匹配
- 路由策略计算
- fallback 计划生成
- 区域与代理选择
- 特定租户、分组、用户的路由约束

### 5.3 不负责

- 真正发起 HTTP 请求
- 解析 provider 响应
- 扣费
- 写流

### 5.4 核心实体

- `ModelCatalogEntry`
- `ProviderChannel`
- `RoutingPolicy`
- `RouteConstraint`
- `RoutePlan`
- `FallbackPlan`

### 5.5 对外应用服务

- `ResolveModel`
- `BuildRoutePlan`
- `ChooseFallbackCandidate`
- `EvaluateChannelAvailability`
- `PreviewRouteDecision`

### 5.6 输入

- 用户请求中的模型名
- 用户组、套餐、权限
- 渠道元数据
- 管理员配置的策略规则

### 5.7 输出

- 归一化模型信息
- 路由计划
- fallback 顺序
- 路由解释

### 5.8 与其它模块的交互

- 从 `identity` 获取用户标签和角色信息
- 从 `commerce` 获取套餐限制和权益规则
- 为 `provider-execution` 提供 route plan
- 为 `audit-observability` 输出 route decision 事件

### 5.9 实施注意事项

- 当前 `relay` 中的模型映射、渠道锁定、fallback 策略、部分 channel 设置逻辑都需要从“执行代码”里抽出到该模块
- 该模块必须可单测，不能隐式依赖 gin context

## 6. provider-execution 模块

### 6.1 模块目标

负责对接所有 provider，并将 provider 世界和 CodeGo 世界隔离开。

### 6.2 职责

- 请求映射
- provider 协议适配
- 同步、流式、异步提交执行
- provider 结果解析
- usage 证据归一化
- provider 能力声明

### 6.3 不负责

- 最终计费规则
- 钱包/订阅/令牌修改
- 任务终态判定之外的工作流推进
- 审计视图组装

### 6.4 核心实体

- `ExecutionRequest`
- `ExecutionAttempt`
- `ProviderCapability`
- `ProviderResponse`
- `StreamChunkEvidence`
- `UsageEvidence`

### 6.5 对外应用服务

- `ExecuteSync`
- `ExecuteStream`
- `SubmitAsync`
- `FetchAsyncStatus`
- `NormalizeUsageEvidence`

### 6.6 输入

- route plan
- 标准化后的请求对象
- 执行策略
- provider channel 凭证

### 6.7 输出

- 标准化响应
- 标准化流块
- 异步任务提交结果
- 任务状态查询结果
- usage 证据

### 6.8 与其它模块的交互

- 接收 `gateway-routing` 的 route plan
- 将 usage 证据交给 `billing-ledger`
- 将异步任务结果交给 `async-tasks`
- 将执行尝试结果交给 `audit-observability`

### 6.9 Provider Adapter Contract

建议每个 adapter 实现以下接口：

- `NormalizeRequest`
- `ValidateProviderSpecificFields`
- `BuildSyncRequest`
- `BuildStreamRequest`
- `BuildAsyncSubmitRequest`
- `ParseSyncResponse`
- `ParseStreamChunk`
- `ParseAsyncSubmitResponse`
- `ParseAsyncStatusResponse`
- `ExtractUsageEvidence`
- `DeriveSettlementHint`

### 6.10 实施注意事项

- 当前 `relay/channel/*` 中的 provider 代码需要继续保留，但要逐步重整为统一 contract
- 所有直接写 `c.Writer` 的流式逻辑最终必须迁出 adapter 直接控制

## 7. billing-ledger 模块

### 7.1 模块目标

提供平台唯一可信的额度与资金变化事实层。

### 7.2 职责

- 创建账户
- 创建 reservation
- 执行 settlement
- 执行 refund
- 执行 adjustment
- 生成 ledger entry
- 聚合 balance snapshot
- 执行账本幂等校验

### 7.3 不负责

- 支付接口本身
- provider 请求执行
- UI 套餐展示

### 7.4 核心实体

- `BillingAccount`
- `LedgerEntry`
- `Reservation`
- `Settlement`
- `BalanceSnapshot`
- `AdjustmentRequest`

### 7.5 对外应用服务

- `CreateReservation`
- `SettleReservation`
- `ReleaseReservation`
- `RefundByReference`
- `AdjustAccount`
- `GetAccountBalance`
- `RebuildBalanceSnapshot`

### 7.6 输入

- request execution 结果
- workflow 终态事件
- 订单权益发放结果
- 管理员调账命令

### 7.7 输出

- 账本流水
- 账户余额快照
- 结算结果
- 审计与读模型事件

### 7.8 与其它模块的交互

- 被 `gateway-api` 在在线请求中调用 reservation
- 被 `workflow-plane` 在异步终态中调用 settlement/refund
- 接收 `commerce` 的权益 grant
- 向 `audit-observability` 发出账本事件

### 7.9 模块约束

- 任何业务模块不得直接更新余额字段
- 任何额度变化必须有可追溯 reference id
- settlement 和 refund 必须支持幂等键

## 8. async-tasks 模块

### 8.1 模块目标

统一管理所有异步任务生命周期。

### 8.2 职责

- 创建任务工作流
- 记录 upstream task id
- 管理轮询计划
- 管理 timeout / retry
- 终态转换
- 驱动 settlement 或 refund
- 物化任务结果

### 8.3 不负责

- 实际 provider 协议转换
- 直接改余额
- 用户认证

### 8.4 核心实体

- `TaskWorkflow`
- `TaskExecution`
- `TaskSnapshot`
- `TaskTerminalResult`
- `TaskResultProjection`

### 8.5 对外应用服务

- `CreateTaskWorkflow`
- `RecordAsyncSubmission`
- `TransitionTaskState`
- `HandleTaskTimeout`
- `FinalizeTask`
- `ProjectTaskResult`

### 8.6 输入

- async submit 结果
- provider status poll 结果
- timeout signal
- user fetch refresh request

### 8.7 输出

- 工作流状态变更
- 终态事件
- settlement/refund 触发命令
- 用户可见任务结果

### 8.8 与其它模块的交互

- 从 `provider-execution` 获取异步状态
- 调用 `billing-ledger` 完成结算或退款
- 向 `audit-observability` 发出状态变更事件

### 8.9 实施注意事项

- 当前 `service/task_polling.go`、`relay/relay_task.go`、部分 `controller/task_*` 中的职责将被重新分配到该模块
- 任务状态机与结算必须统一在 workflow 中表达

## 9. commerce 模块

### 9.1 模块目标

负责一切“卖什么、怎么买、买到了什么权益”的业务规则。

### 9.2 职责

- 套餐与订阅计划
- 钱包充值产品
- 升级与续费规则
- 订单创建
- 支付回调推进
- 权益发放
- 重置机会、兑换、转换等订阅衍生规则

### 9.3 不负责

- 真正修改账本
- provider 执行
- 流式请求

### 9.4 核心实体

- `Product`
- `SubscriptionPlan`
- `Order`
- `PaymentAttempt`
- `BenefitGrant`
- `PricingRule`

### 9.5 对外应用服务

- `CreateOrder`
- `MarkOrderPaid`
- `GrantBenefits`
- `UpgradeSubscription`
- `RenewSubscription`
- `UseResetOpportunity`

### 9.6 与其它模块的交互

- 调用 `billing-ledger` 发放权益
- 从 `identity` 获取用户信息
- 向 `audit-observability` 发出商业事件

## 10. audit-observability 模块

### 10.1 模块目标

为平台提供审计、可观察性和运营查询事实层。

### 10.2 职责

- 审计日志
- 使用日志
- 管理员行为记录
- 指标采集
- trace 关联
- 读模型投影

### 10.3 不负责

- 主业务事务推进
- 实时 provider 请求执行
- 资金结算

### 10.4 核心实体

- `AuditEvent`
- `UsageLog`
- `AdminActionLog`
- `MetricRecord`
- `TraceLink`

### 10.5 对外应用服务

- `RecordAuditEvent`
- `AppendUsageEvidence`
- `BuildReadProjection`
- `SearchAuditLog`
- `AggregateUsageTrend`

## 11. admin-ops 模块

### 11.1 模块目标

统一承接后台高风险管理操作和运维命令。

### 11.2 职责

- 批量启停渠道
- 上游模型同步
- 安全开关切换
- 缓存清理
- 强制重试与补偿
- 手工调账指令入口

### 11.3 不负责

- 实际 provider 调用细节
- 直接操作账本底层
- 用户身份真相

### 11.4 核心实体

- `AdminCommand`
- `MaintenanceJob`
- `ConfigChange`
- `RecoveryAction`

## 12. platform-runtime 模块

### 12.1 模块目标

承载跨业务模块共享的运行时基础设施，而不是业务规则。

### 12.2 职责

- 启动装配
- 配置加载
- 依赖注入
- HTTP server bootstrap
- worker bootstrap
- scheduling shell
- tracing bootstrap

### 12.3 不负责

- 任何业务决策
- 任何模型或账本规则

## 13. platform-security 模块

### 13.1 模块目标

承载跨模块共享的安全基础设施能力。

### 13.2 职责

- protected fetch client
- redirect validation
- webhook verification helpers
- secret handling
- stream disconnect policy
- high-risk action verification

### 13.3 不负责

- 用户注册
- 订单业务
- provider 路由

## 14. 模块间同步调用矩阵

允许的高频同步调用：

- `gateway-api -> identity`
- `gateway-api -> gateway-routing`
- `gateway-api -> provider-execution`
- `gateway-api -> billing-ledger`
- `workflow-worker -> provider-execution`
- `workflow-worker -> async-tasks`
- `workflow-worker -> billing-ledger`
- `control-api -> identity`
- `control-api -> commerce`
- `control-api -> admin-ops`

不建议的同步调用：

- `provider-execution -> billing-ledger`
- `provider-execution -> commerce`
- `audit-observability -> billing-ledger`
- `identity -> provider-execution`

## 15. 模块优先级排序

实施优先级建议：

1. `billing-ledger`
2. `async-tasks`
3. `provider-execution`
4. `gateway-routing`
5. `platform-security`
6. `commerce`
7. `audit-observability`
8. `admin-ops`
9. `identity`
10. `platform-runtime`

排序原因：

- 当前最昂贵的问题集中在计费、任务与 provider 边界
- 身份模块虽重要，但现有系统相对可用，不是首个一致性瓶颈

## 16. 模块拆分实施验收

当某模块被认为“拆分完成”时，至少应满足：

- 有独立目录和 owner
- 有显式应用服务接口
- 有自己的数据库 schema 或表前缀
- 不再依赖跨模块直接读写数据库
- 关键路径具备模块级测试
- 有 ADR 或模块说明文档

## 17. 总结

本清单的核心意义在于给 CodeGo v2 一个长期稳定的责任分工体系。未来任何新功能都必须先归属模块，再进入开发，而不是继续在大目录中横向扩展。只有这样，后续的目录重构、数据库设计、服务拆分和团队协作才有统一依据。
