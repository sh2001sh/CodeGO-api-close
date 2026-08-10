# Main Goal
使用 `chen-006/gpt56_api_detector` 验证 `test_api.txt` 中 OpenAI-compatible API 的 GPT-5.6 模型真实性与线路质量。

# Current Status
- 检测器固定在 v4.0.1 / commit `e9ef5d0`，位于系统临时目录。
- `/v1/models` 声明 `gpt-5.6-sol` 与 `gpt-5.6-terra`。
- 两个模型均完成官方低档检测，各 14 个逻辑请求；未启用请求/响应留存，报告确认未持久化认证信息。
- `gpt-5.6-sol` 判定为“Juice混用”：11 个 Juice 样本全部不匹配 Sol，高档 8/8 命中 Terra 指纹 32，低档 3/3 返回 12。
- `gpt-5.6-terra` 判定为“通过”：11/11 Juice 样本匹配，输出完整性 2/2、提示覆盖 1/1。

# Verification
- Sol：14/14 成功，0 网络错误，0 重试；总耗时平均 3715ms、中位数 3525ms，首事件平均 2872ms、中位数 2667ms。
- Terra：14/14 成功，0 网络错误，0 重试；总耗时平均 2839ms、中位数约 2559ms，首事件平均 2263ms、中位数约 1928ms。
- 本地检测服务已关闭；临时 SQLite 报告保留在 `%TEMP%/gpt56_api_detector_runs_20260809/`。

# Key Context
- `test_api.txt` 位于工作区根目录，包含敏感 API Key；不得输出或提交。
- 低档未启用概率探针、Native Codex 格式或 32K 长上下文，因此“Terra 通过”仅覆盖本轮确定性短上下文测试。
- 未继续运行 64/202 请求的中高档，避免接近 150 RPM 时产生额外费用和限流干扰。

# Next Actions
- 优先将 `gpt-5.6-sol` 从对外模型列表移除或修正为 Terra 别名，避免误售/误路由。
- 若需更强证据，可在低流量时段运行 Terra 中档，并把并发/RPM影响纳入测试安排。

# Blockers
- 无。
