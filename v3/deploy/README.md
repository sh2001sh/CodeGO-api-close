# 独立测试环境与全量切换

测试环境使用独立 PostgreSQL、Redis、网关、控制面、worker 和新前端。数据库端口不向宿主机开放，网页仅监听 `127.0.0.1`；默认端口18083，用 `V3_TEST_PORT` 选择未占用端口。2026-10审计整改使用独立项目 `codego-v3-repair-20261004` 和18086，原18085项目及数据保留。当前本地重构尚未生产切换。

## 配置与启动

准备未入库的环境文件，至少设置 `V3_TEST_PG_PASSWORD`、`V3_SECRET_KEY`、独立 `V3_SESSION_SECRET` 和 `V3_TEST_PORT`。加密密钥使用32字节随机值的base64，会话密钥至少32字节。导入旧渠道/API Key 时，迁移工具与运行服务必须使用同一个目标加密密钥；已有环境不能覆盖该密钥。环境文件、RSA私钥及集成凭据不写进仓库或日志。

Compose 的 Redis 使用单主、AOF 和 `noeviction`，网关/worker各有 WAL 卷。网关和 worker共享 `V3_FILES_DIR=/data/files`，worker通过 `V3_INTERNAL_GATEWAY_URL=http://gateway:3001` 执行真正计费的渠道批量测试。当前文件配额/去重协调适用于单上传网关，不承诺多个上传进程共享卷的严格一致性。

可信代理地址通过 `V3_TRUSTED_PROXY_CIDRS` 配置，逗号分隔；仅这些对端的 `X-Forwarded-For` / `X-Real-IP` 用于客户端 IP 策略与鉴权失败限制。审计采样默认关闭；worker仅在明确设置 `V3_AUDIT_SAMPLE_RETENTION_DAYS`（1–3650天）时清理样本，保留期应与实际策略一致。

测试入口会覆盖外部传入的 `X-Forwarded-For`、`X-Real-IP`；不能把整个 Docker 网段、`0.0.0.0/0` 或 `::/0` 配成可信代理。Docker 为每个隔离项目分配不同网络，首次启动后读取该项目 `web` 容器的 IPv4，只把这个 `/32` 地址写入私有环境文件的 `V3_TRUSTED_PROXY_CIDRS`，再用原参数执行 `up -d --no-deps gateway control`。`web` 重建后核对地址并同步；生产使用真实边缘代理的固定对端地址，代理前若还有负载均衡器，只接收经过验证的来源链。未配置时保持拒绝信任转发头，但用户会共享代理地址的鉴权限制。

PostgreSQL 测试容器共享内存为 1 GB，避免并行查询在 Docker 默认 64 MB 中失败；查询本身仍需优化。Redis 数据预算默认 1 GiB，容器限制 3 GiB，为 AOF 重写与写时复制留出空间；分别使用 `V3_TEST_REDIS_MAXMEMORY_BYTES` 与 `V3_TEST_REDIS_CONTAINER_MEMORY` 根据测量调整。达到数据预算时明确拒绝写入，不能改成淘汰资金、幂等或预扣数据。AOF `everysec` 仍有突然掉电的持久化窗口，WAL 和资金恢复测试不等于整机硬崩溃零损失。

网关、控制面和 worker 的 PG 池上限分别为 32、32、16，一套常驻池预算为 80；另计通知和目录 `LISTEN`、迁移与运维连接，PostgreSQL 的 `max_connections=100` 只适合当前单套配置。增加副本前按全部进程计算预算，预留管理连接，不以提高连接数替代查询优化。后台及入口停止宽限为 60 秒，覆盖应用 30 秒退出等待；超过 30 秒的流仍须先停止新接单并主动排空，不能靠 `docker stop` 保证无限长流无中断。

Nginx 对网关和控制面使用有界上游连接复用，控制 JSON 启用 gzip；模型流和通知事件禁用压缩与缓冲。通知入口为 `/api/notifications/events`。独立代理回归覆盖复用、伪造转发头、首事件即时返回及上游失败原样返回：

```bash
python3 v3/deploy/verify-proxy.py
```

账户请求保护沿用 `REQUEST_ABUSE_GUARD_ENABLED`，默认关闭；迁入的限制次数、限制截止时间和封禁状态在启用后生效。使用采样由资金事务记录，worker独立处理，避免保护规则与资金锁互相等待。

