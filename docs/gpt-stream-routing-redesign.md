# GPT 流式路由与故障转移重构方案

## 1. 目标与边界

目标不是让所有 503 消失，而是把可恢复故障在下游响应提交前完成切换，把不可安全重放的故障准确结束，并避免重试风暴、缓存粘性失效和全量渠道冷却。

必须满足：

- 语义输出提交前的连接失败、容量失败和无首包超时，可以切换到备用渠道。
- 已提交模型语义内容后禁止重新发送完整请求，避免 Codex 收到重复文本或重复工具调用。
- 客户端主动断开不计入渠道健康、不触发冷却、不消耗重试预算。
- 生图和长上下文使用独立的首包/总时长策略，不能套用普通 GPT 的短窗口。
- 每个请求拥有有限的尝试预算、时间预算、故障域预算和成本预算。
- 任何最终上游错误都保留内部原因、请求阶段和尝试链；在尚未提交下游响应时，用户端继续返回现有脱敏 `503 当前模型服务暂不可用，请稍后重试`，不暴露渠道、故障域、原始上游状态或上游 request id。

## 2. 现状诊断

当前网关已经具备路由池成本评分、渠道/故障域健康、半开探测、prompt cache 粘性和 Responses 生命周期缓冲，这是基础优势。

主要问题是职责分散：

1. `relay_handlers.go` 决定重试，`shouldRetry` 决定是否允许重试，执行器决定流阶段，健康模块又单独记录冷却，缺少统一的“本次尝试是否已提交下游”的事实源。
2. `ResponseBodyDelivered`、`StreamContentDelivered`、Responses 生命周期状态分别由多个模块设置，容易出现“已写响应头但未写语义内容”“已写生命周期但仍可重试”之间的边界不一致。
3. 用户侧需要统一保留脱敏 503，这一安全边界是正确的；问题在于内部把上游 502/504、无候选、账户 403、客户端取消和本站资源问题混成一个不可审计的最终结果，管理员难以从一条访问日志判断是否发生过重试。
4. 健康状态主要以进程内状态参与选路。多网关或重启后，同一故障域可能被不同节点重复探测，导致轮流失败。
5. 只要多个候选在短时间同时失败，剩余渠道会被连续打入半开/冷却，最终出现 `no available channel` 峰值；单个请求的最后探测缺少全局并发配额。
6. `timeout waiting for goroutines to exit` 是流清理阶段的二次症状，不应作为上游失败原因，也不应触发新的冷却。

## 3. 参考项目的可借鉴点

### CLIProxyAPI

重点参考 `sdk/cliproxy/auth`：

- 账号级和模型级冷却状态分离，支持持久化恢复。
- 选择器会记录已经尝试过的 auth，避免同一请求反复拿到同一凭据。
- transient 错误会合并冷却；同一 HTTP/2 或代理故障造成的并发断流不会被每条请求重复计数。
- 当断路器把所有候选都挡住时，允许一次有界 fail-open 探测，而不是直接返回无可用账户。
- 每次执行都有独立的 attempt context 和取消资源，旧尝试结束后才释放选择范围。

不直接照搬：CLIProxyAPI 的 auth/home 模型与 CodeGo 的渠道/分组不同，不能把账号健康简单映射成全局渠道禁用。

### sub2api

重点参考：

- `readStreamBootstrap` 先读取第一批流块，确认流不是空的，再把内容交给下游。
- 一旦 `streamStarted`/SSE 字节已经写入，故障转移守卫立即停止，改发协议错误事件，禁止拼接第二个 `message_start`。
- OpenAI 代理的断流断路器按代理 ID 统计，并以约 3 秒折叠同一 HTTP/2 故障爆发，避免一个连接事故触发全量熔断。
- 断路器全挡住候选时，第二次选择可忽略断路器进行一次 fail-open，优先保证服务可用。
- 流错误被分类为稳定的客户端错误码，原始上游文本不直接暴露。

不直接照搬：sub2api 的代理拓扑更简单，CodeGo 还要考虑模型倍率、套餐计费、故障域和 prompt cache 粘性。

### 原始 new-api

可借鉴其成熟的“按分组/优先级选择渠道”和协议适配，但不保留把失败完全绑定在单次渠道选择上的模式。CodeGo 应将它降级为 `CandidateProvider`，由统一尝试控制器决定是否切换。

