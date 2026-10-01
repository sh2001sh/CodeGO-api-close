# CodeGo v3 前端

React 19、TanStack Router / Query、Base UI、Tailwind、Rsbuild 独立应用。保留 new-api 与 QuantumNous 的项目标识，沿用铜色和纸面配色。

```sh
bun install --frozen-lockfile
bun run generate:api
bun run check:api
bun run typecheck
bun run test
bun run build
bun run test:e2e
```

开发服务监听 `127.0.0.1:3100`；API 默认转发到 `127.0.0.1:3002`，通过 `V3_CONTROL_URL` 修改。前后端的公共来源应相同；控制面的 `V3_PUBLIC_URL` 应设为浏览器访问来源。首次运行浏览器测试前执行 `bun x playwright install chromium`。

生产构建输出 `dist/`，包含按内容生成名称的资源及 `.br` / `.gz` 文件。`v3/cmd/control` 可通过 `-assets web/app/dist` 提供页面；HTML 使用重新验证缓存，带内容指纹的静态资源使用一年 immutable 缓存。入口脚本同时受 size-limit 和实际 HTML 入口脚本 gzip 总和检查约束，预算 300 KB。

控制面规范位于 `v3/api/openapi.json`，生成到 `src/lib/api.generated.ts`；`check:api` 检查生成物是否过期。生成脚本保留 int64 精度并生成请求整数路径；不要直接运行 openapi-typescript 覆盖输出。客户端使用生成的路径和操作类型，API Key / 用户 ID / 支付金额 / credits 超出 JavaScript 安全整数时保持精确字符串或 BigInt。金额按 micro credits 整数处理，支付金额遵守币种的 0、2 或 3 位小数。语言包与页面按需加载；取数使用 queryOptions 和路由 loader。

21 个页面涵盖登录、注册、个人资料与通行密钥、仪表板、API Key、钱包与订阅购买/续期/升级/燃料/额度转换/退款、订单、使用日志、渠道、用户、系统设置、拼团、盲盒、社区、渠道市场、我的渠道、市场审核、钱包转账、发票、兑换码与套餐管理。社区浏览器请求使用登录会话接口，服务间密钥不进入前端。

默认浏览器测试使用明确标为测试的 API fixture，检查桌面、移动视口及客户端失败重试流程。真实容器测试使用独立的 `real-stack.spec.ts`，不拦截 API；仅允许本地隔离验收容器的 18083 端口。先按 `v3/deploy/compose.test.yaml` 构建并启动整套容器，再运行：

```powershell
$env:V3_REAL_URL = 'http://localhost:18083'
bun run test:e2e tests/real-stack.spec.ts
Remove-Item Env:V3_REAL_URL
```

fixture 通过不能证明真实支付或数据库事务成立；后端业务由整套容器验收脚本验证。生成压缩资源和缓存头也需在构建后的容器检查。

社区 OIDC 登录会保留服务器生成的本地 `returnTo`，密码、通行密钥与 OAuth 登录后继续授权；旧 `/oauth/{provider}` 回调通过浏览器转至同源 API，只转发 `state` / `code`。绝对地址、编码后的外部域名或反斜杠返回地址会被拒绝。

`tests/auth-navigation.spec.ts` 使用模拟 API 和浏览器原生虚拟通行密钥验证前端流程。`tests/real-oidc.spec.ts` 完全使用实际 API，需要隔离控制面配置临时 OIDC 客户端及 RSA 签名密钥，并设置公开测试变量 `V3_REAL_OIDC_CLIENT_ID` / `V3_REAL_OIDC_REDIRECT_URI`。回调地址须位于同一隔离测试来源。未配置时该用例明确跳过，不能据此宣称社区 OIDC 已通过容器验收。
