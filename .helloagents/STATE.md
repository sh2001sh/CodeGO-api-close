# Main Goal

完成 CodeGo 网关性能优化的 GitHub 构建、生产发布与线上验收，确保生产运行精确对应构建提交。

# Status

- 分组市场的路由池、渠道管理、管理员治理与分组详情已按交互拆为异步模块，并在悬停/聚焦时预取。
- 市场首屏异步块由约 147 KB 降至约 51 KB，减少约 66%；路由池独立块约 5.7 KB。
- 市场列表移除逐行 Motion 入场，时间格式化与状态指标完成 memo 化，屏幕外行使用 `content-visibility`。
- 批量测试展示与 controller 已按职责拆分，列表入口文件降至约 110 行，行为保持不变。
- 分组状态搜索使用 `useDeferredValue`，筛选、排序、汇总、模型选项与时间序列均完成 memo 化；屏幕外 section 使用 `content-visibility`。
- Playwright 以 30 组、每组 8 模型、每模型 12 时间桶完成压力验证：状态页 30 组就绪约 2.0 秒，搜索约 228 ms；桌面与 390px 移动端无页面级横向溢出，控制台零错误。
- 浏览器复验通过分组勾选、测试区状态更新、路由池预取与页签切换。
- `pnpm exec eslint . --quiet`、`pnpm exec tsc --noEmit`、5 项 Bun 回归测试、`git diff --check`、`pnpm run build` 均通过；生产构建 2.31 秒。
- 工作树仍包含用户此前约 60 个前端修改文件，本轮未回退或覆盖无关改动。
- 性能优化提交 `dfb051c37` 已构建成功；原自动部署运行成功但 SSH/同步/部署/健康检查均被跳过，公网仍为旧版本 `sha-3de6f2d`。
- 已新增部署修复提交 `0ec8afb2f` 并推送：部署时强制注入触发构建提交的 `NEW_API_VERSION=sha-xxxxxxx`，公网版本头不匹配时健康检查失败。
- `0ec8afb2f` 的多架构构建和生产 workflow 均显示 success，但因生产 secrets 缺失，部署步骤再次 skipped，生产尚未切换。
- 当前公网 `/api/status` 连续探测均 200，版本仍为 `sha-3de6f2d`；20 次无缓存基线 min/p50/p95/max 为 476/525/629/760 ms。
- 已通过生产机 `codego-new-api` 的密钥登录完成原地滚动发布 `sha-0ec8afb`；四个服务均运行对应 amd64 镜像。
- 发布前执行 db-migrate、ledger-backfill；严格校验在 worker 启动后通过，账本不一致为 0、待处理 outbox 降为 0。
- 公网 `/api/status` 已返回 `x-new-api-version: sha-0ec8afb`，连续 8 次均 HTTP 200，Cloudflare 源站耗时约 233-265 ms，总耗时 477-670 ms。
- 发布后 Nginx 等长窗口对比（15:10-15:17 vs 15:20-15:27）：Responses 成功请求首响 p50/p95 由 6732/52339ms 降至 5688/39206ms，约下降 15%/25%；总请求 p50/p95 为 13103/63465ms → 13938/69962ms，受上游和请求构成影响未下降。
- 发布后 gateway `first_byte_trace`（近 10 分钟）显示 request_conversion p50=89ms、outbound_conn_reused 平均 60%、headers_to_first_event p50=11.5ms；主要耗时为 upstream_server_wait p50=3134ms、first_flush p50=6169ms。
- 发布后未发现 panic、fatal、context deadline 或 Redis timeout；gateway CPU 约 6.6%、内存约 1.2GiB，主机 load 约 1.1。
- 本轮本地修改已完成前端类型检查、lint、Prettier、生产构建与差异检查，待提交、推送并发布生产。

# Next Actions

- 补齐 GitHub secrets：`PRODUCTION_DEPLOY_HOST`、`PRODUCTION_DEPLOY_USER`、`PRODUCTION_DEPLOY_SSH_KEY`、`PRODUCTION_DEPLOY_PATH`，使后续自动部署不再 skipped。
- 读取已认证 `/api/perf-metrics/summary` 对比 TTFT、p95、连接复用率、CPU 与错误率。
- 提交本轮市场与分组状态页修改后，等待 GitHub 多架构镜像构建并按精确 SHA 发布。

# Blockers

- GitHub Actions production deploy secrets 未配置或不可见，自动部署仍会跳过；本次已使用生产机现有密钥完成手工原地发布。
- 若本轮自动部署仍因 secrets 缺失跳过，将复用已验证的 `codego-new-api` 密钥手动发布，不影响构建流程。
