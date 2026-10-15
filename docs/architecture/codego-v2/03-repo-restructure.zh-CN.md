# CodeGo v2 代码仓与目录重组方案

## 1. 文档目的

本文档用于将 CodeGo 当前代码仓从“增长型单体目录”重组为适合 v2 架构的“单仓多二进制 + internal 模块边界”结构。它不要求一次性完成所有物理迁移，但要求先定义最终目标目录和中间过渡策略。

## 2. 当前问题

当前项目目录具备典型的早期 Go 项目特征：

- `controller`
- `service`
- `model`
- `relay`
- `router`
- `common`

这种结构在项目早期足够高效，但在平台复杂度提升后会出现问题：

- 分层以技术类型而不是业务边界组织
- 同类文件容易跨业务域聚集
- controller/service/model 横向变大
- 很难回答“某个业务能力归谁负责”

因此 v2 需要从“技术层目录”向“业务模块目录”迁移。

## 3. 目标目录结构

建议最终形态：

```text
codego/
  cmd/
    control-api/
    gateway-api/
    workflow-worker/
    ledger-worker/
  internal/
    identity/
      app/
      domain/
      infra/
      transport/
    gateway/
      routing/
      execution/
      stream/
      contract/
    billing/
      app/
      domain/
      ledger/
      projection/
    workflow/
      app/
      domain/
      temporal/
      projection/
    commerce/
      app/
      domain/
      payment/
      subscription/
    audit/
      app/
      domain/
      projection/
    adminops/
      app/
      domain/
    platform/
      bootstrap/
      config/
      db/
      cache/
      eventing/
      httpx/
      security/
      observability/
  api/
    openapi/
    proto/
  web/
    default/
  docs/
```

## 4. 顶层目录职责

### 4.1 cmd

存放各独立可执行程序的入口。每个可执行程序只负责装配，不包含业务逻辑。

### 4.2 internal

存放所有后端业务与平台代码。任何核心实现不再散落在根目录技术分层中。

### 4.3 api

存放显式 API 契约定义。后续如引入 gRPC、internal API 或 schema-first 设计，该目录作为契约源。

### 4.4 web

保留前端工程。前端未来仍可单仓维护。

### 4.5 docs

存放架构、运行、产品、迁移和运维文档。

## 5. internal 层分法

### 5.1 app

应用服务层，负责：

- 编排用例
- 事务边界
- 模块对外接口

### 5.2 domain

领域层，负责：

- 核心实体
- 值对象
- 状态机
- 业务规则

### 5.3 infra

基础设施实现，负责：

- repository
- provider client
- database mapper
- cache adapter

### 5.4 transport

对外协议入口，负责：

- HTTP handlers
- request / response DTO
- middleware adapter

不是所有模块都必须叫 `transport`，但凡对外 API 比较明显的模块，建议采用这种分法。

## 6. 现有目录到目标目录的映射

### 6.1 当前 `controller`

迁移目标：

- 用户与认证相关 -> `internal/identity/transport/http`
- 订阅和支付相关 -> `internal/commerce/transport/http`
- 渠道路由与模型管理 -> `internal/gateway/routing/transport/http`
- 日志与报表 -> `internal/audit/transport/http`
- 任务查询与管理 -> `internal/workflow/transport/http`
- 管理端高风险运维操作 -> `internal/adminops/transport/http`

### 6.2 当前 `service`

迁移目标：

- 计费相关 -> `internal/billing/app`
- 任务轮询相关 -> `internal/workflow/app`
- 渠道探测与 provider 公共逻辑 -> `internal/gateway/execution/app`
- 用户与认证服务 -> `internal/identity/app`
- 商业规则 -> `internal/commerce/app`

### 6.3 当前 `model`

迁移目标：

