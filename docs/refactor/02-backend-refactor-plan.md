# 后端重构设计与实施计划

## 目标

保留现有 API、协议和账务数据，降低网关与扣费耦合，统一异步任务，删除有证据的冗余代码。最终调整为“模板化原子服务 + 编排层”的微服务架构；迁移期间保留兼容运行模式，避免一次性拆分导致业务中断。

原子服务必须满足三个条件：拥有单一业务职责、拥有自己的数据访问边界、可以通过稳定 API 或事件被其他服务调用。原子服务不是按文件夹拆分，也不是每个小功能单独部署。

## 微服务拓扑

```text
                    ┌──────────────┐
Client ────────────▶│ API Gateway  │
                    └──────┬───────┘
                           │
                    ┌──────▼───────┐
                    │ Request      │
                    │ Orchestrator │
                    └──┬────┬──────┘
                       │    │
          ┌────────────┘    └──────────────┐
          ▼                               ▼
   ┌──────────────┐                 ┌──────────────┐
   │ Route        │                 │ Billing      │
   │ Service      │                 │ Service      │
   └──────┬───────┘                 └──────┬───────┘
          ▼                               ▼
   ┌──────────────┐                 ┌──────────────┐
   │ Upstream     │                 │ Ledger       │
   │ Adapter      │                 │ Service      │
   └──────────────┘                 └──────────────┘

   Control services: Identity / Marketplace / Commerce / Notification / Audit
   Async services: Worker / Reconciliation / Analytics
```

## 原子服务清单

| 服务 | 原子职责 | 同步接口 | 事件 |
| --- | --- | --- | --- |
| `api-gateway` | 鉴权、协议入口、流式连接、限流 | `ForwardRequest` | `request.accepted`, `request.failed` |
| `request-orchestrator` | 编排一次模型请求，不拥有余额和渠道数据 | `ExecuteRequest` | `request.completed` |
| `route-service` | 模型解析、渠道选择、路由池、熔断 | `ResolveRoute`、`ReleaseRoute` | `route.health.changed` |
| `upstream-service` | 上游协议适配、重试、超时、响应转换 | `Execute`、`Stream` | `upstream.failed` |
| `billing-service` | 预检查、预占、增加预占、结算、退款 | `Preflight`、`Reserve`、`Settle`、`Refund` | `billing.reserved`, `billing.settled`, `billing.refunded` |
| `ledger-service` | 不可变账本和对账，不直接参与请求路由 | `AppendEntry`、`Reconcile` | `ledger.appended`, `ledger.mismatch` |
| `identity-service` | 用户、权限、API Key、身份校验 | `Authenticate`、`Authorize` | `user.changed` |
| `marketplace-service` | 渠道、分组、倍率、砍价 | `ResolveGroup`、`SubmitBargain`、`ResolveBargain` | `bargain.requested`, `bargain.resolved` |
| `commerce-service` | 充值、订阅、盲盒、奖励 | `CreateOrder`、`GrantReward` | `order.paid`, `reward.granted` |
| `notification-service` | 通知收件箱、已读、投递、去重 | `ListNotifications`、`MarkRead` | `notification.created` |
| `audit-service` | 请求审计和运营审计 | `RecordAudit` | `audit.recorded` |
| `worker-service` | outbox、补偿、定时任务、统计 | 内部任务 API | 消费所有异步事件 |

`billing-service` 和 `ledger-service` 可以在第一阶段部署为一个进程、两个清晰模块；账务稳定后再拆成独立进程。`api-gateway`、`request-orchestrator` 和 `upstream-service` 也可先作为同一网关进程中的三个模块。

## 原子服务化调整

目标不是继续保留四个大服务，而是拆成多个可独立部署、独立扩容、独立演进的原子服务。服务之间通过稳定 API 和事件关联，禁止共享内部表和内部对象。

### 原子服务分组

```text
接入层
├── edge-gateway
├── auth-service
├── rate-limit-service
└── request-admission-service

请求执行层
├── request-orchestrator
├── model-registry-service
├── route-service
├── channel-health-service
├── upstream-http-service
├── upstream-stream-service
└── protocol-adapter-service

账务层
├── billing-preflight-service
├── reservation-service
├── settlement-service
├── ledger-service
├── reconciliation-service
└── pricing-service

业务控制层
├── identity-service
├── api-key-service
├── marketplace-service
├── bargain-service
├── subscription-service
├── payment-service
├── reward-service
└── notification-service

基础和异步层
├── config-service
├── feature-flag-service
├── audit-service
├── event-service
├── task-service
└── analytics-service
```

### 必须合并的原子服务

原子不等于越小越好。以下服务在第一版必须合并，避免产生高频网络跳转：