## 4. 新的核心模型：Attempt Controller

新增统一的 `AttemptController`，负责一条下游请求的整个生命周期。执行器只报告事件，不再自行决定是否重试。

### 4.1 尝试阶段

每次尝试只有以下五个阶段：

1. `selected`：已选定渠道、key、模型和故障域，但尚未发出上游请求。
2. `connected`：上游 HTTP 响应已建立，保存状态码和响应头，但尚未向下游提交。
3. `bootstrap`：收到生命周期事件或首个流块，仍未收到模型语义内容。
4. `semantic_committed`：已经向下游写出文本 delta、tool call、图像结果或等价语义内容。进入不可重放状态。
5. `completed` / `failed` / `client_gone`：请求结束。

重试判定只看阶段和结构化错误，不再看一个模糊的 `ResponseBodyDelivered` 布尔值。

### 4.2 可重试矩阵

| 错误 | 阶段 | 动作 |
|---|---|---|
| 408/429/500/502/503/504/524 | selected/connected/bootstrap | 在剩余预算内跨故障域重试 |
| Responses 生命周期后 EOF，未有语义事件 | bootstrap | 丢弃缓存生命周期事件，切换渠道 |
| 首包超时且未有语义事件 | bootstrap | 切换渠道；记录首包超时 |
| 401/403 认证失败 | connected/bootstrap | 仅当前 key/model 失效，尝试另一 key；不禁用整个渠道 |
| 余额/模型不可用 | connected/bootstrap | 当前 key+模型短期冷却，存在替代时切换 |
| 已提交文本、tool call、图像结果 | semantic_committed | 禁止完整重试，发送脱敏的协议级 `error` 事件并结束 |
| 客户端取消/断开 | 任意 | 不重试、不冷却、不计入成功率分母 |
| 413、敏感词、额度不足、参数错误 | 任意 | 不重试，保留本地业务错误 |

### 4.3 重试预算

默认 GPT 短上下文：最多 2 次尝试（初始 + 1 次备用）；只有在第一次失败发生于 `bootstrap` 且剩余总时长不少于 6 秒时，才允许第二次。

长上下文：最多 1 次备用，但使用请求级总时长预算，不使用 20 秒固定首包阈值。

生图/非流式：使用独立的 45 秒首包窗口和更长总时长，不进入普通 GPT 的首包冷却。

每次重试必须同时满足：

- 当前请求尚未提交语义内容。
- 当前故障域本次请求未尝试过。
- `attempts < max_attempts`。
- `now < request_deadline - reserve_for_response`。
- 全局/故障域重试令牌桶仍有令牌。

重试等待使用 50~150ms 全抖动；上游提供 `Retry-After` 时限制在 100ms~2s。禁止固定 sleep 和无界循环。

## 5. 流式响应提交屏障

### 5.1 Bootstrap Buffer

继续保留当前 Responses 生命周期缓存，但将它抽成协议无关的 `StreamBootstrapBuffer`：

- 默认最多 128 个事件、1MiB；超限只丢弃可重建的生命周期事件，不能丢语义事件。
- 在首次语义事件前，不调用会导致 HTTP 头/状态实际提交的 flush。
- 首次语义事件到来时一次性提交响应头、缓存的生命周期事件和当前事件。
- bootstrap EOF/502/504 时直接丢弃缓存并交给 Attempt Controller 重试。

### 5.2 已提交内容的结束方式

当前 `finalizeRelayError` 在响应已经写出后直接 return，容易让客户端只看到连接断开。改为：

- 尚未向下游提交任何字节：最终仍返回 HTTP `503`，消息固定为 `当前模型服务暂不可用，请稍后重试`；保留本站 request id，但不输出渠道、故障域、原始状态码或上游 request id。
- Responses：已提交语义内容时发送一次脱敏的 `error` 或 `response.failed` 事件，使用与 HTTP 503 相同的稳定用户消息、本站 request id 和 `retryable=false`，然后正常结束流。
- Chat Completions：已提交语义内容时发送一个符合 SSE 的脱敏错误块和 `[DONE]`，不改变已经提交的 HTTP 状态。
- Anthropic：已提交语义内容时发送一个标准的脱敏 `error` SSE 事件。

