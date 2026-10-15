# CodeGo v2 生产迁移 Runbook

本文档定义 CodeGo v2 的生产切换顺序。所有步骤均可重复执行；不通过验收不得进入下一步。

## 1. 前置条件

- 已备份主库和日志库，并完成恢复演练。
- Redis 已配置，所有 `gateway-api` 实例连接同一个 Redis。
- Temporal 已部署，且已配置 `TEMPORAL_HOSTPORT` 和可访问的 namespace。
- 准备四个独立进程：`control-api`、`gateway-api`、`workflow-worker`、`ledger-worker`。

## 2. 数据库迁移

先以只读方式检查，再执行迁移：

```powershell
go run ./cmd/db-migrate --dry-run
go run ./cmd/db-migrate
go run ./cmd/db-migrate
```

第二次执行必须成功且不创建重复 schema migration。迁移包含账本、工作流、订阅、网关执行模型和读模型表。

仅对全新空数据库，首轮改为 `go run ./cmd/db-migrate --bootstrap`；该模式会建立旧业务基表后再执行版本化 v2 migration，禁止用于已有生产库。

## 3. 历史账本回填

先执行 dry-run，核对待回填数量；确认后分批执行：

```powershell
go run ./cmd/ledger-backfill
go run ./cmd/ledger-backfill --apply --limit 1000
go run ./cmd/ledger-backfill --apply
```

回填会为钱包、Claude 钱包、令牌和订阅创建缺失的账本账户，并以幂等 bootstrap entry 写入旧余额。已经存在账本流水的账户不会被重复授信。

`v2-verify --strict` 是只读切换闸门：它必须确认全部 v2 migration 已记录、四类额度主体均已建立账本账户、全部余额快照可由账本重建、outbox 已排空，且不存在待迁移盲盒额度。由于回填会产生 outbox 事件，该命令必须在启动 `ledger-worker` 后运行。

## 4. 进程切换

1. 启动 `ledger-worker`，确认 outbox、账本快照、读模型和 SLO 循环正常。
   随后执行 `go run ./cmd/v2-verify --strict`，必须无错误。
2. 启动 `workflow-worker`，确认四个 Temporal task queue 已注册。
3. 启动 `control-api`，验证运营、对账和审计接口。
4. 将流量切到 `gateway-api`，确认其只提供网关请求路由。

每个 worker 必须只在 master node 运行。不得在未配置 Redis 或 Temporal 时将多副本网关、工作流流量切到新运行时。

## 5. 灰度验收

- 钱包、Claude 钱包、令牌、订阅各执行一笔成功请求和一笔失败请求。
- 核对每笔请求存在 reservation、settlement 或 release，且余额快照可重建。
- 验证首包前上游失败会切换备用渠道，并在审计日志记录 `route_decision`。
- 验证同一请求重放不会生成重复账本流水、执行记录或用量证据。
- 验证异步任务失败会由 `AsyncTaskWorkflow` 释放 reservation。
- 验证订阅订单在支付确认后进入 `fulfillment_status=pending`，由 `OrderFulfillmentWorkflow` 发放权益并转为 `completed`；停止并恢复 `workflow-worker` 后，待履约订单仍会被补偿调度。
- 验证订阅周期重置由 `workflow-worker` 启动 Temporal workflow，生成账本 adjustment 审计，且不清除累计额度。
- 验证充值、兑换码、每日签到、盲盒奖励和邀请额度转入均生成账本 credit entry 与 outbox 事件，旧额度字段只作为投影。

## 6. 完成门槛

仅在以下条件同时满足时，允许宣布生产完全迁移：

- `go test ./...` 通过。
- `db-migrate`、`ledger-backfill --apply` 和 `v2-verify --strict` 在生产快照上重复执行无错误。
- 对账结果无未解释差异。
- Redis、Temporal、四个独立进程均有运行监控和告警。
- 灰度请求全部通过，并完成一次故障回退演练。
