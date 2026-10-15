# CodeGo v2 代码仓迁移任务分解

## 1. 文档目的

本文档把目录重组方案进一步拆成任务级清单，方便进入实际项目管理和排期。

## 2. 迁移策略

- 先建新目录
- 再迁高价值逻辑
- 再做兼容桥接
- 最后删除旧实现

## 3. 批次 1：基础骨架

### 任务 1

创建目录：

- `cmd/control-api`
- `cmd/gateway-api`
- `cmd/workflow-worker`
- `cmd/ledger-worker`

### 任务 2

创建目录：

- `internal/platform/bootstrap`
- `internal/platform/config`
- `internal/platform/db`
- `internal/platform/security`
- `internal/platform/observability`

### 任务 3

建立 shared error model、request id、trace id 基础包。

## 4. 批次 2：账本模块

### 任务 4

创建：

- `internal/billing/domain`
- `internal/billing/app`
- `internal/billing/infra/postgres`

### 任务 5

迁移或新建：

- account entity
- reservation entity
- settlement entity
- ledger entry entity

### 任务 6

实现：

- reservation service
- settlement service
- refund service

## 5. 批次 3：网关执行模块

### 任务 7

创建：

- `internal/gateway/routing`
- `internal/gateway/execution`
- `internal/gateway/stream`

### 任务 8

把现有 stream helper 迁入统一 stream 模块。

### 任务 9

把 provider adaptor contract 抽象出来。

## 6. 批次 4：任务工作流模块

### 任务 10

创建：

- `internal/workflow/domain`
- `internal/workflow/app`
- `internal/workflow/temporal`

### 任务 11

迁移：

- task snapshot
- task terminal state handling
- timeout handling

## 7. 批次 5：商业模块

### 任务 12

创建：

- `internal/commerce/domain`
- `internal/commerce/app`
- `internal/commerce/payment`
- `internal/commerce/subscription`

### 任务 13

迁移：

- 订单创建
- 支付回调
- 权益发放

## 8. 批次 6：身份模块

### 任务 14

创建：

- `internal/identity/domain`
- `internal/identity/app`
- `internal/identity/transport/http`

### 任务 15

迁移：

- 登录注册
- OAuth
- Passkey
- Desktop auth

## 9. 批次 7：审计和读模型

### 任务 16

创建：

- `internal/audit/domain`
- `internal/audit/app`
- `internal/audit/projection`

### 任务 17

迁移：

- usage logs
- admin action logs
- metrics projection

## 10. 文件级高优先级迁移源

建议优先梳理这些现有文件族：

- `service/billing_*`
- `service/task_*`
- `relay/relay_task.go`
- `relay/helper/*stream*`
- `relay/channel/*`
- `controller/subscription*`
- `controller/topup*`
- `controller/user*`
- `model/task.go`
- `model/subscription.go`

## 11. 完成定义

一个迁移批次完成，要求：

- 新目录有真实实现
- 老代码仅作桥接或已删除
- 测试通过
- 文档更新

## 12. 总结

本任务清单的意义是让目录重构从“抽象想法”变成“可拉 issue 的具体工作包”。
