# CodeGo v2 ADR 索引

## 1. 文档目的

本目录用于固化 CodeGo v2 的关键架构决策。第四层实施包不应只有接口和迁移，还必须把关键决策写成 ADR，避免后续开发回退到旧思路。

## 2. ADR 列表

- `ADR-001-modular-monolith-first.zh-CN.md`
  - 先采用模块化单体，再按边界拆服务
- `ADR-002-ledger-as-source-of-truth.zh-CN.md`
  - 账本作为计费真相源
- `ADR-003-temporal-for-durable-workflows.zh-CN.md`
  - Temporal 作为长事务与异步副作用编排器
- `ADR-004-protected-fetch-required.zh-CN.md`
  - 用户可控 URL 一律走 SSRF 防护 fetch
- `ADR-005-unified-stream-lifecycle.zh-CN.md`
  - 流式响应统一生命周期和断连停写

## 3. 使用规则

- 任何偏离这些决策的实现，必须新增或修订 ADR
- PR 若涉及账本、工作流、网关流式链路、外部 URL 拉取，必须引用对应 ADR