- `billing-preflight-service`、`reservation-service`：预检查和预占必须在同一账务边界内原子完成。
- `settlement-service`、`ledger-service`：结算和账本写入必须共享事务或同一可靠写入协议。
- `upstream-http-service`、`upstream-stream-service`：先共享连接池和供应商状态，后续有独立扩容证据再拆进程。
- `marketplace-service`、`bargain-service`：砍价是 marketplace 的一个聚合能力，不应跨多个远程服务完成一次状态变更。
- `payment-service`、`subscription-service`：支付成功和订阅开通使用同一订单状态机，异步事件只负责后续发放。

### 请求同步链路

同步链路最多保留以下调用：

```text
edge-gateway
  -> auth-service
  -> request-admission-service
  -> route-service
  -> billing-service
  -> upstream-service
  -> billing-service
```

如果一次普通请求需要同步访问超过 6 个远程服务，必须改为本地缓存、批量接口或事件处理，否则会产生串联延迟和级联故障。

### 事件关联链路

以下动作只能通过事件关联，不进入模型请求同步路径：

```text
request.completed  -> audit-service / analytics-service
billing.settled    -> analytics-service / notification-service
bargain.requested  -> notification-service
bargain.resolved   -> notification-service
payment.completed  -> subscription-service / reward-service
config.changed     -> route-service / channel-health-service
```

所有事件必须包含 event_id、aggregate_id、occurred_at、producer、schema_version 和 idempotency_key。消费者必须支持重复投递和重放。

### 独立数据边界

每个原子服务拥有自己的 schema 或数据库账号：

```text
identity_db       用户、身份、API Key
gateway_db        请求摘要和路由运行状态
marketplace_db    渠道、分组、砍价
billing_db        账户、预占、结算、账本
commerce_db       支付、订阅、奖励
notification_db   通知和已读状态
analytics_db      聚合指标和报表
```

服务之间只能通过 API 或事件读取数据。报表和搜索需要的数据通过事件投影生成，不能跨库 join 线上事务表。

### 原子服务模板

```text
service/
├── cmd/server
├── internal/domain
├── internal/application
├── internal/repository
├── internal/transport
├── internal/events
├── internal/infrastructure
├── migrations
├── api/openapi.yaml
├── service.yaml
└── Dockerfile
```

每个服务必须具备健康检查、超时、请求 ID、幂等、错误码、指标、结构化日志、优雅退出和独立迁移。模板不强制创建未使用的 gRPC、缓存或消息消费者。

### 服务拆分判断标准

只有满足以下至少两项，才拆成独立部署单元：

1. 有独立扩容需求。
2. 有独立故障隔离需求。
3. 有独立数据所有权。
4. 有稳定 API 或事件契约。
5. 能在不共享事务表的情况下运行。

否则保留为同一服务内的模块，避免“微服务数量增加但耦合不变”。

### 高并发设计对应关系

根据当前日志中 502/503/504 集中和每分钟配置同步现象，优先独立扩容：

- `edge-gateway`：无状态，按连接数和带宽扩容。
- `route-service`：本地缓存加 Redis 共享健康状态，按路由计算量扩容。
- `upstream-service`：按供应商、协议和连接池隔离扩容。
- `channel-health-service`：吸收故障折叠、冷却和半开探测。
- `billing-service`：按账户分片和写入吞吐扩容，保持强一致。
- `notification-service`、`analytics-service`：完全异步，按队列积压扩容。

配置同步从“每个网关每分钟主动拉取”调整为 `config.changed` 事件广播，保留低频全量校验作为兜底。

## 服务模板

每个原子服务使用同一套最小模板：

```text
service-name/
├── cmd/server/main.go
├── internal/
│   ├── domain/          # 领域对象和状态机
│   ├── application/     # 用例和服务接口
│   ├── repository/      # 本服务数据访问
│   ├── transport/http/  # HTTP/JSON API
│   ├── transport/grpc/  # 需要低延迟时启用
│   ├── events/          # outbox、消费者、幂等
│   └── infrastructure/  # 数据库、缓存、日志、配置
├── migrations/
├── api/openapi.yaml
├── Dockerfile
├── config.example.yaml
└── service.yaml
```

模板强制包含：健康检查、就绪检查、结构化日志、请求 ID、超时、幂等键、错误码、指标、迁移版本和 graceful shutdown。没有业务需要的 transport 或存储实现不创建，避免模板反向制造冗余。

## 请求编排

一次模型请求只由 `request-orchestrator` 编排：

```text
1. identity.Authenticate
2. route.ResolveRoute
3. billing.Preflight
4. billing.Reserve
5. upstream.Execute/Stream
6. billing.Settle 或 billing.Refund
7. audit.RecordAudit
```

编排器不读任何服务数据库。每一步只使用 API 返回值，使用 request_id 和 reservation_id 贯穿全链路。

