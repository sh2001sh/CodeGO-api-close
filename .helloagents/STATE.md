# Main Goal
发布当前每日幸运号与盲盒页面重构、奖励结算调整和相关回归测试，创建并构建 `v2.0.0-rc.34`。

# Current Status
- 用户页已提供高可见的“查看完整规则”入口，首次进入页面自动打开规则弹窗。
- 规则弹窗已拆分为摘要、匹配流程、匹配示例、倍率表、奖励计算、奖池和结算说明，内容支持中英文翻译。
- 规则明确使用完整月卡编号最后四位，与当天全站统一四位幸运数字从右向左连续匹配，只结算最高命中档位。
- Lite、Standard、Pro、Ultra 月卡倍率分别展示，并说明倍率应用顺序、奖励去向及四位全中奖池规则。
- 购买、续费和升级月卡当前不会自动赠送盲盒；旧的规则面板已移除。
- 弹窗已增加视口高度约束和 `min-h-0`，正文独立滚动，底部关闭按钮在 390px 和 320px 下保持可用。
- 提交 `f94ba47f1` 已推送到 `v2-refactor-20260711`，annotated tag `v2.0.0-rc.34` 已推送。
- `artifacts/` 与 `scripts/audit-upstream-api.ps1` 仍为明确排除的未跟踪文件。
- 每日幸运号奖励已改为进入用户普通钱包，并保留账本幂等和到账日志；对应回归测试已同步验证钱包余额与日志。
- 规则弹窗按登录用户的 `uid` 持久化首次访问标记，只在第一次进入时自动打开；后续通过内容首屏的高可见规则入口手动打开。
- 规则入口已从页面右上角操作区移至标题下方的规则提示条，右上角仅保留刷新操作。

# Verification
- `bun run typecheck` 通过。
- `bun run build` 通过，静态主题页生成完成。
- 规则相关 TSX 和 i18n JSON 的格式、JSON 解析及 `git diff --check` 已通过。
- `go test ./internal/commerce/app -run "TestDailyLucky" -count=1 -v` 通过。
- `go vet ./internal/commerce/app` 通过。
- Playwright 模拟接口验收通过：首次自动打开、关闭后再次打开、390px/320px 视口滚动、倍率与匹配章节可见、页面无横向溢出、控制台无错误。
- 本地真实登录代理当前返回 `504`，因此浏览器交互验收使用了仅限测试上下文的接口模拟；未修改应用运行时逻辑。
- 本次目标文件类型检查、Prettier、JSON.parse 和 `git diff --check` 通过；目标 ESLint 已无重复导入错误，仅保留 React Hooks/快速刷新警告。
- 全量 commerce 包测试仍有两条既有 group-buy 断言失败，单独运行同样失败，与本次改动无交集。
- GitHub Actions Docker run `30732013165` 成功完成 amd64/arm64、7 个服务镜像、manifest 创建和 cosign 签名；Release run `30732013174` 也已成功。
- GHCR 版本根标签与 `latest` digest 为 `sha256:4bb4de47f3cd2851756b2bb746b65b8054401f64428c5537c564c488d6701d46`，根标签及 7 个服务标签均核验为 linux/amd64 + linux/arm64。
- Playwright 模拟接口验收通过：首次自动弹出、刷新后不再弹出、规则入口可重新打开、390px 下正文滚动/关闭按钮可用、桌面布局入口清晰、无横向溢出且无控制台错误。

# Next Step
- `v2.0.0-rc.34` 已完成提交、推送、Actions 构建、签名和 GHCR 多架构核验；当前等待后续需求。

# Blockers
- 无发布链路阻塞；全量 commerce 测试的两条 group-buy 既有失败已保留记录，GHCR Registry API 直连因未提供凭据返回 `UNAUTHORIZED`，但本机 Docker 凭据核验已成功。
