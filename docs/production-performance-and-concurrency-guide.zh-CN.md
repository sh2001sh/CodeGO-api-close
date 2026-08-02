# Code Go 生产性能与高并发优化操作手册

本文面向当前 Code Go 部署：公网域名 `shu26.cfd`，Nginx 作为入口，控制、网关、账本和工作流为独立 Docker 容器，Redis 用于共享状态和限流。

目标是提升静态页面访问性能、提高 API 并发承载能力、隔离异常流量，并保持现有用户的 API 配置可用。

## 1. 目标架构

推荐逐步演进到以下结构：

```text
浏览器、SDK、桌面客户端
  |
  +-- shu26.cfd -- Cloudflare CDN/WAF -- Nginx -- control-api
                                      |
                                      +-- /v1/* -- gateway-1
                                                   -- gateway-2
                                                   -- gateway-N
                                                         |
                                                         +-- Redis
                                                         +-- PostgreSQL / PgBouncer
                                                         +-- 上游模型渠道
```

保留兼容入口：

| 用途 | 地址 | 缓存策略 |
| --- | --- | --- |
| 网站、控制台与 API | `https://shu26.cfd` | CDN 仅缓存静态资源，HTML 与 API 不缓存 |
| 所有用户 API Base URL | `https://shu26.cfd/v1` | 永久保持不变，Nginx 反代到网关池 |

不要修改或重定向 `/v1/*`。API SDK、桌面客户端、SSE 和 WebSocket 可能不正确处理 301/302；所有现有和新用户都继续使用 `https://shu26.cfd/v1`。

## 2. 实施前准备

### 2.1 建立基线

在优化前记录以下数据，优化后才能判断是否有效：

- 网站首页和登录页的 TTFB、总加载时间、静态资源大小。
- `/v1/*` 的 QPS、活跃流数量、p95/p99 延迟、429 和 5xx 比例。
- 上游渠道的 429、503、超时比例和单渠道并发数。
- gateway、control、ledger、workflow 的 CPU、内存、重启次数和文件句柄数。
- Redis 延迟、内存占用、连接数和命中率。
- PostgreSQL 连接数、慢查询、锁等待、磁盘与 CPU。
- 服务器磁盘使用率、Docker 日志体积、网络入/出带宽。

建议初始告警阈值：CPU 70%、内存 75%、磁盘 80%、Redis 延迟 10ms、数据库连接 70%、5xx 1%、429 5%。阈值应根据压测结果调整。

### 2.2 发布和回滚纪律

每次变更遵守以下顺序：

1. 在本地执行定向测试和 `git diff --check`。
2. 创建递增 tag，例如 `v2.0.0-rc.33.9-alpha.18`，等待 amd64、arm64 构建和 manifest 签名完成。
3. 先拉取新镜像，再替换容器；不要先停止生产容器再等待镜像下载。
4. 保存旧容器 inspect、环境变量和镜像 tag。
5. 替换后检查容器运行状态、重启次数、启动日志和公网版本。
6. 出现异常时立即恢复旧容器，不要在故障状态继续修改数据库或渠道配置。

当前发布方式已经保留了容器 inspect 和回滚容器，应继续沿用。

## 3. 第一阶段：CDN 和静态资源优化

这一阶段对模型 API 风险最低，优先实施。

### 3.1 Cloudflare 域名配置

1. 在 Cloudflare 添加根域名对应的站点。
2. 将域名注册商的 Nameserver 改为 Cloudflare 分配的 Nameserver。
3. 添加 DNS 记录：

```text
A     shu26.cfd       <源站公网 IP>   Proxied
CNAME www             shu26.cfd       Proxied
```

4. Cloudflare SSL/TLS 使用 `Full (strict)`。
5. 源站安装有效证书；不要使用 Flexible TLS，避免 HTTPS 回源降级和重定向循环。
6. 在 Cloudflare 开启 HTTP/3、Brotli、自动压缩和基础 WAF 防护。

### 3.2 缓存规则

创建以下 Cache Rules，优先级从高到低：

