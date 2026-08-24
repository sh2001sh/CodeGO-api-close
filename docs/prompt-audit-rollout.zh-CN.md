# Prompt Audit 灰度与回滚

Prompt Audit 使用 Qwen3Guard 对进入模型网关的客户端可控文本做安全审计。功能默认关闭，配置只从网关进程环境读取，Guard 凭据不会写入数据库。

## 模式

- `off`：不提取、不调用 Guard，默认值。
- `async`：请求照常执行，审计任务进入进程内有界队列；队列满或 Guard 失败不会影响请求。
- `blocking`：在 token 估算、预扣费和 HTTP 上游调用前同步审计；Block、无可用节点或无效响应均拒绝请求。

Realtime WebSocket 当前不支持 `blocking`。匹配 Blocking 分组的 Realtime 请求会在渠道选择、预扣费和上游拨号前返回 `prompt_guard_unavailable`。`async` 模式仍执行逐帧影子审计。

## 最小配置

```env
PROMPT_AUDIT_MODE=async
PROMPT_AUDIT_GROUPS=guard-canary
PROMPT_AUDIT_BASE_URL=https://guard.example.com/v1
PROMPT_AUDIT_API_KEY=replace-with-runtime-secret
PROMPT_AUDIT_MODEL=Qwen/Qwen3Guard-Gen-0.6B
PROMPT_AUDIT_TIMEOUT_MS=1500
PROMPT_AUDIT_INPUT_LIMIT=2000
PROMPT_AUDIT_LATEST_TURN_ONLY=true
PROMPT_AUDIT_BLOCK_CONTROVERSIAL=false
PROMPT_AUDIT_QUEUE_CAPACITY=256
PROMPT_AUDIT_WORKERS=1
PROMPT_AUDIT_GLOBAL_CONCURRENCY=1
PROMPT_AUDIT_PER_NODE_CONCURRENCY=1
```

`PROMPT_AUDIT_GROUPS` 为空时覆盖所有分组。生产首次启用必须填写独立的 canary 分组，禁止直接全量开启。

`PROMPT_AUDIT_BLOCK_CONTROVERSIAL` 默认是 `false`：Controversial 只记录并放行，Unsafe 才可能阻断。只有 Async 数据证明 Jailbreak、PII 和自残类别的 Controversial 误报率可接受后，才可设为 `true`。

多节点使用 `PROMPT_AUDIT_ENDPOINTS_JSON`。节点按数组顺序尝试；连接失败、429 和 5xx 可以切换下一个节点，认证失败和无效模型响应不会继续切换。

## 150 RPM / CPU-only 推荐档

对单个 CPU-only Guard 节点、网关高并发、目标约 150 RPM 的场景，推荐先固定为下面这组参数：

```env
CheckSensitiveEnabled=true
CheckSensitiveOnPromptEnabled=true
StopOnSensitiveEnabled=true

PROMPT_AUDIT_MODE=async
PROMPT_AUDIT_GROUPS=guard-canary
PROMPT_AUDIT_SCANNERS=jailbreak,pii,suicide_and_self_harm
PROMPT_AUDIT_TIMEOUT_MS=1500
PROMPT_AUDIT_INPUT_LIMIT=2000
PROMPT_AUDIT_LATEST_TURN_ONLY=true
PROMPT_AUDIT_BLOCK_CONTROVERSIAL=false
PROMPT_AUDIT_QUEUE_CAPACITY=256
PROMPT_AUDIT_WORKERS=1
PROMPT_AUDIT_GLOBAL_CONCURRENCY=1
PROMPT_AUDIT_PER_NODE_CONCURRENCY=1
```

原因很直接：

- 第一层硬拦截已经会挡掉最明确的攻击请求，第二层 Guard 只需要审边界请求，不需要全量高并发。
- `LATEST_TURN_ONLY=true` 能明显减少扫描文本体积，降低 CPU 和长尾延迟。
- 单节点下 `WORKERS=1 / GLOBAL=1 / PER_NODE=1` 才和 CPU-only Guard 的实际吞吐一致，避免多路堆积把 Guard 和网关一起拖慢。
- `TIMEOUT_MS=1500` 和 `INPUT_LIMIT=2000` 用于尽早放弃慢审计，并把异步审计控制在短文本、当前回合为主的范围。
- `QUEUE_CAPACITY=256` 允许短时突发，但不会把过期审计积压到几分钟以后才处理。

## 灰度步骤

1. 保持 `off` 部署，确认网关启动及既有请求基线正常。
2. 为独立测试分组设置 `async`，至少观察一个完整业务高峰。
3. 检查 Guard P95/P99 延迟、不可用率、无效响应率、队列丢弃数和人工复核误报率。
4. 只有当 Guard 不可用率低于 0.1%、无效响应率低于 0.05%、队列无持续丢弃且高风险误报率可接受时，才对同一分组启用 `blocking`。
5. Blocking 灰度期间保持 Realtime 关闭或把 Realtime 用户放在不匹配的分组。

## 告警建议

- 5 分钟 Guard 不可用率超过 1%。
- 任意 5 分钟出现无效响应。
- Async 队列持续丢弃超过 1 分钟。
- Blocking 后网关 503 比例较基线增加 0.5 个百分点。
- Guard P99 接近 `PROMPT_AUDIT_TIMEOUT_MS` 的 80%。

当前指标保存在网关进程内，尚未接入统一 Metrics API。接入监控前必须通过结构化系统日志采集 `prompt audit decision`、配置错误和队列满事件。

## 回滚

将 `PROMPT_AUDIT_MODE` 设置为 `off` 并滚动重启 `gateway-api`。配置由 `sync.Once` 在进程首次使用时加载，修改环境变量不会热更新。

回滚不需要数据库操作。Async 队列是进程内临时队列，进程退出时未完成任务会丢弃；它只用于影子审计，不得作为合规审计记录源。

## 凭据要求

- Guard API Key 只能通过密钥管理系统或部署环境注入。
- 不得把真实 Key 写入 `.env.example`、Compose 文件、数据库选项或日志。
- 多节点 JSON 中包含明文 Key 时，部署平台必须把整个变量视为 secret。
- Guard 节点由运维管理员配置，不得允许普通用户提交 Base URL。
