# Main Goal

优化 CodeGo 分组市场页与分组状态页的前端加载速度和交互响应。

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
- 本轮仅完成本地实现与验证，未提交、未推送、未发布生产。

# Next Actions

- 若进入发布流程，按仓库现有 GitHub 自动构建与生产发布流程提交本轮及用户已有修改。

# Blockers

- 无。
