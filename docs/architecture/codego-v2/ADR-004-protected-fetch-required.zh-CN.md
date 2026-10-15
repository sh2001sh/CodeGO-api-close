# ADR-004 用户可控 URL 一律走受保护抓取

## 状态

Accepted

## 背景

凡是用户可控 URL，如果直接用普通 HTTP client 拉取，都存在 SSRF、内网探测、回环访问、metadata service 访问和 DNS rebinding 风险。

## 决策

CodeGo v2 中所有用户可控 URL 拉取行为必须统一走 protected fetch 组件。默认拒绝：

- loopback
- private range
- link-local
- multicast
- reserved range

并且要求：

- DNS 解析后 IP 校验
- 重定向逐跳校验
- 端口白名单或黑名单
- 大小、时长、协议限制

## 后果

正面：

- SSRF 风险显著下降
- 安全策略集中管理

负面：

- 某些历史“能访问”的地址会被阻断
- 需要为可信网络场景提供显式例外机制

## 落地约束

- download、media proxy、callback fetch、webhook helper 都必须接入
- 例外策略必须配置化并可审计