1. `/v1/*`、`/api/*`、`/oauth/*`、`/signin*`、`/sign-in*`、`/dashboard*`、支付回调路径：`Bypass cache`。
2. `*/assets/*`、带内容哈希的 `.js`、`.css`、字体、图片和应用下载文件：`Cache eligible`，浏览器和边缘缓存 TTL 设为 1 年。
3. HTML 页面：`Bypass cache` 或短 TTL，不要使用全站 Cache Everything。

不能缓存以下响应：

- 含 `Authorization`、Cookie、用户余额、订阅、订单、管理数据的响应。
- `/v1/*` 模型转发、SSE、WebSocket、文件上传和回调接口。
- OAuth 的 state、callback 和登录完成后的跳转响应。

### 3.3 源站 Nginx 静态资源策略

在站点 Nginx 配置中为构建后的静态资源增加长期缓存。路径须按实际前端构建目录调整：

```nginx
location ^~ /assets/ {
    try_files $uri =404;
    expires 365d;
    add_header Cache-Control "public, max-age=31536000, immutable" always;
    access_log off;
}

location ~* \.(?:js|css|woff2|png|jpg|jpeg|webp|svg|ico)$ {
    expires 365d;
    add_header Cache-Control "public, max-age=31536000, immutable" always;
}

location = /index.html {
    add_header Cache-Control "no-cache" always;
}
```

前端资源文件名必须包含内容哈希。发布新版本后，旧资源可继续被 CDN 缓存，新 HTML 会引用新的资源文件，不会出现用户拿到旧 JS 的问题。

### 3.4 源站防火墙

网站域名开启 Cloudflare 代理后，源站仍可被公网 IP 直接访问。应限制 80/443 仅接受 Cloudflare 官方 IP 段的访问；这样网站和保持不变的 `/v1/*` API 都通过 Cloudflare 回源。

变更防火墙前必须保留 SSH 管理端口，并确认 Cloudflare IP 段来自官方文档。先在维护窗口验证，再收紧规则，避免将自身锁在服务器外。

## 4. 第二阶段：保持 Base URL 不变的 Nginx 反代

### 4.1 固定 Base URL

所有控制台、文档、桌面客户端和第三方工具统一使用：

```text
https://shu26.cfd/v1
```

不新增 API Base URL，不要求历史用户修改配置，不为 `/v1/*` 配置跳转。OAuth 回调、网站登录和支付回调继续使用 `https://shu26.cfd`。

### 4.2 Nginx 网关池

将 API 请求反代至多个 gateway。下面端口仅为示例，必须与实际容器端口一致：

```nginx
upstream codego_gateway_pool {
    least_conn;
    keepalive 128;
    server 127.0.0.1:3001 max_fails=3 fail_timeout=20s;
    server 127.0.0.1:3002 max_fails=3 fail_timeout=20s;
}

map $http_upgrade $connection_upgrade {
    default upgrade;
    '' close;
}

server {
    server_name shu26.cfd;

    location ^~ /v1/ {
        proxy_pass http://codego_gateway_pool;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;

        proxy_buffering off;
        proxy_request_buffering off;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }
}
```

流式 API 需要 `proxy_buffering off`，否则用户会等待 Nginx 缓冲完整响应才收到内容。`proxy_read_timeout` 应覆盖正常的长任务，但业务层仍需限制单用户并发和总请求时长。

### 4.3 真实客户端 IP

当网站通过 Cloudflare 访问时，Nginx 必须正确使用 Cloudflare 传递的客户端 IP；否则所有用户会被识别为同一 CDN IP，导致 IP 限流、审计与风控失效。

仅信任 Cloudflare 官方 IP 段，并配置：

```nginx
real_ip_header CF-Connecting-IP;
real_ip_recursive on;
# 为每一个 Cloudflare 官方 IP 段配置 set_real_ip_from。
```

不要盲目信任任意客户端发来的 `X-Forwarded-For` 或 `CF-Connecting-IP`。

## 5. 第三阶段：gateway 横向扩容

### 5.1 扩容原则

gateway 是模型转发高并发入口，应优先扩容。control、ledger、workflow 先保留单副本，只有监控显示它们成为瓶颈时再扩。

扩容前确认以下状态均不依赖容器内存：

