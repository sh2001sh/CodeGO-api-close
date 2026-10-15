# CodeGo v2 第四层实施包总览

## 1. 文档目的

本文档说明第四层实施包的定位、组成和使用方式。第四层不再是“解释方案”，而是“帮助团队直接开工”的交付层。

## 2. 产物组成

### 2.1 接口契约产物

- `10-openapi-control-plane.yaml`
- `11-openapi-gateway-internal.yaml`
- `12-internal-events.proto`

### 2.2 工作流骨架产物

- `13-temporal-skeleton.zh-CN.md`

### 2.3 研发任务产物

- `14-implementation-backlog.zh-CN.md`

### 2.4 迁移产物

- `15-sql-migrations-initial.sql`

### 2.5 架构决策产物

- `16-adr-index.zh-CN.md`
- `ADR-001-modular-monolith-first.zh-CN.md`
- `ADR-002-ledger-as-source-of-truth.zh-CN.md`
- `ADR-003-temporal-for-durable-workflows.zh-CN.md`
- `ADR-004-protected-fetch-required.zh-CN.md`
- `ADR-005-unified-stream-lifecycle.zh-CN.md`

## 3. 使用顺序

建议团队使用顺序如下：

1. 先阅读 `17-fourth-layer-overview`
2. 再阅读 `16-adr-index` 与 5 份 ADR
3. 后按角色读取：
   - 后端 API：`10`、`11`
   - 账本/数据库：`15`
   - 工作流：`13`
   - 项目经理/Tech Lead：`14`

## 4. 第四层的价值

第四层的意义不是继续增加文档数量，而是把前三层中的抽象设计压缩为可编码的边界、可迁移的数据结构、可排期的任务和不可随意漂移的决策约束。

## 5. 与后续工作的边界

第四层之后，下一步不应再先写“第五层理论文档”，而应直接进入：

- 仓库目录重组
- migration 文件正式入库
- workflow worker 骨架代码创建
- OpenAPI 转换为 DTO / handler contract
- feature flag 和灰度切换实现