社区评分由主站管理。worker 使用 `CODEGO_COMMUNITY_RATING_EVENTS_URL` 指向社区 `/api/codego/rating-events`，并与社区、control 配置同一个独立的 `CODEGO_COMMUNITY_API_SECRET`（至少32字符）；不使用 OIDC 密钥或用户 API Key。未设置事件地址时明确停用投递，已提交事件保留在 `v3_community.rating_event_outbox`，恢复配置后继续处理。数据库 `LISTEN/NOTIFY` 仅用于唤醒；空队列不定时查询社区，失败事件按10秒至5分钟退避重试，多个worker通过 `SKIP LOCKED` 领取事件。

事件请求使用 `application/vnd.codego.rating-event+json`，字段仅为公开 `channel_id`、`owner_sub` 和字符串 `version`（outbox记录ID）。同时发送 Bearer、Unix秒 `X-CodeGo-Timestamp` 和 `X-CodeGo-Signature`，签名为独立密钥计算的 `HMAC-SHA256(timestamp + "." + 原始请求字节)` 小写十六进制。社区限制16KB及五分钟时间偏差，先查询主站权威评分，成功广播后记录版本；失败返回503，重复或旧版本返回200且不再广播。该媒体类型保留NodeBB全局JSON解析器之前的原始签名字节，不能换成普通 `application/json`。

投递默认要求HTTPS。测试与容器内部通信仅允许回环、私网IP或明确内部名称 `localhost`、`host.docker.internal`、`nodebb`、`codego-community` 使用HTTP；禁止URL凭据、查询、片段及跳转。社区页面只展示评分并链接主站市场，收到变更通知才重新读取评分；查询失败明确提示，不用零分替代失败结果。

账号两步验证、邮箱验证及找回密码已恢复。显式 `V3_SMTP_*` 优先于迁入的 SMTP 设置；使用旧设置时，每次发送读取当前配置并解密 SMTPToken。支持默认587、465/SSL和原LOGIN服务器规则，TLS证书仍须可信；缺失设置或投递失败明确报错。短期浏览器会话、邮箱/密码验证凭证和缓存配置导入码在切换后需重新发起；TOTP、备用码已使用状态和持久桌面授权保留。

桌面旧客户端无版本头或版本2时保留 `desktop:` 权限前缀，版本3使用原生权限名；撤销/过期令牌返回401，有效令牌缺权限返回403。新请求状态通过固定容量后台队列记录，满队列或写入失败有计数和日志，故障期间可能缺少统计样本。使用金额以账本及用量日志为准，WAL待重放的请求摘要金额为0，不能用摘要替代对账。

后台模型收藏、倍率同步、部署管理和运维工具已恢复。部署操作需要真实provider配置，性能统计覆盖当前control进程及数据库连接池；v3没有旧请求磁盘缓存，清除接口返回409。三个服务写入共享日志目录，活动日志受清理保护。

以下命令在仓库根执行，选择明确的隔离项目名：

```bash
docker compose --env-file /absolute/path/test.env -f v3/deploy/compose.test.yaml -p codego-v3-acceptance up --build -d
docker compose --env-file /absolute/path/test.env -f v3/deploy/compose.test.yaml -p codego-v3-acceptance ps
docker compose --env-file /absolute/path/test.env -f v3/deploy/compose.test.yaml -p codego-v3-acceptance logs --tail=100
```

未发布迁移 SQL 改变后，旧测试卷的 checksum 拒绝是预期保护。显式更新 `atlas.sum` 并用新的隔离项目/卷重新演练，保留旧卷；不要改已应用的 checksum 来掩盖差异。停止本项目使用同样参数的 `docker compose ... down`，默认保留数据、文件及 WAL 卷。

## 真实验收

运行完整后端验证，并在最终镜像上执行全部严格断言；禁止以跳过断言的诊断副本作为通过证据。

```bash
bash v3/scripts/verify.sh boundaries test lint race integration pgtest atlas
python v3/scripts/acceptance-stack.py --project codego-v3-acceptance --url http://localhost:18083
```

脚本验证隔离项目归属，覆盖真实注册/登录/key/模型发现、权限、上游失败退款、精确资金、重放、PG/Redis 故障恢复、静态压缩及缓存。浏览器验收另覆盖最终镜像桌面/移动端用户流程和管理员拒绝。API生成可复现及前端类型、单测、构建、格式和体积检查也须运行；未配置真实环境而跳过的浏览器测试不算通过。

