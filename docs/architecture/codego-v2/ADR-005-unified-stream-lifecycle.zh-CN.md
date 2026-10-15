# ADR-005 流式响应统一生命周期与断连停写

## 状态

Accepted

## 背景

当前和历史兼容链路中，不同 provider 的流式处理方式分散，容易出现客户端断开后后端仍继续拉流、继续写出、继续计费、goroutine 泄漏的问题。

## 决策

CodeGo v2 的所有流式请求必须收敛到统一 stream lifecycle：

- 统一请求上下文取消
- 统一 writer 封装
- 统一 footer / usage / done / error 事件语义
- 统一断连检测
- 非 OpenAI provider 同样必须支持断连停写

## 后果

正面：

- 降低无效 token 消耗
- 避免资源泄漏
- 提高对齐度和排错效率

负面：

- 初期需要逐 provider 适配

## 落地约束

- provider adapter 不得直接绕过统一 writer
- disconnect test suite 必须覆盖 OpenAI 与非 OpenAI provider