这不是重试，而是将不可重放失败变成可识别的协议结束，避免客户端把长时间断流误认为无原因 503。

## 6. 健康、断路器与故障域

### 6.1 健康键

健康状态按以下键维护：

`{provider, normalized_base_url, channel_id, key_fingerprint, model, protocol}`

故障域单独维护：

`{normalized_base_url, proxy_id, region, model, protocol}`

不同 key 只有在共享 base URL/代理/上游错误签名时才合并到同一故障域，不能按渠道 ID 硬编码。

### 6.2 失败折叠和冷却

- 同一健康键 3 秒内的断流只计一次；同一故障域在 3 秒内的并发断流只增加一次故障权重。
- 使用 Beta-Binomial 平滑成功率：先验 `alpha=2, beta=1`，窗口内样本不足 10 时不做硬冷却。
- 连续两次可归因的上游失败进入 `degraded`；连续三次或可信成功率低于 60% 才进入 `cooling`。
- 冷却采用 5s、15s、45s、120s、300s 指数退避，上限 5 分钟并加入 20% 抖动。
- 半开：每故障域每 10 秒最多 1 个正常探测；全挡住时每故障域最多 1 个 `last_resort` 探测。
- 探测成功立即恢复，但恢复后先以 10% 流量逐步放大；失败重新进入下一级冷却。

### 6.3 Fail-open

只有满足以下条件才 fail-open：

- 当前请求在 `bootstrap`，没有语义输出。
- 健康候选为空，但存在最近成功过且非永久认证/余额失败的候选。
- 全局故障域探测令牌可用。

一次 fail-open 只允许一个候选，不解除全局冷却、不批量解冻、不改变其他请求的正常选路。

## 7. 选路评分与粘性

候选分数建议：

`score = cost + reliability_penalty + ttft_penalty + cooldown_penalty + domain_load_penalty + switching_penalty`

- `cost` 只影响健康候选之间的排序。
- `reliability_penalty` 使用可信成功率和 502/504/429 分类权重。
- `ttft_penalty` 使用 P50/P95 首包，并按模型/上下文长度分桶。
- `domain_load_penalty` 来自自适应并发窗口，避免把多个 key 同时打到同一上游。
- `switching_penalty` 防止没有故障时频繁切换，保留 prompt cache 粘性。

使用“两次选择”而不是全量排序：随机取两个满足硬条件的候选，选择分数更低者；对最低成本候选保留 70% 粘性，对高成功率候选保留 30% 探索。探索和切换均按故障域分散。

## 8. 分布式状态与并发保护

Redis 保存短 TTL 的共享状态：

- `health:{model}:{key_fp}`：失败计数、可信成功率、cooling_until、half_open_ticket。
- `fault:{model}:{domain}`：故障域状态和探测票据。
- `retry-budget:{model}:{domain}:{10s_bucket}`：重试令牌桶。
- `affinity:{user}:{model}:{cache_fp}`：软粘性，不可作为唯一候选。

使用 Redis Lua 或带版本号的 CAS 更新，避免多网关同时半开；进程内只做 1~2 秒热缓存。Redis 不可用时降级为本地状态，但审计标记 `health_state_scope=local`。

## 9. 观测与审计

每一次尝试记录一条结构化 attempt event，最终请求再记录一条 summary：

- `request_id`, `attempt_id`, `retry_index`
- `model`, `requested_group`, `selected_group`
- `channel_id`, `key_fp`, `fault_domain`
- `stage_before_error`: selected/connected/bootstrap/semantic_committed
- `upstream_status`, `failure_class`, `client_gone`
- `retry_decision`: retry/stop, `retry_reason`
- `excluded_candidates`, `excluded_domains`
- `ttft_ms`, `total_ms`, `stream_events`, `semantic_output_seen`
- `health_scope`: local/shared, `probe_mode`

管理端指标：成功率、bootstrap retry rate、semantic non-retry rate、P50/P95 TTFT、P95 总时长、每故障域并发、冷却覆盖率、fail-open 次数、重复候选率、重试放大系数。