OIDC真实验收需要在私有测试环境文件中设置 `OIDC_ISSUER`、`OIDC_CLIENT_ID`、随机至少32字符的 `OIDC_CLIENT_SECRET`、同源已注册 `OIDC_REDIRECT_URI`，以及临时RSA（至少2048位）PEM的base64值 `OIDC_SIGNING_PRIVATE_KEY_BASE64`。Compose将这些值传给control；全部留空时不启用该集成。浏览器端另设置 `V3_REAL_URL`、`V3_REAL_PROJECT`、`V3_REAL_OIDC_CLIENT_ID`、`V3_REAL_OIDC_REDIRECT_URI` 后运行 `tests/real-oidc.spec.ts`；真实浏览器测试要求明确本地端口及隔离项目名，并通过 Docker 发布端口与项目标签核对其归属，验证登录后的S256授权续接与state保留。

套餐改造真实浏览器用例为`web/app/tests/real-subscription-v2.spec.ts`，通过`V3_REAL_URL`及`V3_REAL_PROJECT`选择独立本地 Compose 测试栈，覆盖主动同意、到期套餐禁选、真实整份转换、保留旧福利、次数换卡及独立激活；另覆盖root分段核定当前与未来周期权益、少一个微单位拒绝保存、保存不替用户转换、本人确认后停止周期发放。用例只在所选隔离数据库准备合成权益。运行前核对 Docker 项目及端口，固定简体中文避免浏览器默认语言影响选择器。

真实认证用例共用反向代理来源地址，会共同消耗每分钟10次的认证限额。运行`real-restored`时按桌面/移动端分别用`--workers=1`，与其它真实用例及整栈脚本隔开一分钟；也可在专用测试环境中重启control后分别执行，保留数据库及真实限流规则。禁止提高生产限制来让测试通过。

## v3 结算消费者与连接预算

`V3_PG_MAX_CONNS` 设置当前进程的 PostgreSQL 连接池上限，必须为正整数；未设置时 gateway/control/worker 分别使用 32/32/16。它优先于 DSN 中的 `pool_max_conns`。Compose 分别通过 `V3_GATEWAY_PG_MAX_CONNS`、`V3_CONTROL_PG_MAX_CONNS`、`V3_WORKER_PG_MAX_CONNS` 传入，便于各角色独立分配预算。

`V3_LEDGER_CONSUMERS` 为同一个 worker 进程的账务消费者数量，默认 1，范围 1–32；各消费者共享 Redis 消费组，使用不同消费名。增加该值不会重复启动 River、凭据刷新等其他后台服务。`V3_LEDGER_BATCH_SIZE` 为每个消费者每次事务的事件数，默认 500，范围 1–5000。非法值会在连接基础设施前拒绝启动。

账务批处理仍在同一事务保存逐请求结算、账本、余额版本和 outbox。市场进度回调按数字用户 ID 稳定排序，同一用户的事件顺序保持不变；市场进度与结算记录先锁定，再将本批全部扣款账户（含拆分支付）及渠道收入、佣金账户按 ID 统一锁定。收益释放同样先锁定整批账户。升级时先停止旧账务消费者并排空在途事务，再启动同一新版的消费者，避免新旧锁顺序并存。

扩容前计算所有实例连接池上限之和，并为独立 LISTEN、迁移、管理和维护连接预留空间。例如数据库 `max_connections=100` 时，两个 gateway 各 20、一个 control 20、一个 worker 16，共 76 个池连接，仍需核对额外连接。消费者共用 worker 的连接池，数量不应挤占维护任务；先测 1 与 2 个消费者及 128/500 的批次，再决定配置，不能假设消费者增加会线性提高吞吐。大批次也可能延长热点账户持锁时间。

验收同时观察持续请求速率、`codego_ledger_backlog_entries`、数据库锁等待、审计 dropped/failed、Redis 内存及资金对账。短时请求成功后仍有持续增长的积压，不能视为该速率已经达标。每个 gateway 保留独立持久 WAL 卷，扩副本前核对持久化和代理配置。

worker 的 `/metrics` 另提供 `codego_ledger_consumers`、`codego_pg_pool_{max,acquired,idle}_connections`、`codego_pg_pool_empty_acquires_total` 和 `codego_pg_pool_canceled_acquires_total`。后两项增长说明池连接不足或等待被取消；它们不能单独区分慢 SQL 与账户锁争用，需结合数据库等待事件判断。

`compose.capacity.yaml` 是可选的本地容量复现配置，与基础测试配置一起使用：

```bash
docker compose --env-file /absolute/path/test.env -f v3/deploy/compose.test.yaml -f v3/deploy/compose.capacity.yaml -p codego-v3-capacity up --build -d
```