- 限流计数和用户/设备识别在 Redis。
- 渠道路由亲和、模型冷却和会话共享状态在 Redis 或数据库。
- 临时文件和任务状态不写入单个 gateway 容器本地磁盘。
- 审计、账本和工作流可接受异步写入。

### 5.2 单机双 gateway

当前容器使用 host 网络时，两个 gateway 不能监听同一端口。为第二个 gateway 分配新端口，例如：

```text
gateway-1: GATEWAY_PORT=3001
gateway-2: GATEWAY_PORT=3002
```

创建第二个容器前：

1. 从现有 gateway 导出环境变量配置。
2. 仅修改容器名、`NODE_NAME` 与 `GATEWAY_PORT`。
3. 使用相同版本镜像、SQL DSN、Redis 连接和日志轮转参数。
4. 启动后确认其能连接 Redis、PostgreSQL 和工作流服务。
5. 在 Nginx upstream 中加入新端口后执行 `nginx -t`，再 reload Nginx。
6. 观察 30 分钟的 5xx、429、流式中断和 Redis 延迟。

不要直接在当前 host 网络模式下复制同端口容器。更长期的方案是切换到 Docker bridge 网络或独立节点，以固定内部 DNS 名称和端口。

### 5.3 多机扩容

当单机 CPU、内存或公网带宽接近告警线时，增加第二台 gateway 节点：

1. 新节点部署相同 gateway 镜像和只读配置。
2. 两台节点连接同一个 Redis、PostgreSQL 和工作流后端。
3. 负载均衡 upstream 中配置两台私网地址。
4. 使用健康检查自动摘除不可用节点。
5. 为 API 使用连接耗尽、超时和断线重试的压测验证。

账本、数据库迁移和管理任务不要在所有节点重复执行。它们需要单实例或分布式锁。

## 6. 第四阶段：数据库、Redis 和日志

### 6.1 PostgreSQL 与 PgBouncer

在 PostgreSQL 前部署 PgBouncer，应用连接 PgBouncer 而非直接连接 PostgreSQL：

```ini
[databases]
codego = host=<postgres-host> port=5432 dbname=<database-name>

[pgbouncer]
listen_addr = 127.0.0.1
listen_port = 6432
pool_mode = transaction
max_client_conn = 1000
default_pool_size = 30
reserve_pool_size = 10
```

操作要点：

1. 使用新的连接地址先在一个非核心 worker 验证。
2. 检查应用是否使用依赖 session 的临时表、`SET LOCAL` 之外的 session 状态；存在时不要直接使用 transaction pooling。
3. 分批切换 control、gateway、ledger、workflow。
4. 持续观察数据库连接数、等待事件、慢查询和账本一致性。

### 6.2 索引和慢查询

重点检查高频表：`logs`、`users`、`tokens`、`channels`、`abilities`、账本与订阅相关表。

每次新增索引前先执行 `EXPLAIN (ANALYZE, BUFFERS)`，确认真实查询计划。生产创建大型索引使用 `CREATE INDEX CONCURRENTLY`，避免长时间锁表。

日志列表必须按时间、用户、token、模型和 request id 的实际查询方式建立组合索引。不要为了“可能有用”无限增加索引，因为写入日志也会变慢。

### 6.3 Redis

Redis 用于限流、会话、亲和和冷却状态。应设置：

- 明确的 `maxmemory` 和适合缓存数据的淘汰策略。
- 连接池上限和连接超时，避免高峰创建大量短连接。
- Redis 连接失败时的明确失败策略；不得静默放开限流。
- Redis 延迟、内存、命令速率和被驱逐 key 的监控告警。

### 6.4 日志治理

保留现有 Docker `json-file` 轮转策略，并继续限制 journald 容量。

用户可见错误日志只能保存脱敏文本；管理员诊断日志可保留原始上游错误，但应设置保留期、访问控制和导出审计。高峰期的请求审计写入必须异步，避免日志表成为 API 延迟瓶颈。

## 7. 第五阶段：限流、并发和上游保护

### 7.1 限流层级

建议按以下层次独立配置：

