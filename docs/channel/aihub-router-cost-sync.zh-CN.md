# AIHubRouter 采购倍率同步

当多个 CodeGo 渠道分别使用由 AIHubRouter 管理的 AIHub Key 时，控制服务可以从各实例的 `watch --json` 审计日志同步当前实际分组倍率。该同步仅更新自动池的采购成本，用于选路和收益统计；不会改变用户价格、套餐额度或已完成请求的账本快照。

## 启用条件

1. AIHubRouter 必须以 `watch --json --log-file <path>` 运行。
2. 控制服务容器必须以只读方式挂载日志目录。
3. 对应渠道必须已作为显式成员加入至少一个自动池。同步会更新该渠道在所有自动池中的成员倍率。

## 控制服务环境变量

```env
AIHUB_ROUTER_COST_SYNC_SOURCES=[{"channel_id":50,"log_file":"/var/run/aihub-router/key11016.jsonl"},{"channel_id":52,"log_file":"/var/run/aihub-router/key11239.jsonl"},{"channel_id":53,"log_file":"/var/run/aihub-router/key11240.jsonl"},{"channel_id":54,"log_file":"/var/run/aihub-router/key11422.jsonl"}]
AIHUB_ROUTER_COST_SYNC_INTERVAL_SECONDS=60
AIHUB_ROUTER_COST_SYNC_MAX_AGE_SECONDS=180
AIHUB_ROUTER_COST_SYNC_MAX_MULTIPLIER=0.15
```

上例中的渠道与 Key 映射仅为格式示例。部署前必须以实际渠道 Key 对应的 AIHub Key ID 为准，不能依赖渠道名称或创建顺序。

控制服务容器还需要增加只读挂载：

```text
-v /mnt/codego-data/aihub-router/logs:/var/run/aihub-router:ro
```

## 安全行为

- 日志中最新周期是 dry-run、缺少目标分组、倍率不在 `(0, max]`，或超过最大年龄时，该渠道保持原有倍率。
- 每个渠道独立校验，单个 AIHubRouter 实例异常不会覆盖其他渠道的有效成本。
- 只更新 `route_pool_members.cost_multiplier`，不改成员启用状态和手工配置的模型覆盖倍率。
- 更新在一个数据库事务内执行，并清除自动池缓存；网关最多在其现有缓存周期后使用新倍率。
