# 独立测试环境与全量切换

测试环境使用独立 PostgreSQL、Redis、网关、控制面、worker 和新前端。数据库端口不向宿主机开放，网页仅监听 `127.0.0.1`；默认端口18083，可用 `V3_TEST_PORT` 选择18084。当前本地重构尚未生产切换。

## 配置与启动

准备未入库的环境文件，至少设置 `V3_TEST_PG_PASSWORD`、`V3_SECRET_KEY`、独立 `V3_SESSION_SECRET` 和 `V3_TEST_PORT`。加密密钥使用32字节随机值的base64，会话密钥至少32字节。导入旧渠道/API Key 时，迁移工具与运行服务必须使用同一个目标加密密钥；已有环境不能覆盖该密钥。环境文件、RSA私钥及集成凭据不写进仓库或日志。

Compose 的 Redis 使用单主、AOF 和 `noeviction`，网关/worker各有 WAL 卷。网关和 worker共享 `V3_FILES_DIR=/data/files`，worker通过 `V3_INTERNAL_GATEWAY_URL=http://gateway:3001` 执行真正计费的渠道批量测试。当前文件配额/去重协调适用于单上传网关，不承诺多个上传进程共享卷的严格一致性。

可信代理地址通过 `V3_TRUSTED_PROXY_CIDRS` 配置，逗号分隔；仅这些对端的 `X-Forwarded-For` / `X-Real-IP` 用于客户端 IP 策略与鉴权失败限制。审计采样默认关闭；worker仅在明确设置 `V3_AUDIT_SAMPLE_RETENTION_DAYS`（1–3650天）时清理样本，保留期应与实际策略一致。

账户请求保护沿用 `REQUEST_ABUSE_GUARD_ENABLED`，默认关闭；迁入的限制次数、限制截止时间和封禁状态在启用后生效。使用采样由资金事务记录，worker独立处理，避免保护规则与资金锁互相等待。

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

OIDC真实验收需要在私有测试环境文件中设置 `OIDC_ISSUER`、`OIDC_CLIENT_ID`、随机至少32字符的 `OIDC_CLIENT_SECRET`、同源已注册 `OIDC_REDIRECT_URI`，以及临时RSA（至少2048位）PEM的base64值 `OIDC_SIGNING_PRIVATE_KEY_BASE64`。Compose将这些值传给control；全部留空时不启用该集成。浏览器端另设置 `V3_REAL_URL`、`V3_REAL_OIDC_CLIENT_ID`、`V3_REAL_OIDC_REDIRECT_URI` 后运行 `tests/real-oidc.spec.ts`；本地真实测试只接受18083/18084，并验证登录后的S256授权续接与state保留。

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

旧积分、宠物、旧 GPT 钱包和旧 `blind_box_credits` 明确排除，不兑换成现用钱包。迁移报告记录排除数量和原单位金额，v2备份保留原始记录。现用统一钱包以账本快照为权威，镜像冲突/负期初余额/溢出/未完成金融工作不能静默处理。`aff_quota` 保留现用可提现余额及转钱包功能，不新增邀请奖励程序。

完整合成数据的迁移演练不能替代生产只读快照和源文件的验收。生产10万条日志计价对照、5000流+2000RPS独立机压测和付费上游可用性分别验证；本地模拟与单模块通过不代表这些条件已经满足。

## 正式全量切换

1. 记录生产镜像、Nginx上游、数据库/文件备份及Redis持久化状态。
2. 停止v2控制面的资金写入及网关新请求，等待在途请求、预扣和异步结算排空。
3. 从最终只读源执行全量迁移，核对现用资金总和、历史事实、所有保留领域关联与文件资产。
4. 启动经正式CI构建的v3镜像，检查网关、控制面、worker和前端健康，再一次切换整套上游；不需要shadow或按账户切流。
5. 验证真实协议、资金变更、事件积压及账本对账，记录镜像和公开服务版本。

Redis故障时生产配置拒绝新消费；WAL恢复已接单结算和失败预扣撤销。每个网关使用独立持久WAL卷。

预扣回复丢失时，拒绝请求会在WAL保存零扣款撤销，恢复后释放未知冻结额并阻止延迟预扣；撤销不产生账单、使用日志或市场收益。WAL写入失败明确返回错误，生产必须保留卷并恢复重放。

切换前失败可恢复原服务。切换后已有交易时先停写、排空v3事件并核对新增资金变动，再反向迁移或恢复修复后的v3，不能用切换前备份覆盖新交易。旧源码、容器与备份在稳定验收后再退役。远程CI、生产切换和v2删除均未因本地重构自动完成。

## 最终本地验收

保留的演示项目为`codego-v3-review`，地址http://localhost:18084。六个服务健康，严格整栈97项及真实桌面/移动端/OIDC共4项通过，无跳过；最终数据库冻结额、积压及账本/用量差异为0。完整全仓测试和最终资金失败路径复测通过，48条正式迁移通过PG15/17及Atlas（含列对齐提示）。

最终本地2000流+1000RPS场景正式测量116,949请求，错误0%，网关开销p50≤0.4ms、p99≤20ms。含预热155,130条用量和500账户全部排空精确对账；冷预热未达阈值、正式测量有117次调度丢弃，所以独立5000流+2000RPS容量目标仍待专门环境。生产快照、10万条历史计价样本、付费外部系统及远程CI不因上述本地结果视为通过。