跨服务事务不使用分布式 2PC，采用：

- billing reservation 状态机
- outbox 事件
- 幂等消费者
- 超时补偿
- 对账任务

## 服务发现和部署

第一阶段使用静态服务配置或 Docker Compose 服务名；服务数量稳定后再接入注册中心。请求路径只经过 API Gateway 和 Request Orchestrator，不允许前端直接调用内部原子服务。

服务间通信约定：

- 外部和控制面：HTTP/JSON + OpenAPI。
- 网关内部低延迟调用：gRPC 或同进程接口。
- 状态变化和异步动作：Outbox + 消息队列。
- 每个服务只能访问自己的表或数据库 schema。

## 不拆分的内容

以下内容不单独拆成服务：

- 每一种模型协议一个服务。
- 每一种支付方式一个服务。
- 每一张表一个服务。
- 每一个后台页面一个服务。
- 一次性数据修复脚本。

这些内容属于同一原子服务内部的适配器、repository 或 worker task。

## 可被替代或删除的服务

微服务重构不要求保留所有现有服务。以下能力优先按“删除中间层、复用基础设施”的原则处理。

### `request-admission-service` 可被 API Gateway 替代

鉴权后的基础限流、请求体大小限制、并发连接限制和黑名单判断属于 edge gateway 能力。只有涉及账户余额、模型配额或复杂策略时才调用后端服务。

替代结果：

```text
edge-gateway
├── 基础限流
├── 连接保护
├── 请求体限制
└── 请求 admission
```

避免每个请求多一次远程调用。

### `rate-limit-service` 不单独部署

普通 IP、API Key、用户和渠道限流使用网关本地令牌桶加 Redis 共享计数即可。只有需要复杂配额策略时，由 billing 或 identity 返回额度策略。

### `model-registry-service` 可被配置投影替代

模型列表和协议能力变化频率低，不需要独立在线服务。由 `config-service` 发布版本化配置，gateway 和 route-service 保留本地只读快照。

### `protocol-adapter-service` 不单独部署

OpenAI、Claude、Gemini 协议转换与上游连接强相关，拆开会增加序列化和流式转发延迟。保留为 `upstream-service` 内部适配器。

### `channel-health-service` 可被 route-service 内部模块替代

健康状态、故障域、冷却、半开探测都直接服务于路由决策。除非健康探测需要独立的大规模调度，否则不独立部署，作为 route-service 的状态模块运行。

### `request-orchestrator` 不一定是独立网络服务

编排逻辑位于请求热路径，第一版作为 gateway 内部 application module，使用清晰接口隔离。只有当请求编排需要独立扩容或多种客户端复用时才拆成网络服务。

### `pricing-service` 可被版本化价格快照替代

模型价格、渠道倍率和套餐规则在请求期间只需要一致快照。由 billing 发布价格版本，gateway 和 billing 使用本地/Redis 快照；不为每次请求单独访问 pricing-service。

### `event-service` 不作为业务服务保留

事件服务容易变成没有业务边界的“万能中转站”。直接使用 Kafka、NATS 或 Redis Streams 提供传输，业务事件由各服务的 outbox 产生，避免再包一层自定义转发服务。

### `task-service` 可被工作流运行时替代

短任务使用消息队列消费者；需要长时间重试、人工介入和补偿的流程使用 Temporal 等工作流引擎。不要再维护一个功能不完整的自研通用任务服务。

### `analytics-service` 改为事件投影

统计和报表不需要参与在线事务。由事件消费者写入 ClickHouse、PostgreSQL 汇总表或现有分析存储，不单独维护复杂的同步业务服务。

### `audit-service` 分为同步最小审计和异步扩展审计

安全必须保留的审计摘要在业务事务中写入 outbox；详细请求耗时、上游状态和调试字段异步投影。避免 audit-service 阻塞模型请求。

## 重构后的最小服务集合

在删除可替代服务后，推荐的第一版实际部署单元为：

```text
1. edge-gateway
   鉴权接入、基础限流、协议适配、请求编排、流式响应

2. route-service
   模型能力快照、渠道选择、健康状态、故障域和路由池

3. upstream-service
   上游连接池、供应商适配、重试、超时、响应转换

4. billing-service
   预检查、预占、结算、退款、账本写入

5. identity-service
   用户、API Key、权限和身份数据

6. marketplace-service
   渠道、分组、倍率、砍价

7. commerce-service
   支付、订阅、奖励和盲盒

8. notification-service
   通知收件箱、已读、去重和投递

9. worker-runtime
   outbox 消费、异步投影、补偿和定时任务
```

其中 `edge-gateway`、`route-service` 和 `upstream-service` 可以在低流量环境合并部署，但代码边界必须保持独立；`billing-service` 必须始终拥有独立数据写入边界。