该配置为 PostgreSQL 分配 4 CPU / 4 GiB，使用 `shared_buffers=1GB`、`max_wal_size=4GB`，保持默认 `work_mem=4MB` 及持久化设置。基础配置保持原默认值；不要把 1 GB 缓冲区复制到内存不足的实例。WAL 大小是检查点目标，需要保留磁盘余量。worker 默认 2 个消费者、每批 500 条，所有角色的连接预算仍按前述方式核算。真实上游、数据规模、流时长或消费者配置变化后重新测量，不能把本地模拟上游结果当作生产容量保证。

固定速率验收先运行不访问业务的 60 秒定速探针，并比较发压器、宿主单调计时和服务端时间。Docker/WSL 曾出现配置 500/s、600 秒且无调度丢弃，但实际到达平均仅约 451/s；其内核计时在空载时也比宿主慢约 10%。Windows 原生同版 k6 恢复正常速率，无需调高目标来补偿或重启共享 VM。服务器自身时间异常时，短时间分桶同样不可信，应保留原桶、使用可信客户端发送时序，并联合核对服务端全部成功计数及首末跨度。

容量通过必须同时满足真实速率、HTTP/响应内容、审计、资金/钱包和稳态队列门槛。原生平均 500/s 已送达、结束后全部对账排空，仍曾因持续队列斜率超过预设容差而失败；不能只依据请求总数或无丢弃认证该容量。

## 离线迁移演练

源为实际独立只读 v2 PostgreSQL，目标为隔离 v3 PostgreSQL。目标先应用正式 schema；迁移环境配置 `V3_SOURCE_PG_DSN`、`V3_PG_DSN`、`V3_SECRET_KEY`，以及源 `V3_SOURCE_FILE_STORAGE_DIR` 和目标 `V3_FILES_DIR`。目标目录必须与运行时共享卷对应，不能复制进服务看不到的临时目录。

以下使用已经构建的 `migrate` 可执行文件；所需环境由私有环境文件或运行环境提供，不在命令中写凭据：

```bash
migrate schema
migrate files
migrate files -apply -offline
migrate background
migrate background -apply -offline
migrate import
migrate import -apply -offline
migrate import -apply -offline
migrate check
migrate ledger-check
```

`files` 和 `import` 默认dry-run；`-apply` 要求 `-offline`，其含义是已停止全部v2写入方，工具不代替实际停写。先复制并校验文件资产，再导入数据。源事务只读，目标数据库导入与关联投影在一次事务内；重复导入不能重发期初资金，check须发现目标篡改或缺失。资金、身份、key、渠道、订单、订阅、市场、历史和文件均须核对，失败显式报告。

`background` 同样默认dry-run，先把已结算终态响应及事件复制到运行服务使用的目标Redis，再执行数据库导入及check。原ID、权限、内容和事件顺序保留，历史持久保存；重复复制不派发任务、不再次计费。每项Redis资产原子写入，与数据库之间通过预复制和校验衔接；活动或未结算任务须先排空。

旧积分、宠物、旧 GPT 钱包、旧 `blind_box_credits` 和微信小程序明确排除，不兑换成现用钱包。迁移报告记录排除数量和原单位金额，v2备份保留原始记录。现用统一钱包以账本快照为权威，镜像冲突/负期初余额/溢出/未完成金融工作不能静默处理。`aff_quota` 保留现用可提现余额及转钱包功能。迁移79应用时固定邀请规则截止T：仅T前冻结订单保留原首购月卡刷新承诺，已有次数继续按原月度限制使用或自愿换本人套餐卡。T后新订单不发刷新，消费邀请奖励默认关闭、预算为0，须核定成本和预算后启用；奖励仅本人消费，不可转出或退款。旧现金奖励函数为空实现，不凭空补发历史奖励。每日幸运号已退役：不再发号、开奖或开放专用接口；历史奖金、账本和通知保留，仅恢复已确定但未完成的奖励，不重发。订阅重置及已有次数继续按原规则执行。

完整合成数据的迁移演练不能替代生产只读快照和源文件的验收。生产10万条日志计价对照、5000流+2000RPS独立机压测和付费上游可用性分别验证；本地模拟与单模块通过不代表这些条件已经满足。

## 正式全量切换

