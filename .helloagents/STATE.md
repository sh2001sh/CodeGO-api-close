# Main Goal
完善 Daily Lucky Number 用户页面，使其全中文、规则易查阅、月卡倍率明确，并适配窄屏。

# Current Status
- 每日幸运号用户页已完成中文化，包含开奖概览、开奖进程、月卡号码、中奖记录和当日公开中奖名单。
- 规则已重构为默认收起的 `Collapsible` 面板；首屏“查看完整规则”按钮、规则标题栏和键盘交互均可展开/收起。
- 规则面板明确展示 Lite、Standard、Pro、Ultra 四档月卡倍率、开奖时间、匹配方式、奖励档位、累计奖池、奖励去向和历史保留规则。
- 购买、续费和升级月卡不再自动赠送盲盒；页面保留现有品牌令牌与组件体系。
- 规则表在窄屏下仅由自身滚动容器承载；顶栏搜索入口在 380px 以下收敛为图标，避免页面级横向溢出。

# Verification
- `git diff --check` 通过。
- `web/default` 执行 `bun run typecheck` 和 `bun run build` 均通过。
- 相关前端文件执行 Prettier 检查通过。
- Playwright 模拟登录态验收：390px 默认收起并可展开；320px 展开后页面宽度等于视口，倍率和匹配规则可见。
- 定向 Go 测试 `go test ./internal/commerce/app -run "Test(Lucky|DailyLucky|SubscriptionPurchaseDoesNotGrant)" -count=1` 已通过。
- 前端提交 `b547d5a1d` 已推送到 `v2-refactor-20260711`。
- 已创建并推送 tag `v2.0.0-rc.33.9-alpha.111`，GitHub 多架构 Docker 构建成功。
- 生产四服务已替换为 alpha.111；公网 `/api/status` 返回成功并报告新版本。
- 已运行账本补数：新增 6 个钱包账户、6 个 Claude 钱包账户与 1 个订阅账户；复核迁移、账户覆盖与账本快照不一致项均为 0。
- 已清理 12 个退出容器、alpha.109 及更早版本的未使用镜像、旧 smoke 卷和未使用 PostgreSQL Alpine 镜像；系统盘可用空间从约 3.2GB 提升到约 6.4GB。

# Next Step
- 观察 ledger-worker 的异步 outbox 队列和数据库磁盘余量；alpha.110 保留为上一版回退镜像。

# Blockers
- None.
