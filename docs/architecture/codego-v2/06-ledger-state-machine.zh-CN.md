# CodeGo v2 账本状态机设计

## 1. 文档目的

本文档定义 CodeGo v2 账本系统中的核心状态机与幂等语义。目的是让 reservation、settlement、refund、adjustment 的行为成为显式设计，而不是 scattered 逻辑。

## 2. 核心对象

- `BillingAccount`
- `Reservation`
- `Settlement`
- `LedgerEntry`
- `Adjustment`

## 3. Reservation 状态机

### 3.1 状态

- `open`
- `settled`
- `released`
- `expired`

### 3.2 创建条件

创建前必须满足：

- account 可用
- available balance 足够
- idempotency key 未冲突

### 3.3 状态转移

- `open -> settled`
- `open -> released`
- `open -> expired`

禁止：

- `settled -> open`
- `released -> settled`
- `expired -> settled`

### 3.4 幂等语义

若以相同 `idempotency_key` 重复创建 reservation：

- 若 payload 相同，返回原 reservation
- 若 payload 不同，返回冲突错误

## 4. Settlement 状态机

### 4.1 状态

- `pending`
- `completed`
- `rejected`

### 4.2 创建条件

- reservation 必须为 `open`
- settlement 必须有 reference
- settlement idempotency key 未冲突

### 4.3 转移

- `pending -> completed`
- `pending -> rejected`

完成时副作用：

- 写 debit/credit ledger entry
- 更新 balance snapshot
- 把 reservation 标记为 `settled`

### 4.4 Delta 语义

`delta_amount = actual_amount - reserved_amount`

可能情况：

- `delta = 0`
- `delta > 0` 补扣
- `delta < 0` 退回差额

## 5. Refund 状态机

Refund 本身建议不建复杂独立状态表，优先以 ledger entry + refund command 幂等实现。

如果需要实体表，可定义：

- `pending`
- `completed`
- `rejected`

退款要求：

- 必须基于 reference 定位
- 必须可判断是否已经退款
- 必须保留 reason code

## 6. Adjustment 状态机

适用于管理员调账或系统修正。

状态：

- `requested`
- `approved`
- `applied`
- `rejected`
- `canceled`

生产环境建议把高金额 adjustment 加审批流。

## 7. LedgerEntry 语义

### 7.1 entry_type

- `reserve_hold`
- `reserve_release`
- `settle_debit`
- `settle_credit`
- `refund_credit`
- `adjust_debit`
- `adjust_credit`
- `grant_credit`
- `reset_credit`
- `expire_debit`

### 7.2 direction

- `debit`
- `credit`

### 7.3 reference_type

- `request`
- `task_workflow`
- `order`
- `subscription_reset`
- `admin_adjustment`

## 8. 一致性规则

### 8.1 规则 1

每个成功 reservation 必须最终对应以下之一：

- 一个 completed settlement
- 一个 release
- 一个 expire

### 8.2 规则 2

每个 settlement 只能关联一个 reservation。

### 8.3 规则 3

每个 refund 必须能定位到唯一业务 reference。

### 8.4 规则 4

余额快照必须可由 ledger entries 重建。

## 9. 失败场景设计

### 9.1 provider 成功但 settlement 失败

处理策略：

- 保留 usage evidence
- 将 settlement 置为 `pending`
- workflow 进入 `needs_compensation_check`
- 重试 settlement

### 9.2 refund 执行失败

处理策略：

- workflow 保持 `compensation_pending`
- refund job 重试

### 9.3 snapshot 更新失败

处理策略：

- ledger entry 已经写成事实
- balance snapshot 异步重建

## 10. 账本状态机的接口对应

- `CreateReservation`
- `SettleReservation`
- `ReleaseReservation`
- `ExpireReservation`
- `RefundByReference`
- `AdjustAccount`

## 11. 审计要求

所有以下动作必须审计：

- 管理员调账
- 订阅重置
- 大额 refund
- settlement mismatch
- reservation expired

## 12. 测试要求

至少包含：

- reservation 幂等测试
- settlement 幂等测试
- delta 正负场景测试
- refund 重复调用测试
- balance snapshot 重建测试

## 13. 总结

账本状态机是 CodeGo v2 的一致性核心。只要该状态机被严格执行，绝大部分 quota、退款、补扣类问题都会从“散点 bug”转化为“状态流转错误”，从而更容易被验证和治理。