| 场景 | 限流依据 | 目标 |
| --- | --- | --- |
| 未认证请求 | IP | 防扫描、爆破和 DDoS |
| 网页/API 用户 | 用户 ID | 防单账号压垮系统 |
| 桌面客户端 | 设备 ID | 避免共享网络误伤 |
| API 调用 | Token/Key | 防泄露 Key 滥用 |
| 登录/OAuth/验证码 | IP + 账号 | 防撞库和状态滥用 |
| 模型转发 | 用户 + Key + 模型 | 控制流式并发和成本 |

现有按用户/设备隔离的 Redis 限流应继续保留。不要恢复为全局 IP 限流，否则公网、办公网和 CDN 用户会互相影响。

### 7.2 流式并发上限

除 QPS 外，为每个用户和 token 设置活动流数量上限。例如先从每用户 5 条、每 token 10 条开始，实际数值按套餐、上游并发和服务器容量调整。

达到上限时返回明确的 429，不要允许排队无限增长。长队列会占用连接、内存和 goroutine，最终导致所有用户变慢。

### 7.3 上游渠道保护

保持以下策略：

- 对渠道的单个模型执行短时冷却，不因一个模型错误禁用整条渠道。
- 只有存在备用路由时才自动切换，避免无意义重试。
- 429、503、余额不足、模型不存在和超时分别统计。
- 限制单一渠道、单一模型和单一上游 Key 的并发。
- 用户侧统一返回通用模型不可用提示；管理员日志保留原始诊断信息。

对上游的重试必须设置最大次数、指数退避和总超时。不要在高峰期对失败渠道无限重试。

## 8. 第六阶段：监控、压测和容量规划

### 8.1 最小监控面板

至少建立以下图表：

- 网关请求数、活跃请求、活跃 SSE 流、p50/p95/p99 延迟。
- HTTP 2xx、4xx、429、5xx 分布。
- 每模型、每渠道的请求数、成功率、超时和冷却次数。
- Redis 命中率、内存、连接数、命令耗时。
- PostgreSQL 连接数、TPS、慢查询、锁等待。
- 容器 CPU、内存、网络、文件句柄、重启数。
- 磁盘、Docker 镜像和日志目录增长速度。

### 8.2 压测顺序

不要直接使用真实用户 Key 或高价模型进行压测。

1. 静态页面压测：验证 CDN 命中率和源站带宽下降。
2. 登录、状态、模型列表等轻量 API：验证限流和数据库连接池。
3. 使用低成本测试模型进行短响应压测：验证 QPS。
4. 使用受控并发的 SSE 压测：验证活跃流上限、Nginx 超时和断线恢复。
5. 模拟单渠道 429/503：验证模型级冷却、备用路由和用户错误脱敏。

每轮只改变一个变量，并记录最大稳定并发、p95/p99、错误率、CPU、内存和数据库连接。以“错误率低于 1%、p95 可接受、无队列持续增长”为扩容标准，而不是只看瞬时 QPS。

## 9. 验收清单

- [ ] `shu26.cfd` 静态资源由 CDN 缓存，HTML 和用户数据不缓存。
- [ ] 所有用户继续使用 `https://shu26.cfd/v1`，不需要修改 Base URL。
- [ ] API 没有 301/302 跳转，SSE 和 WebSocket 可稳定工作。
- [ ] Nginx 正确传递真实客户端 IP、流式响应和超时配置。
- [ ] 至少两个 gateway 实例通过负载均衡提供服务。
- [ ] Redis 和 PostgreSQL 在峰值下没有连接耗尽或持续慢查询。
- [ ] 限流按用户、设备、Key 和 IP 分层，不存在全站误伤。
- [ ] 上游渠道异常只冷却对应模型，并向用户返回脱敏通用错误。
- [ ] 监控、告警、发布备份和回滚演练已经完成。

## 10. 推荐实施顺序

1. Cloudflare 静态加速与 WAF。
2. 在同一域名下为 `/v1/*` 配置缓存绕过、流式反代和真实 IP。
3. Nginx 流式优化和真实 IP 配置。
4. 单机双 gateway 与 Nginx 负载均衡。
5. PgBouncer、慢查询治理和 Redis 容量配置。
6. 监控与压测。
7. 根据数据决定是否增加第二台 gateway 节点。

每完成一个阶段都应压测、观察并保留回滚点，再进入下一阶段。
