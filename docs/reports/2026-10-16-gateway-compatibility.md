# V2 网关与 Codex 兼容性检查

检查以生产网关 2a543be 为基线，范围为 Responses WebSocket、Chat / Responses 双向桥接及中断用量处理。

## 报错原因

网关只接受 response.create 和 response.append，因此在本地拒绝了 response.interrupt；原处理循环还会在生成期间阻塞消息读取。仅增加事件白名单无法停止正在进行的请求。

修复将客户端读取与单轮执行分开，统一加锁发送消息。response.interrupt / response.cancel 取消当前轮；错误的 response_id 不会取消其他轮。连接断开也会取消当前轮。被取消的输出不进入会话缓存，后续请求仍可执行。队列限制为 16 个请求、总计 32 MiB。

原生上游 WebSocket 在取消时关闭该轮连接，解除静默读取及管道等待，下一轮重新建连。无故断开的请求仍禁止自动重放，防止重复生成或扣费。

## 修复项

| 范围 | 原行为 | 修复行为 |
| --- | --- | --- |
| Chat → Responses 图片 | 丢弃 detail | 保留图片精度 |
| Chat → Responses 文件 | 错误嵌套 file 对象 | 使用顶层 filename / file_data / file_id |
| 工具模式 | 省略 strict 导致两种 API 默认语义变化 | 保留 Chat 默认非严格模式，显式 strict 仍生效 |
| 输出上限 | 取两个限制的较大值 | 优先 max_completion_tokens；反向使用现代参数名 |
| 工具流 | 稀疏工具索引可能遗漏；提前生成错误 call_id | 按真实索引排序收尾，等待真实 call_id |
| 自定义工具 | 分片 JSON 包装泄露到原始输入 | 增量解码转义、Unicode 代理对，正确处理空字符串 |
| 流生命周期 | 多段消息 / 推理复用 ID | 各输出项使用独立 ID 和独立内容 |
| 拒答 | refusal 被丢弃 | 保留非流式拒答和流式拒答事件 |
| 截断 | incomplete 原因字段错误；过滤结果误报完成 | 使用 incomplete_details.reason；保留长度 / 内容过滤原因 |
| 用量 | 标准 output_tokens_details 未解析 | 保留推理 Token 等输出明细 |
| 混合输出 | 文本后工具调用被跳过 | 文本与工具调用均返回 |
| 断流 | 桥接可能伪造正常完成 | 缺少终止事件 / finish_reason 时返回错误 |
| 图片工具回传 | 图片结果转为文本，模型无法看图 | 工具文本保留原位置；图片 / 文件在全部并行结果后进入用户消息 |
| 上游状态 | 顶层 error 或取消终态遗漏 | 正确终止读取，错误不会变成成功 |
| 不可转换能力 | 内置服务工具被静默删除 | 明确要求原生 Responses 上游；file_url 也明确拒绝桥接 |
| 请求元数据 | 反向丢失缓存保留期 / 安全标识 | 保留对应字段 |

中断沿用已有账务机制：上游精确用量可用时保留；缺少最终用量时按既有输入估算与已收到输出处理。关闭 HTTP / WebSocket 是本地停止手段，无法证明所有上游都会立即停止其内部计算。本次没有更改价格、倍率、余额或历史账单。

## 边界

- 官方 WebSocket 指南当前描述 stream_id 多路复用。本补丁维持单连接串行轮次；多路并发、独立流缓存尚未实现，不能宣称完整覆盖该能力。
- 官方指南没有建立 response.interrupt 的正式协议语义。本补丁将其作为网关兼容控制事件，与 response.cancel 同样处理，并发送 response.cancelled 收尾。
- Chat 上游无法生成 Responses 的加密推理状态、执行服务端内置工具或代替真正的持久会话。需要这些能力的请求应路由到原生 Responses 上游。
- previous_response_id / conversation / background 的不可转换状态继续明确拒绝；现有原生 compact、后台响应及轮询路径保留。
- 测试采用本地可控上游，没有向付费模型发起验证调用，也没有修改生产账务数据。

## 验证

新增测试覆盖活动中断、空闲中断、错误目标 ID、断线取消、连接复用、缓存隔离、原生静默上游、工具索引、分片转义、文件与图片、并行工具结果、拒答、截断、顶层错误及用量字段。

本地测试：translation、responsesws、transport/http、providers、providers/openai、billing/app 均通过；gateway-api 编译通过。中断和断线场景连续运行 30 次通过。发布工作流额外执行 Linux race 检查与真实前端构建。

## 依据

- https://developers.openai.com/codex/config-reference ：Codex 提供方使用 Responses 协议，WebSocket 支持单独声明。
- https://developers.openai.com/api/docs/guides/websocket-mode ：创建事件、连接缓存与 stream_id。
- https://developers.openai.com/api/docs/guides/function-calling ：严格模式默认值与自定义工具。
- https://developers.openai.com/api/docs/guides/pdf-files ：两类 API 的文件字段及能力差异。
- https://developers.openai.com/api/docs/guides/streaming-responses ：响应流生命周期与拒答事件。
- 本地 Codex 参考 1715e55；sub2api 参考 bdb42e2；CLIProxyAPI 参考 d198db5。参考其会话、工具映射、图片回传及严格模式处理，未引入项目依赖。
