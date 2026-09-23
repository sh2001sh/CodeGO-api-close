# 后端结构盘点

## 结论

当前后端已经按领域拆分，但网关、平台基础设施和运营业务边界仍然交叉。系统偏重的主要原因是历史兼容、多个运行入口和重复业务路径叠加。

## 规模

| 模块 | Go 文件 | 主要职责 |
| --- | ---: | --- |
| gateway | 523 | 协议、路由、上游、流式、运行时、请求审计 |
| platform | 206 | 配置、数据库、缓存、安全、迁移、运行时 |
| commerce | 190 | 充值、订阅、盲盒、奖励 |
| identity | 145 | 用户、权限、令牌、设置 |
| marketplace | 94 | 渠道、分组、倍率、砍价 |
| workflow | 82 | 工作流和异步任务 |
| billing | 70 | 账户、预占、结算、账本 |

## 运行入口

当前共有 12 个 cmd 入口：`gateway-api`、`control-api`、`ledger-worker`、`workflow-worker`、`db-migrate`、`ledger-backfill`、`marketplace-multiplier-correction`、`marketplace-multiplier-refund`、`group-buy-reconcile`、`unified-credit-migrate`、`backfill-inviter`、`v2-verify`。

这些入口反映出系统已经拆分运行，但补偿、修复和业务 worker 还没有统一任务模型。

## 关键问题

1. `gateway` 同时包含请求入口、协议转换、路由、上游调用、流式处理和扣费触发。
2. `platform/store/migrations.go` 集中了历史迁移、兼容修复和新业务迁移。
3. `billing` 已有 reservation、settlement、ledger、outbox，但旧的 relay billing 入口仍存在。
4. `commerce`、`marketplace` 和 `billing` 存在交叉读写，导致业务变更需要修改多个模块。
5. 多个一次性修复命令长期保留，增加构建和部署复杂度。

## 删除候选

后续按调用关系确认后删除：无路由和无命令消费者的 handler；已被 reservation/settlement 替代的旧余额扣减函数；已完成的一次性修复命令；重复分页和错误转换；新迁移已经覆盖的重复 `AutoMigrate` 分支。

没有调用证据的历史兼容代码暂不直接删除，先列入迁移和回滚清单。