用户端对最终不可恢复的上游故障统一显示当前脱敏 503 语义：`当前模型服务暂不可用，请稍后重试`。流已提交后以等价 SSE 错误事件表达；两种路径均不显示渠道名称、故障域、原始上游状态、上游 request id 或内部冷却原因。

## 10. 分阶段实施

### Phase 0：影子模拟

用最近 24 小时审计日志重放，不改变真实路由，比较当前策略与新策略：成功率、重试次数、预计缓存损失、上游成本、P95 首包。先验证 67/61/32 故障波是否能在 bootstrap 阶段切换。

### Phase 1：统一事实源

新增 `AttemptController`、`StreamBootstrapBuffer`、`FailureEvent` 和结构化审计；保留现有选路逻辑作为候选提供器。为 Responses、Chat Completions、Claude 各写状态机单测。

### Phase 2：共享健康与有界 fail-open

Redis CAS 健康状态、故障域断路器、3 秒故障折叠、半开票据和重试令牌桶。先仅对 GPT 短上下文开启，生图和长上下文继续旧策略。

### Phase 3：协议级结束与灰度

5% GPT 用户灰度，确认无重复语义输出后扩大到 25%、50%、100%。任何以下条件触发自动回滚：重复语义事件、重试放大系数 > 1.3、P95 总时长增加 20%、账单幂等失败、数据库连接池异常。

### Phase 4：清理旧路径

删除 `relay_handlers.go` 中分散的用户保护、故障域容量和重试分支，只保留 Attempt Controller 的单一入口；保留老字段兼容读取 2 个版本周期。

## 11. 必测场景

1. 上游连接前 503：备用渠道成功，用户只看到一次 200。
2. Responses 只收到 `response.created` 后 EOF：生命周期不转发，切换成功。
3. 收到文本 delta 后 EOF：不重放，发送协议级错误事件，渠道进入健康衰减。
4. 收到 tool call 后 EOF：不重放，记录 `semantic_committed`。
5. 客户端取消：无冷却、无重试、无错误率污染。
6. 连续 100 个并发请求共享一个 HTTP/2 故障：故障折叠后只产生一个故障域事件。
7. 所有候选冷却：只发起一个 last-resort 探测，不出现同步解冻风暴。
8. 429 带 Retry-After：遵守 100ms~2s 上限并跨故障域。
9. 长上下文首包超过 30 秒：不误判为普通 GPT 超时。
10. 生图 45 秒内无首字：不触发普通文本冷却。
11. 预扣、重试、退款和最终计费的幂等键保持不变。

## 12. 参考资料

- Jeffrey Dean, Luiz André Barroso, “The Tail at Scale”, Communications of the ACM, 2013. DOI: [10.1145/2408776.2408794](https://doi.org/10.1145/2408776.2408794)。用于尾延迟、选择性 hedging 和避免慢节点拖垮整体响应。
- Michael Mitzenmacher, “The Power of Two Choices in Randomized Load Balancing”, IEEE Transactions on Parallel and Distributed Systems, 2001. DOI: [10.1109/71.963420](https://doi.org/10.1109/71.963420)。用于低成本候选抽样与负载分散。
- Richard K. Williams et al., “Progressive Retry for Software Error Recovery in Distributed Systems”, FTCS, 1993. DOI: [10.1109/FTCS.1993.627317](https://doi.org/10.1109/FTCS.1993.627317)。用于渐进式重试和避免恢复风暴。
- Google SRE Book, “Addressing Cascading Failures” 与 “Handling Overload”。用于重试预算、过载保护和客户端/服务端责任边界。
- AWS Architecture Blog, “Exponential Backoff And Jitter”。用于带抖动的退避，避免同一故障恢复时同步重试。
- Nygard, *Release It!*, Circuit Breaker pattern。用于断路器、半开恢复和故障隔离边界。

## 结论

最合适的方向不是增加重试次数，而是把“是否已经提交语义内容”变成唯一事实源，先建立 bootstrap 提交屏障，再将渠道健康、故障域健康、重试预算和 fail-open 探测统一到 Attempt Controller。这样可以在不重复计费、不重复工具调用、不破坏 prompt cache 粘性的前提下，消化 502/504/503 波动；真正所有上游同时不可用时，则以可解释的协议错误结束，而不是让用户等待数十秒后只看到一个没有上下文的 503。
