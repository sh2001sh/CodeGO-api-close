# 新产品实施计划

## 1. 实施方式

新建独立仓库和独立运行环境，不修改旧项目。旧项目只作为：

- API 行为参考。
- 业务规则参考。
- 上游协议适配参考。
- 账务边界和异常案例参考。
- 压测数据和日志样本来源。

任何旧代码迁移前必须经过接口、依赖、测试和性能审查。

## 2. 仓库骨架

```text
ai-platform/
├── services/
│   ├── edge-gateway/
│   ├── request-runtime/
│   ├── billing-service/
│   ├── upstream-runtime/
│   ├── identity-service/
│   ├── marketplace-service/
│   ├── commerce-service/
│   └── worker-runtime/
├── packages/
│   ├── contracts/
│   ├── event-schemas/
│   ├── observability/
│   ├── errors/
│   └── testkit/
├── deploy/
│   ├── compose/
│   ├── k8s/
│   └── observability/
├── docs/
├── scripts/
└── Makefile
```

每个服务模板：

```text
service/
├── cmd/server
├── internal/domain
├── internal/application
├── internal/repository
├── internal/transport/http
├── internal/events
├── internal/infrastructure
├── migrations
├── api/openapi.yaml
├── service.yaml
├── Dockerfile
└── README.md
```

模板只提供启动、配置、日志、健康检查、超时、幂等、指标和优雅退出，不预置无使用场景的抽象。

## 3. 阶段计划

### Phase 0：契约和基线，1 周

产出：

- 服务目录和依赖规则。
- OpenAPI 契约。
- 事件 schema。
- 请求状态机。
- 账务状态机。
- P95/P99 指标定义。
- 本地 Compose 环境。

验收：所有服务可以启动，契约检查通过，事件可以发布和消费。

### Phase 1：基础设施骨架，1 周

实现：

- 配置加载。
- 结构化日志。
- Trace ID。
- 健康和就绪检查。
- 数据库连接池。
- Redis 客户端。
- 事件总线客户端。
- Outbox 基础实现。

验收：服务可以独立启动、停止、检查和发布版本化事件。

### Phase 2：请求最小闭环，1～2 周

实现：

- `edge-gateway`。
- `request-runtime`。
- 一个协议适配器。
- 一个 `upstream-runtime`。
- 基础 route snapshot。

验收：能够完成一次非流式和一次流式请求；不接入真实扣费，先使用测试 billing。

### Phase 3：账务闭环，1～2 周

实现：

- account。
- reservation。
- settlement。
- refund。
- ledger。
- reservation 超时回收。
- 幂等和并发测试。

验收：重复请求不重复扣费；上游失败退款；流式中断按 usage 结算；并发预占不超卖。

### Phase 4：路由和故障治理，1～2 周

实现：

- 多供应商。
- 连接池。
- 故障域。
- 熔断和半开。
- bootstrap 重试。
- 重试令牌桶。
- 长上下文和短请求分池。

验收：模拟 429、502、503、504、超时、连接断开和客户端取消，确认不会重试已提交语义内容。

### Phase 5：身份、商业和 marketplace，2～3 周

按顺序实现：

1. identity 和 API Key。
2. payment 和 subscription。
3. marketplace 和渠道。
4. bargain 和倍率覆盖。
5. benefit 和 reward。

验收：每个领域只写自己的数据；跨领域通过 API 或事件；砍价通知和订阅开通不阻塞模型请求。

### Phase 6：异步和分析，1 周

实现：

- notification。
- audit projector。
- analytics projector。
- reconciliation worker。
- 配置变更事件。

验收：关闭消费者后主请求仍可完成；恢复消费者后事件可以重放；重复消费结果不变。

### Phase 7：功能迁移，按功能批次进行

迁移顺序：

```text
模型和协议适配
→ 路由和供应商
→ API Key 和身份
→ 账务和额度
→ 充值和订阅
→ marketplace 和砍价
→ 通知、审计和报表
```

每批迁移都必须有：

- 新旧行为对照。
- 接口兼容记录。
- 数据迁移脚本。
- 回放测试。
- 压测结果。
- 回滚开关。

## 4. 旧实现迁移规则

允许迁移：

- 已验证的供应商协议适配器。
- 已验证的价格计算规则。
- 已验证的错误分类。
- 已验证的账务边界测试。
- 真实日志中的异常案例测试。

禁止直接迁移：

- 旧 handler 的跨模块逻辑。
- 直接修改余额的函数。
- 依赖全局变量的运行时状态。
- 旧的定时全量同步循环。
- 无调用关系证明的一次性修复代码。

迁移采用“提取规则，不搬目录”的方式：先写新接口和测试，再从旧实现提取最小逻辑。

## 5. 测试策略

### 单元测试

- 状态机。
- 价格计算。
- 路由评分。
- 重试判断。
- reservation 幂等。
- settlement 幂等。

### 契约测试

- HTTP OpenAPI。
- 事件 schema。
- 服务错误码。
- 版本兼容。

### 集成测试

- gateway 到 upstream。
- gateway 到 billing。
- outbox 到 worker。
- 支付到订阅。
- 砍价到通知。

### 压测

- 稳定并发。
- 突发流量。
- 长上下文。
- 长输出。
- 上游 429/5xx。
- 单账户高并发。
- 热门模型热点。

## 6. 发布和回滚

每个服务支持：

- 独立版本。
- 独立健康检查。
- 灰度比例。
- 超时和熔断配置。
- 旧契约兼容期。
- 一键关闭新路径。

数据库只做向前兼容迁移。账务迁移必须先双读或影子计算，连续对账通过后再切换写路径。

## 7. 完成标准

- 新产品可以独立构建、部署和运行。
- 模型请求不依赖旧项目运行。
- 账务没有直接改余额的旁路。
- 热路径远程调用不超过三个核心阶段。
- 配置采用事件广播和本地快照，不再每分钟全量拉取。
- 通知、审计和统计不阻塞模型响应。
- 关键状态和事件可追踪、可重放、可补偿。
- 旧功能只按经过测试的规则迁移，不整体搬运旧代码。
