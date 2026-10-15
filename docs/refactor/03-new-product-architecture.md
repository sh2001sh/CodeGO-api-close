# 新产品最终架构设计

## 1. 产品定位

新产品是一个面向多模型、多供应商的 AI API 平台，提供：

- OpenAI、Anthropic、Gemini 等协议兼容接入。
- 多供应商路由、故障切换和连接池管理。
- 用户、租户、API Key 和权限管理。
- 额度预占、按量计费、结算、退款和账本。
- 套餐、充值、订阅、奖励和渠道市场。
- 砍价、倍率覆盖、通知、审计和运营分析。

旧项目只作为功能和边界参考。新产品不复制旧目录，不继承旧的调用关系和数据库结构。

## 2. 总体原则

1. 逻辑能力原子化，热路径服务数量最少化。
2. 每个服务拥有自己的数据边界，不共享内部表。
3. 请求热路径只做鉴权、路由、预占、上游调用和结算。
4. 通知、审计扩展、统计、报表和补偿通过事件异步处理。
5. 账务以预占、结算、退款和不可变账本为核心。
6. 所有写操作具备幂等键，所有事件支持重复消费和重放。
7. 服务只有在需要独立扩容、故障隔离或数据所有权时才独立部署。
8. 所有性能目标用 P95、P99 和真实突发流量验证，不使用平均值掩盖尾延迟。

## 3. 部署拓扑

```text
                    Client
                      |
                Load Balancer
                      |
                edge-gateway
                      |
                request-runtime
             /         |          \
        identity   route-runtime   billing
                                  /      \
                             reserve   settle
                      |
                upstream-runtime
                      |
                 Provider APIs

       Event Bus -------------------------------
          |          |          |        |       |
       worker     audit     notify   analytics  reconcile
```

第一阶段的实际部署单元为：

```text
edge-gateway
request-runtime
billing-service
upstream-runtime
identity-service
marketplace-service
commerce-service
worker-runtime
```

逻辑原子模块可以多于部署单元。后续只有压测证明需要独立扩容时才拆进程。

## 4. 原子能力目录

### 接入与请求

```text
edge-gateway
request-runtime
protocol-adapter
admission-control
```

`edge-gateway` 负责网络接入、基础限流、连接管理和流式返回。`request-runtime` 负责身份、模型、路由、重试和请求状态编排。

### 路由与上游

```text
model-catalog
route-selector
health-circuit
provider-registry
upstream-connector
stream-controller
```

模型、价格、渠道、健康状态使用 Redis 和进程内快照，避免请求每次查询数据库。

### 账务

```text
billing
reservation
settlement
refund
ledger
reconciliation
```

这些能力逻辑分离，但预占、结算和账本写入必须处在明确的一致性边界内。第一版统一部署为 `billing-service`。

### 用户和商业

```text
identity
api-key
marketplace
bargain
payment
subscription
benefit
notification
```

### 异步与数据

```text
event-bus
outbox
worker-runtime
audit-projector
analytics-projector
reconciliation-worker
```

不自研通用 `event-service`。事件传输直接采用 Kafka、NATS JetStream 或 Redis Streams；短任务由 worker 消费，长流程再引入 Temporal。

## 5. 请求热路径

```text
1. edge-gateway 解析协议、API Key、请求大小和基础限流。
2. request-runtime 从本地快照完成身份、模型和路由决策。
3. billing-service 一次调用完成额度预检查和 reservation。
4. upstream-runtime 建立连接、执行重试、处理流式响应。
5. billing-service 一次调用完成 settlement 或 refund。
6. 写入最小 outbox 事件后立即返回。
```

热路径最多 3 个核心远程阶段：预占、上游执行、结算。模型目录、价格、健康和权限策略优先本地缓存。

## 6. 请求状态模型

```text
created
authenticated
route_selected
reserved
upstream_connected
bootstrap
semantic_committed
settled
refunded
failed
client_gone
```

一旦进入 `semantic_committed`，禁止完整重试。客户端断开不触发供应商冷却和重试统计。

## 7. 账务模型

核心表：

```text
accounts
reservations
ledger_entries
settlements
outbox_events
```

规则：

- reservation 必须有过期时间。
- settlement 和 refund 必须幂等。
- ledger 只追加，不更新历史金额。
- 余额快照可以重建，不能作为唯一事实来源。
- 任何服务禁止直接修改余额字段。

## 8. 事件模型

事件统一包含：

```json
{
  "event_id": "evt_123",
  "event_type": "billing.settled",
  "aggregate_id": "req_123",
  "occurred_at": "2026-09-05T12:00:00Z",
  "producer": "billing-service",
  "schema_version": 1,
  "idempotency_key": "settlement:req_123"
}
```

第一批事件：

```text
request.completed
request.failed
billing.reserved
billing.settled
billing.refunded
bargain.requested
bargain.resolved
payment.completed
subscription.activated
notification.created
config.changed
```

## 9. 数据边界

```text
identity-db       用户、租户、API Key
gateway-db        请求摘要和运行状态
route-db          模型、供应商、渠道和健康快照
billing-db        账户、预占、结算、账本
commerce-db       支付、订阅、奖励
marketplace-db    渠道、分组、砍价
notification-db   通知和已读状态
analytics-db      报表和聚合指标
```

服务之间只能通过 API 或事件获取数据。报表通过事件投影生成，禁止在线跨库 join。

## 10. 可靠性与性能目标

第一版目标：

| 指标 | 目标 |
| --- | ---: |
| 平台额外 P95 延迟 | < 30 ms，不含供应商响应时间 |
| 预占 P95 | < 10 ms |
| 结算 P95 | < 10 ms |
| 重试放大系数 | <= 1.2 |
| 重复结算 | 0 |
| reservation 超时回收 | < 5 分钟 |
| 事件重复消费 | 允许，但结果必须幂等 |

必须使用突发流量、长上下文、长输出和上游故障进行压测。

## 11. 新产品不继承的内容

- 旧项目的目录层级。
- 旧项目的跨模块直接数据库访问。
- 旧的多套扣费入口。
- 旧的每分钟全量配置拉取。
- 旧的一次性修复命令作为常驻服务。
- 旧的重复分页、错误转换和通知实现。
