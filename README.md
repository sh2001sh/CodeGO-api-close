# CodeGO API · V2

当前维护版本为 V2，代码基线为生产版本 `2a543bef367e16e79450ae0a549a6d10e2994cac`。后续修复在 `main` 上进行。

## 开发与验证

需要 Go 1.26、Bun 及 Node.js。先构建网页，后编译后端（控制服务会嵌入网页资源）：

```sh
cd web/default
bun install --frozen-lockfile
bun run build
cd ../..
go build -p 1 ./cmd/...
go test -p 1 ./internal/billing/app ./internal/gateway/execution/providers/openai
```

后端入口位于 `cmd/`；前端位于 `web/default/`。V2 架构文档见 [docs/architecture/codego-v2](docs/architecture/codego-v2)。本地配置使用 `.env.example`，真实服务器凭据与数据库文件存放在仓库之外。

## 镜像与生产

`main` 推送后由 GitHub Actions 构建 V2 镜像。生产部署工作流只接受手动触发，代码推送和镜像构建不会自动切流。生产仍使用已验证的各服务固定镜像；恢复代码无需迁移数据库。

镜像仓库：`ghcr.io/sh2001sh/codego-api`。V2 运行配置见 [docker-compose.yml](docker-compose.yml)。

## V3 归档

V3 已停止维护，已提交代码保留在 [`archive/v3-before-v2-20261015`](https://github.com/sh2001sh/CodeGO-api-close/tree/archive/v3-before-v2-20261015)。V3 源码与构建流程已从 `main` 移除。

临时日志、浏览器会话、测试快照和本地运维文件不纳入源码。分析报告整理在 [docs/reports](docs/reports)。

## 许可

保留上游版权与 [LICENSE](LICENSE)、[NOTICE](NOTICE)、[THIRD-PARTY-LICENSES.md](THIRD-PARTY-LICENSES.md)。
