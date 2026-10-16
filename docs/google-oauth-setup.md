# Google 登录配置

代码复用自定义 OAuth，支持 Google 登录、首次注册和个人中心主动绑定。无需新增用户字段或单独迁移表结构。已有账号的原数字 ID 不变；不会仅因邮箱相同而合并账号。

## 1. 确定网站地址

在网站后台「系统设置 → 站点设置」填写公开的服务器地址（`ServerAddress`），例如 `https://your-domain.example`，必须与用户实际访问网站的协议、域名及端口一致。生产环境使用 HTTPS。

Google 预设的回调地址是：

```text
https://your-domain.example/oauth/google
```

这里的域名是占位符。不要填 `/api/oauth/google`，不要添加末尾斜杠或 `?bind=true`。后台新增或编辑 OAuth 时会显示基于服务器地址和标识生成的准确回调地址。

## 2. 创建 Google Cloud 客户端

1. 打开 [Google Cloud Console](https://console.cloud.google.com/)，创建或选择项目，进入 **Google Auth Platform**。
2. 在 **Branding（品牌）**填写应用名称、支持邮箱、网站主页、隐私政策、服务条款及联系邮箱。按控制台要求添加并验证网站授权域名。
3. 在 **Audience（受众群体）**选择 **External（外部）**，供普通 Google 账号登录。Internal 仅限相应 Google Workspace 组织成员。
4. 在 **Data Access（数据访问）**仅选择基本身份权限：`openid`、`email`、`profile`。不需要 Gmail、Drive、离线访问或刷新令牌。
5. 在 **Clients（客户端）**创建 OAuth 客户端，应用类型选择 **Web application（Web 应用）**。
6. **Authorized JavaScript origins** 填网站源地址，如 `https://your-domain.example`（无路径）。**Authorized redirect URIs** 填完整的 `https://your-domain.example/oauth/google`。多个域名分别配置，且网站登录时使用与后台服务器地址一致的域名。
7. 保存 **Client ID** 和 **Client Secret**。密钥只填网站后台，不发送到聊天、不提交到 Git。

测试期间按 Audience 页要求添加测试用户。使用纯基本身份权限时，Google 对测试受众限制可能有例外；以控制台显示为准。正式开放前在 Audience 发布应用，并完成控制台要求的品牌或域名验证。无需启用付费 Google API。

## 3. 网站后台启用

进入「系统设置 → 身份认证 → 自定义 OAuth」，新增提供商并选择 **Google** 预设。官方地址、权限、用户字段、认证方式和邮箱验证策略会自动填写。

| 配置 | 值 |
|---|---|
| 名称 / 标识 | `Google` / `google` |
| Client ID / Client Secret | 上一步获得的凭据 |
| 授权地址 | `https://accounts.google.com/o/oauth2/v2/auth` |
| 令牌地址 | `https://oauth2.googleapis.com/token` |
| 用户信息地址 | `https://openidconnect.googleapis.com/v1/userinfo` |
| 权限 | `openid profile email` |
| 用户 ID / 显示名 / 邮箱 | `sub` / `name` / `email` |
| 用户名字段 | 留空，由网站生成本地用户名 |
| 认证方式 | Params (in body) |
| 访问策略 | 要求 `email_verified` 等于布尔值 `true` |

开启并保存。编辑时 Client Secret 留空表示保留原密钥。以后轮换密钥时填入新密钥并保存。不要删除重建已使用的提供商或修改 `sub` 映射，否则现有绑定无法对应原账号。

若允许首次 Google 登录自动注册，网站还需开启「允许新用户注册」；已有绑定账号登录不受关闭注册影响。邮箱域名等现有注册限制仍然生效。

## 4. 验收和排错

- 未登录：登录页出现 Google 按钮，授权后进入网站；再次登录应进入同一账号，数字 ID 相同。
- 已有账号：先以原方式登录，进入「个人中心 → 账号绑定」绑定 Google，随后用 Google 登录应回到该账号。直接在登录页使用尚未绑定的 Google 会创建独立账号，即使邮箱相同也不会自动合并。
- 取消授权应回到登录页；从个人中心发起绑定后取消则回到个人中心。未验证邮箱和无效状态值应被拒绝。
- `redirect_uri_mismatch`：核对 Google 客户端回调、后台服务器地址及浏览器域名，尤其是 HTTP/HTTPS、端口、www 和末尾斜杠。
- `invalid_client` / 令牌交换失败：核对客户端类型、ID、密钥和服务器出网能力。
- `access_denied`：核对受众、测试用户、Workspace 限制和用户是否取消授权。
- 服务器必须能访问 Google 的令牌及用户信息地址；浏览器必须能访问 Google 授权页。反向代理不得重写回调路径或丢失会话 Cookie。

代码验证使用模拟 Google 响应，覆盖正确授权参数、长数字 `sub`、已验证邮箱、注册/重复登录/绑定及错误响应。真实 Google 授权需创建客户端并配置凭据后验收。

参考：[Google OpenID Connect](https://developers.google.com/identity/openid-connect/openid-connect)、[Web Server OAuth](https://developers.google.com/identity/protocols/oauth2/web-server)、[官方发现配置](https://accounts.google.com/.well-known/openid-configuration)。