`.github/workflows/v3-verify.yml` 先完成后端、前端和真实隔离整栈验收，再构建 `gateway/control/worker/migrate/web` 五类镜像。`main`、`v3-rebuild` 或标签的 `push` 发布到 `ghcr.io/sh2001sh/codego-api:sha-<7位提交>-<服务>-amd64`，PR、其他分支和手动验证不推送。镜像包含完整提交 OCI 标签，CI 摘要记录每类 digest；正式切换固定同一提交并核对 digest，不使用 `latest`。旧 v2 Docker、二进制和生产部署工作流只保留明确的 legacy 手动入口，提交或打标签不会触发旧生产自动部署。该配置不自动迁移生产或执行切换。

1. 记录生产镜像、Nginx上游、数据库/文件备份及Redis持久化状态。
2. 停止v2控制面的资金写入及网关新请求，等待在途请求、预扣和异步结算排空。
3. 从最终只读源执行全量迁移，核对现用资金总和、历史事实、所有保留领域关联与文件资产。
4. 启动经正式CI构建的v3镜像，检查网关、控制面、worker和前端健康，再一次切换整套上游；不需要shadow或按账户切流。
5. 验证真实协议、资金变更、事件积压及账本对账，记录镜像和公开服务版本。

Redis故障时生产配置拒绝新消费；WAL恢复已接单结算和失败预扣撤销。每个网关使用独立持久WAL卷。

预扣回复丢失时，拒绝请求会在WAL保存零扣款撤销，恢复后释放未知冻结额并阻止延迟预扣；撤销不产生账单、使用日志或市场收益。WAL写入失败明确返回错误，生产必须保留卷并恢复重放。

切换前失败可恢复原服务。切换后已有交易时先停写、排空v3事件并核对新增资金变动，再反向迁移或恢复修复后的v3，不能用切换前备份覆盖新交易。旧源码、容器与备份在稳定验收后再退役。远程CI、生产切换和v2删除均未因本地重构自动完成。

## 基础恢复验收（套餐改造前）

保留的演示项目为`codego-v3-review`，地址http://localhost:18084。六个服务健康，最终镜像严格整栈137项及真实桌面/移动端/OIDC/恢复流程共6项通过，无跳过。52条正式迁移通过PG15/17及Atlas（含列对齐提示）；API为477操作/378路径/356模型，生成可复现。前端43项单测、160项模拟浏览器用例、类型/格式/构建通过，入口gzip126.88KB≤300KB。

后端边界、普通测试、双构建lint及全仓普通Linux race通过。真实PG/Redis验证按全仓覆盖与最终受影响整包复验汇总；商业化整包405.021s，三个专用Redis订阅资金冻结用例独立补验7.848s，无跳过。验证脚本同时传递`V3_TEST_REDIS_ADDR`和`V3_TEST_COMMERCE_REDIS_ADDR`，均指向该次独立测试Redis，不使用生产实例。

恢复功能前的本地2000流+1000RPS基线正式测量116,949请求，错误0%，网关开销p50≤0.4ms、p99≤20ms。含预热155,130条用量和500账户全部排空精确对账；冷预热未达阈值、正式测量有117次调度丢弃。恢复后新增异步请求摘要后台PG写入，本轮未重跑容量压测，独立5000流+2000RPS容量目标仍待专门环境。生产快照、10万条历史计价样本、付费外部系统及远程CI不因上述本地结果视为通过。

## 套餐改造最终本地验收（2026-10-03）

最终项目`codego-v3-subscription-v2-final`保留在http://localhost:18085，六个服务健康。最新源码构建的镜像严格整栈`164 passed`、真实桌面/移动浏览器`8 passed`，无跳过。前端单测45项、模拟浏览器182项通过，模拟运行跳过的8个真实用例已另行全部实际通过；类型、API一致性、格式与构建通过，镜像入口gzip129.02KB≤300KB。API为493操作/392路径/378模型。

后端边界、普通测试、双构建lint（均`0 issues.`）及普通Linux race通过；55条迁移通过PG15/17与Atlas validate/lint，Atlas仅有列对齐建议。真实PG17/Redis race按全仓覆盖及修复后受影响整包复验汇总，最终ledger340.351s、commerce207.522s、legacy79.571s通过；历史充值退款同时核对原订单证明与实际资金批次，错误编号拒绝且不扣其它资金。全仓首次集成的该项失败已修复，不能视为原命令一次全通过。

新套餐与余额同价，旧套餐及次数保留原规则；到期、恰好到期、报价后到期及待处理恢复中的过期转换全部拒绝。售价、存量比例、换卡及邀请预算仍须后台核定后启用。生产数据、外部真实付款、远程CI和新增代码后的独立容量压测尚未验收；没有生产切换。