## 删除原则

以下情况直接删除服务或模块，而不是迁移成新服务：

1. 只转发请求，没有独立状态和业务规则。
2. 只为弥补旧架构边界存在，新的拥有者已经明确。
3. 只提供一次数据库查询，可以改成缓存或配置投影。
4. 只执行简单定时任务，可以改成 worker task。
5. 只有一个调用方且调用频率极高，拆分只增加网络跳转。

删除前必须完成调用方搜索、部署配置搜索、数据表归属确认和回归验证；删除的是服务边界和重复实现，不删除仍被生产数据依赖的历史迁移。

## 目标边界

```text
gateway: transport / normalize / routing / upstream / session
billing: application / domain / repository / worker
marketplace: channel / group / multiplier / bargain
commerce: subscription / payment / blind-box / reward
notification: event / inbox / read-state / delivery
platform: config / db / cache / security / observability
```

`platform` 只提供基础设施，不包含扣费、渠道、奖励等业务逻辑。

## 网关设计

所有模型请求统一经过：鉴权、规范化、模型解析、路由选择、账务预检查、额度预占、上游执行、用量采集、结算或退款、审计事件。

引入统一 `RequestSession` 保存 request_id、user_id、token_id、route_id、reservation_id、estimated_quota、actual_quota 和状态。协议适配器只做协议转换；重试、超时、熔断和错误映射集中在 upstream 层。

同一 request_id 内重试，不创建新的扣费记录。连接失败、408、429、500、502、503、504 可重试；鉴权失败、参数错误、余额不足、模型不存在不可重试。

## 扣费设计

Billing 对网关只暴露四个操作：`Reserve`、`IncreaseReservation`、`Settle`、`Refund`。禁止网关、commerce、marketplace 直接更新余额或 quota。

每次扣费必须有 request_id、幂等键、reservation_id、ledger entry 和可重放 outbox event。流式请求有 usage 时按实际用量结算，无 usage 时退款，无法确认时进入对账队列。

## 通知和砍价

砍价使用统一事件：`marketplace.bargain.requested`、`marketplace.bargain.approved`、`marketplace.bargain.rejected`。主事务只写砍价状态和 outbox，worker 写通知；通知失败不回滚砍价。

通知需要 recipient_user_id、event_type、resource_id、read_at 和 deduplication_key，支持渠道主收到申请通知、用户收到批准或拒绝通知。

## 数据与迁移

本轮不全量替换 GORM。先禁止 app 层直接使用 DB，将查询封装到 repository。迁移按 `legacy`、`core`、`gateway`、`billing`、`marketplace`、`commerce`、`notification` 分目录；历史迁移只修复旧数据，新业务不得继续写入旧迁移文件。

旧余额字段保留到新账本连续对账通过后再删除。

## 实施阶段

### Phase 0：盘点和基线

建立路由到 service、数据表读写归属、旧扣费入口和可删除代码清单。运行 `go list -deps ./...`、各核心模块测试和前端类型检查。

### Phase 1：建立边界

新增 gateway session、billing facade、notification facade。旧实现先被 facade 包装，行为不变；禁止新增跨模块直接写表。

### Phase 2：迁移网关

统一多协议请求生命周期、RequestSession、上游重试和流式处理。路由选择从协议实现中移出。

### Phase 3：迁移账务

所有网关扣费改走 facade；统一 reservation、settlement、refund 和 outbox；增加 reservation 超时回收及每日对账。

### Phase 4：统一通知

建立通知表和 API，接入砍价申请与处理事件，支持未读数、已读和去重。

### Phase 5：删除冗余

只有满足“无路由、无命令、无前端、无测试消费者，且已有替代实现”才删除。优先删除旧余额扣减函数、重复错误转换、重复迁移分支和确认完成的一次性修复命令。

### Phase 6：收拢 worker

将 ledger、workflow、marketplace 修复任务统一成 worker task。兼容命令保留一个版本，确认无部署引用后删除。

## 验收命令

```powershell
go test ./...
go vet ./...
go build ./cmd/gateway-api
go build ./cmd/control-api
go build ./cmd/worker
go test -race ./internal/billing/...
bun run typecheck
bun run build
```

## 必验场景

- 同一 request_id 重复结算不会重复扣费。
- 上游连接失败会退款。
- 流式中途失败按 usage 正确结算。
- 并发请求不会超扣。
- 砍价通知失败不影响主事务。
- 旧 API 响应字段保持兼容。

## 回滚

每个阶段保留旧执行器开关；数据库只做向前兼容迁移；新旧账务连续对账通过后才删除旧写路径。不得删除历史迁移 ID、余额旧字段和生产修复工具，除非已经完成数据迁移并有验证输出。