- 用户模型 -> `internal/identity/domain` + `infra/repository`
- 任务模型 -> `internal/workflow/domain`
- 订阅/订单模型 -> `internal/commerce/domain`
- 渠道/模型目录 -> `internal/gateway/routing/domain`
- 日志与审计 -> `internal/audit/domain`
- 余额与账本 -> `internal/billing/domain`

### 6.4 当前 `relay`

迁移目标：

- provider adaptor -> `internal/gateway/execution/providers`
- route planning 逻辑 -> `internal/gateway/routing/app`
- stream helper -> `internal/gateway/stream`
- openai compatibility transport -> `internal/gateway/transport/http`

### 6.5 当前 `router`

迁移目标：

- 最终仅保留顶层装配和 route mounting
- 具体 route 定义分散到各模块 transport 层

### 6.6 当前 `common`

迁移目标：

按职责拆分：

- 配额计算 -> `internal/billing/domain`
- SSRF -> `internal/platform/security`
- 通用 JSON / util -> `internal/platform/*`
- 自定义 event/stream -> `internal/gateway/stream`

## 7. 四个二进制的装配职责

### 7.1 control-api

装配模块：

- identity
- commerce
- adminops
- audit 查询接口
- gateway-routing 的管理接口
- billing 的管理接口

### 7.2 gateway-api

装配模块：

- identity 鉴权接口
- gateway-routing
- provider-execution
- gateway-stream
- billing reservation / settlement client

### 7.3 workflow-worker

装配模块：

- workflow
- provider-execution 的 async client
- billing
- audit 事件发布

### 7.4 ledger-worker

装配模块：

- billing projection
- outbox dispatcher
- readmodel projector
- reconciliation jobs

## 8. 迁移中的中间结构

不建议直接一步把全部代码移位。建议中间过渡如下：

### 8.1 第一步

新增：

- `internal/platform`
- `internal/billing`
- `internal/workflow`
- `internal/gateway`

旧目录仍在，但新逻辑优先写入新目录。

### 8.2 第二步

旧 `service` 中高价值逻辑逐步搬迁，旧包只保留 shim 和兼容调用。

### 8.3 第三步

旧 `controller` 拆为各模块 `transport/http`。

### 8.4 第四步

删除旧横向目录中的业务主实现，只保留少量兼容桥接文件。

## 9. 依赖方向规范

必须遵守：

- `transport -> app -> domain`
- `app -> infra interface`
- `infra -> domain`
- `platform` 可被业务模块依赖
- 业务模块之间只能通过 app 接口或事件交互

不允许：

- `domain -> transport`
- `billing.domain -> provider-execution.infra`
- `workflow.domain -> controller`

## 10. 前端目录建议

前端建议保留现有 feature 化方向，但增强 shell 结构：

```text
web/default/src/
  shells/
    public/
    console/
    admin/
  features/
    identity/
    billing/
    subscriptions/
    tasks/
    channels/
    audit/
    site/
  components/
  lib/
  stores/
```

## 11. 工程工具建议

### 11.1 Go

- 引入架构依赖检查脚本
- 引入 package boundaries 检查
- 引入统一 error model

### 11.2 前端

- feature 级 API 客户端
- schema-first 表单
- table column meta 规范

### 11.3 CI

增加：

- 架构依赖检查
- contract test
- workflow test
- ledger invariant test

## 12. 开发规范建议

### 12.1 新功能进入规则

每新增一个功能，必须先回答：

1. 属于哪个模块
2. 是否需要新实体
3. 是否影响账本
4. 是否影响工作流
5. 是否需要新事件

### 12.2 不再允许的开发方式

- 直接往 `common` 塞新业务逻辑
- 直接在 provider adaptor 中写业务结算
- 任意 controller 直接调任意 model

## 13. 总结

这份目录重组方案的核心目的，是把 CodeGo 的代码组织从“随业务增长的技术层单体”迁移到“可长期维护的业务边界单仓平台”。它不要求一次性完成，但要求从现在开始所有新增核心逻辑都沿着目标结构落地。
