# 页面、路由与组件覆盖索引

## 读法

P01–P101表示设计对象，不代表101个独立新路由：店铺/分组详情为现有市场子视图，发票是钱包section，39项设置由动态section承载，P97以后为规划。下表引用专题内可点击P锚点；每页都有布局、字段、操作、异常、动效和未来验收。首页在03单独展开；80组件在05逐项展开。P20相关reset路径、历史别名和结构路由列在后表。

## 全页清单

| ID | 页面 | 路由/子视图 | 布局配方 | 组件 | 动效 | 规范 |
| --- | --- | --- | --- | --- | --- | --- |
| P01 | 首页 | `/` | P | C01 C51 C52 C53 C54 C55 | M01 M20 M21 M22 M23 M24 | [逐页正文](03-immersive-curtain-home.md#P01) |
| P02 | 模型目录 | `/pricing` | P | C09 C21 C22 C24 C25 C19 | M06 M07 | [逐页正文](06-public-pages-specification.md#P02) |
| P03 | 模型详情 | `/pricing/$modelId` | D | C23 C24 C25 C26 C27 C54 | M09 M03 | [逐页正文](06-public-pages-specification.md#P03) |
| P04 | 公开市场 | `/market` | P | C21 C22 C32 C35 C38 C19 | M07 M01 M04 | [逐页正文](06-public-pages-specification.md#P04) |
| P05 | 分组详情视图 | `/market（分组详情视图，沿用实际参数）` | D | C23 C24 C25 C38 C12 | M03 M08 | [逐页正文](06-public-pages-specification.md#P05) |
| P06 | 店铺详情视图 | `/market（店铺视图，沿用实际参数）` | D | C32 C33 C34 C35 C25 | M07 M10 M03 | [逐页正文](06-public-pages-specification.md#P06) |
| P07 | 分组与平台状态 | `/group-status` | P | C12 C27 C25 C18 | M08 | [逐页正文](06-public-pages-specification.md#P07) |
| P08 | 开发者文档 | `/docs` | R | C56 C57 C58 C54 C59 C75 | M02 M06 M05 M24 M23 | [逐页正文](07-playground-and-developer-center.md#P08) |
| P09 | 主题文章目录 | `/topics` | P | C09 C18 C58 | M06 M07 | [逐页正文](06-public-pages-specification.md#P09) |
| P10 | 主题文章详情 | `/topics/$slug` | R | C56 C58 C54 | M23 | [逐页正文](06-public-pages-specification.md#P10) |
| P11 | 接入指南 | `/guide` | R | C56 C54 C59 | M24 | [逐页正文](06-public-pages-specification.md#P11) |
| P12 | FAQ | `/faq` | R | C10 C58 C75 | M02 | [逐页正文](06-public-pages-specification.md#P12) |
| P13 | 关于与主体 | `/about` | R | C58 C25 C55 | M01 | [逐页正文](06-public-pages-specification.md#P13) |
| P14 | 客户端下载 | `/download` | P | C10 C18 C25 | M01 | [逐页正文](06-public-pages-specification.md#P14) |
| P15 | 隐私政策 | `/privacy-policy` | R | C56 C58 C25 | M23 | [逐页正文](06-public-pages-specification.md#P15) |
| P16 | 用户协议 | `/user-agreement` | R | C56 C58 C06 | M23 | [逐页正文](06-public-pages-specification.md#P16) |
| P17 | 登录 | `/sign-in` | A | C60 C14 C61 C08 | M10 M01 | [逐页正文](06-public-pages-specification.md#P17) |
| P18 | 注册 | `/sign-up` | A | C60 C14 C62 C61 | M10 | [逐页正文](06-public-pages-specification.md#P18) |
| P19 | 找回密码 | `/forgot-password` | A | C60 C14 C75 | M10 | [逐页正文](06-public-pages-specification.md#P19) |
| P20 | 重置密码 | `/reset` | A | C60 C14 C75 | M10 | [逐页正文](06-public-pages-specification.md#P20) |
| P21 | 一次性验证 | `/otp` | A | C62 C60 | M10 | [逐页正文](06-public-pages-specification.md#P21) |
| P22 | 通用OAuth入口 | `/oauth` | A | C61 C75 | M05 | [逐页正文](06-public-pages-specification.md#P22) |
| P23 | 供应商OAuth回调 | `/oauth/$provider` | A | C61 C77 C75 | M05 | [逐页正文](06-public-pages-specification.md#P23) |
| P24 | 桌面授权 | `/desktop/authorize` | A | C72 C06 C61 | M04 M08 | [逐页正文](06-public-pages-specification.md#P24) |
| P25 | 首次设置 | `/setup` | F | C71 C60 C75 | M10 | [逐页正文](06-public-pages-specification.md#P25) |
| P26 | 401错误页 | `/401` | E | C75 C08 C54 | M01 | [逐页正文](06-public-pages-specification.md#P26) |
| P27 | 403错误页 | `/403` | E | C75 C08 C54 | M01 | [逐页正文](06-public-pages-specification.md#P27) |
| P28 | 404错误页 | `/404` | E | C75 C08 C54 | M01 | [逐页正文](06-public-pages-specification.md#P28) |
| P29 | 500错误页 | `/500` | E | C75 C08 C54 | M01 | [逐页正文](06-public-pages-specification.md#P29) |
| P30 | 503错误页 | `/503` | E | C75 C08 C54 | M01 | [逐页正文](06-public-pages-specification.md#P30) |
| P31 | 受保护错误入口 | `/errors/$error` | E | C75 C02 | M05 M10 | [逐页正文](06-public-pages-specification.md#P31) |
| P32 | 总览 | `/dashboard/overview` | C | C04 C11 C28 C27 C18 C46 | M09 M07 | [逐页正文](04-console-pages-specification.md#P32) |
| P33 | 模型调用分析 | `/dashboard/models` | C | C11 C27 C28 C18 C25 | M09 M07 | [逐页正文](04-console-pages-specification.md#P33) |
| P34 | 用户消费分析 | `/dashboard/users` | T | C18 C27 C72 C73 | M06 M07 | [逐页正文](04-console-pages-specification.md#P34) |
| P35 | API密钥 | `/keys` | T | C39 C40 C41 C05 C06 C16 | M03 M08 M01 | [逐页正文](04-console-pages-specification.md#P35) |
| P36 | Playground 原生多会话 | `/playground` | W | C42 C43 C44 C45 C46 C77 C05 C06 C70 | M11 M05 M03 M10 | [逐页正文](07-playground-and-developer-center.md#P36) |
| P37 | 外部聊天预设 | `/chat/$chatId` | C | C61 C75 C76 | M05 | [逐页正文](04-console-pages-specification.md#P37) |
| P38 | 客户端链接生成 | `/chat2link` | F | C10 C39 C54 C06 | M10 | [逐页正文](04-console-pages-specification.md#P38) |
| P39 | 图像任务 | `/images` | D | C69 C70 C46 C75 | M15 M08 M05 | [逐页正文](04-console-pages-specification.md#P39) |
| P40 | 调用日志 | `/usage-logs/common` | T | C18 C17 C05 C46 C25 | M06 M07 M03 | [逐页正文](04-console-pages-specification.md#P40) |
| P41 | 异步任务日志 | `/usage-logs/task` | T | C70 C18 C05 C46 | M08 | [逐页正文](04-console-pages-specification.md#P41) |
| P42 | 钱包 | `/wallet` | B | C47 C48 C49 C50 C15 C64 C65 C67 C06 | M12 M04 M07 | [逐页正文](09-billing-wallet-and-subscriptions.md#P42) |
| P43 | 额度包 | `/packages` | P | C66 C24 C25 C49 C50 | M02 M04 M12 M07 | [逐页正文](09-billing-wallet-and-subscriptions.md#P43) |
| P44 | 订阅与持有权益 | `/subscriptions` | C | C66 C67 C68 C46 C50 | M07 M09 M12 M04 | [逐页正文](09-billing-wallet-and-subscriptions.md#P44) |
| P45 | 发票与退款 | `/wallet?section=invoices（/invoices别名）` | F | C64 C65 C13 C15 C50 C72 | M10 M04 M12 | [逐页正文](09-billing-wallet-and-subscriptions.md#P45) |
| P46 | 个人资料与安全 | `/profile` | F | C60 C61 C63 C77 C06 | M10 M04 | [逐页正文](04-console-pages-specification.md#P46) |
| P47 | 邀请权益 | `/invite-rewards` | C | C54 C28 C18 C25 | M01 M07 M08 | [逐页正文](04-console-pages-specification.md#P47) |
| P48 | 保留盲盒功能 | `/blind-box` | B | C06 C50 C66 C25 | M12 | [逐页正文](04-console-pages-specification.md#P48) |
| P49 | 渠道主工作台 | `/channel-workbench` | C | C35 C36 C38 C32 C25 C50 C71 C72 | M03 M13 M14 M12 M05 | [逐页正文](08-channel-workbench-and-admin.md#P49) |
| P50 | 管理员渠道 | `/channels` | T | C18 C35 C36 C37 C38 C73 C72 | M13 M08 M07 M04 | [逐页正文](08-channel-workbench-and-admin.md#P50) |
| P51 | 市场治理 | `/marketplace` | T | C32 C34 C25 C72 C73 C06 | M03 M04 M08 | [逐页正文](08-channel-workbench-and-admin.md#P51) |
| P52 | 模型元数据 | `/models/metadata` | T | C23 C25 C26 C71 C69 C72 | M14 M07 M04 | [逐页正文](08-channel-workbench-and-admin.md#P52) |
| P53 | 模型部署 | `/models/deployments` | C | C70 C36 C18 C71 C72 | M15 M08 M04 | [逐页正文](08-channel-workbench-and-admin.md#P53) |
| P54 | 用户管理 | `/users` | T | C18 C72 C41 C63 C50 C73 | M03 M04 M12 M14 | [逐页正文](08-channel-workbench-and-admin.md#P54) |
| P55 | 兑换码管理 | `/redemption-codes` | T | C18 C15 C54 C73 C06 | M04 M08 M12 M05 | [逐页正文](08-channel-workbench-and-admin.md#P55) |
| P56 | 运营管理 | `/operations` | C | C70 C71 C72 C73 C06 | M14 M04 M15 M07 | [逐页正文](08-channel-workbench-and-admin.md#P56) |
| P57 | 安全审计 | `/security-audit` | T | C18 C17 C25 C72 C54 C05 | M06 M07 M03 | [逐页正文](08-channel-workbench-and-admin.md#P57) |
| P58 | 站点与品牌 | `/system-settings/site/system-info` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M05 M10 M14 | [逐页正文](08-channel-workbench-and-admin.md#P58) |
| P59 | 系统通知 | `/system-settings/site/notice` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M05 M04 M08 M14 | [逐页正文](08-channel-workbench-and-admin.md#P59) |
| P60 | 顶部导航 | `/system-settings/site/header-navigation` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M14 | [逐页正文](08-channel-workbench-and-admin.md#P60) |
| P61 | 侧栏模块 | `/system-settings/site/sidebar-modules` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M02 M04 M14 | [逐页正文](08-channel-workbench-and-admin.md#P61) |
| P62 | 基础认证 | `/system-settings/auth/basic-auth` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M10 M08 M14 | [逐页正文](08-channel-workbench-and-admin.md#P62) |
| P63 | OAuth认证 | `/system-settings/auth/oauth` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M02 M08 M04 M14 | [逐页正文](08-channel-workbench-and-admin.md#P63) |
| P64 | 通行密钥 | `/system-settings/auth/passkey` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M08 M14 | [逐页正文](08-channel-workbench-and-admin.md#P64) |
| P65 | 机器人防护 | `/system-settings/auth/bot-protection` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M08 M10 M14 | [逐页正文](08-channel-workbench-and-admin.md#P65) |
| P66 | 自定义OAuth | `/system-settings/auth/custom-oauth` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M03 M13 M14 | [逐页正文](08-channel-workbench-and-admin.md#P66) |
| P67 | 额度 | `/system-settings/billing/quota` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M12 M14 | [逐页正文](08-channel-workbench-and-admin.md#P67) |
| P68 | 货币 | `/system-settings/billing/currency` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M05 M14 | [逐页正文](08-channel-workbench-and-admin.md#P68) |
| P69 | 模型定价 | `/system-settings/billing/model-pricing` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M07 M12 M04 M14 | [逐页正文](08-channel-workbench-and-admin.md#P69) |
| P70 | 分组定价 | `/system-settings/billing/group-pricing` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M05 M10 M14 | [逐页正文](08-channel-workbench-and-admin.md#P70) |
| P71 | 首购规则 | `/system-settings/billing/first-purchase` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M12 M14 M04 | [逐页正文](08-channel-workbench-and-admin.md#P71) |
| P72 | 支付配置 | `/system-settings/billing/payment` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M13 M12 M04 M14 | [逐页正文](08-channel-workbench-and-admin.md#P72) |
| P73 | 签到规则 | `/system-settings/billing/checkin` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M08 M12 M14 | [逐页正文](08-channel-workbench-and-admin.md#P73) |
| P74 | 盲盒规则 | `/system-settings/billing/blind-box` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M14 M04 | [逐页正文](08-channel-workbench-and-admin.md#P74) |
| P75 | 全局模型策略 | `/system-settings/models/global` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M14 M07 M10 | [逐页正文](08-channel-workbench-and-admin.md#P75) |
| P76 | Gemini配置 | `/system-settings/models/gemini` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M02 M14 | [逐页正文](08-channel-workbench-and-admin.md#P76) |
| P77 | Claude配置 | `/system-settings/models/claude` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M10 M14 | [逐页正文](08-channel-workbench-and-admin.md#P77) |
| P78 | Grok配置 | `/system-settings/models/grok` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M14 M07 M13 | [逐页正文](08-channel-workbench-and-admin.md#P78) |
| P79 | 渠道亲和策略 | `/system-settings/models/channel-affinity` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M14 M05 | [逐页正文](08-channel-workbench-and-admin.md#P79) |
| P80 | 部署策略 | `/system-settings/models/model-deployment` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M14 M08 | [逐页正文](08-channel-workbench-and-admin.md#P80) |
| P81 | 限流 | `/system-settings/security/rate-limit` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M10 M14 | [逐页正文](08-channel-workbench-and-admin.md#P81) |
| P82 | 敏感词 | `/system-settings/security/sensitive-words` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M07 M08 M04 M14 | [逐页正文](08-channel-workbench-and-admin.md#P82) |
| P83 | SSRF防护 | `/system-settings/security/ssrf` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M10 M04 M14 | [逐页正文](08-channel-workbench-and-admin.md#P83) |
| P84 | 控制台内容 | `/system-settings/content/dashboard` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M02 M14 | [逐页正文](08-channel-workbench-and-admin.md#P84) |
| P85 | 公告 | `/system-settings/content/announcements` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M03 M04 M08 M14 | [逐页正文](08-channel-workbench-and-admin.md#P85) |
| P86 | API信息 | `/system-settings/content/api-info` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M24 M14 | [逐页正文](08-channel-workbench-and-admin.md#P86) |
| P87 | 常见问题 | `/system-settings/content/faq` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M02 M14 | [逐页正文](08-channel-workbench-and-admin.md#P87) |
| P88 | 状态集成 | `/system-settings/content/uptime-kuma` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M13 M08 M14 | [逐页正文](08-channel-workbench-and-admin.md#P88) |
| P89 | 聊天预设 | `/system-settings/content/chat` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M03 M05 M14 | [逐页正文](08-channel-workbench-and-admin.md#P89) |
| P90 | 行为策略 | `/system-settings/operations/behavior` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M10 M14 | [逐页正文](08-channel-workbench-and-admin.md#P90) |
| P91 | 监控 | `/system-settings/operations/monitoring` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M13 M14 M09 | [逐页正文](08-channel-workbench-and-admin.md#P91) |
| P92 | 邮件服务 | `/system-settings/operations/email` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M13 M04 M14 | [逐页正文](08-channel-workbench-and-admin.md#P92) |
| P93 | 后台Worker | `/system-settings/operations/worker` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M08 M15 M14 | [逐页正文](08-channel-workbench-and-admin.md#P93) |
| P94 | 日志 | `/system-settings/operations/logs` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M14 M04 | [逐页正文](08-channel-workbench-and-admin.md#P94) |
| P95 | 性能 | `/system-settings/operations/performance` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M02 M14 M08 | [逐页正文](08-channel-workbench-and-admin.md#P95) |
| P96 | 更新检查 | `/system-settings/operations/update-checker` | S | C71 C13 C14 C16 C25 C72 C06 C07 | M13 M05 M14 | [逐页正文](08-channel-workbench-and-admin.md#P96) |
| P97 | 模型榜单（规划） | `/rankings` | P | C29 C27 C25 C18 C31 | M06 M07 M09 | [逐页正文](10-ranking-comparison-and-trust.md#P97) |
| P98 | 渠道与店铺榜单（规划） | `/rankings/channels` | P | C29 C33 C34 C35 C25 | M07 M03 M04 | [逐页正文](10-ranking-comparison-and-trust.md#P98) |
| P99 | 模型对比（规划） | `/compare` | P | C23 C24 C25 C26 C30 C31 C43 | M07 M09 M03 | [逐页正文](10-ranking-comparison-and-trust.md#P99) |
| P100 | 收藏与通知（规划） | `/favorites` | T | C79 C68 C18 C71 | M08 M14 M07 | [逐页正文](10-ranking-comparison-and-trust.md#P100) |
| P101 | 团队工作空间（规划） | `/workspaces` | C | C80 C72 C77 C39 C41 C50 | M05 M04 M12 M14 | [逐页正文](10-ranking-comparison-and-trust.md#P101) |

## 入口别名与结构路由

| 入口 | 目标/职责 | 保护 |
| --- | --- | --- |
| /brand | / | 保留品牌别名，不另造首页 |
| /status | /group-status | 公开状态入口 |
| /console/log | /usage-logs | 再由已注册日志默认项解析 |
| /console/topup | /wallet | 保留原有search，不丢show_history |
| /invoices | /wallet?section=invoices | 源码已确认replace:true |
| /daily-lucky-number | /wallet | 幸运号退休，无新增参与 |
| /group-buy | /wallet | 拼团退休，无新增加入 |
| /dashboard | 已注册dashboard默认section | 由实际registry决定，不硬猜 |
| /usage-logs | 已注册logs默认section | common/task独立记录 |
| /models | 已注册models默认section | metadata/deployments管理员权限 |
| /system-settings | 已注册分类/section默认项 | 不重复计为页面 |
| /system-settings/{category} | 对应section-registry默认项 | 39项见P58–P96，保留非法section错误分支 |
| /user/reset | 按当前reset关联入口 | 与P20密码重置同一任务，不能另设错误语义 |
| __root、(auth)/route、_authenticated/route、settings route | 页面外壳与守卫 | 结构路由，无独立内容页 |

## 组件动效覆盖

每项的尺寸/输入/行为/全部状态见05；本表让实现者直接找到motion token。列表中的“静态”仍是明确行为，不能为了动起来改动金额/身份。

| ID | 组件 | 使用范围 | 动效与状态 |
| --- | --- | --- | --- |
| C01 | [公开导航](05-component-library-guide.md#C01) | P01–P16 | M01/M02；菜单opacity .6→1、y -8→0，180ms；退出120ms |
| C02 | [控制台外壳](05-component-library-guide.md#C02) | P32–P96 | M02/M05；折叠布局即时更新，仅图标180ms淡入 |
| C03 | [全局命令台](05-component-library-guide.md#C03) | 所有登录页 | M04弹层280/180ms；搜索M06，取消旧结果 |
| C04 | [页头工具条](05-component-library-guide.md#C04) | 所有数据页 | M01；标题与动作静态，避免漂移 |
| C05 | [详情抽屉](05-component-library-guide.md#C05) | 模型、Key、日志、渠道 | M03；x24→0和opacity.6→1，280ms；退出180ms |
| C06 | [确认对话框](05-component-library-guide.md#C06) | 撤销、退款、删除、授权 | M04；scale.98→1与opacity.6→1，280ms，退出180ms |
| C07 | [结果通知](05-component-library-guide.md#C07) | 复制、保存等局部动作 | M16；y8→0、opacity0→1，180ms；退出120ms |
| C08 | [按钮与文字链接](05-component-library-guide.md#C08) | 全站 | M01；press180ms，释放120ms；焦点即时 |
| C09 | [搜索框](05-component-library-guide.md#C09) | 模型、市场、日志、文档 | M06；180ms防抖，Abort旧请求；控件位置静态 |
| C10 | [选择器与组合框](05-component-library-guide.md#C10) | 分组、模型、语言、设置 | M02菜单180/120ms；选中反馈M01 |
| C11 | [分段选择](05-component-library-guide.md#C11) | 时间窗、视图 | M01；指示transform180ms，内容M07 |
| C12 | [状态标识](05-component-library-guide.md#C12) | 全站业务状态 | M08；文字交叉淡入180ms，不循环呼吸 |
| C13 | [字段分组](05-component-library-guide.md#C13) | 全部表单 | M10；错误opacity0→1，180ms；不抖动输入 |
| C14 | [文本与密码输入](05-component-library-guide.md#C14) | 认证、命名、设置 | M01/M10；聚焦即时轮廓，文字静态 |
| C15 | [金额与数量输入](05-component-library-guide.md#C15) | 预算、充值、权重 | M10错误180ms；数字不滚动计数 |
| C16 | [状态开关](05-component-library-guide.md#C16) | Key、渠道、设置 | M08；拇指translateX0→20px，180ms；失败同程还原 |
| C17 | [日期与时间范围](05-component-library-guide.md#C17) | 日志、排名、趋势 | M02选择面板180/120ms；应用M06，日期静态 |
| C18 | [数据表与移动记录](05-component-library-guide.md#C18) | 所有列表 | M07；整体opacity.7→1，180ms；不动画行高或排序飞行 |
| C19 | [分页与分批](05-component-library-guide.md#C19) | 列表与记录 | M07替换180ms；焦点留在分页控件 |
| C20 | [骨架与加载状态](05-component-library-guide.md#C20) | 所有异步数据区 | M05；opacity180ms，无高亮扫光；减少动效全静态 |
| C21 | [筛选横轨](05-component-library-guide.md#C21) | 模型、市场、移动列表 | M01/M06；不自动无限滚动 |
| C22 | [模型记录](05-component-library-guide.md#C22) | P01 P02 P04 | M22筛选180ms；hover动作箭头x0→4px，180ms |
| C23 | [模型详情体](05-component-library-guide.md#C23) | P03 P05 首页 | M03外壳；事实字段静态，换组M08交叉淡入 |
| C24 | [价格矩阵](05-component-library-guide.md#C24) | 模型、对比、市场 | M08更新180ms，禁止金额从0滚动 |
| C25 | [来源与证据](05-component-library-guide.md#C25) | 能力、隐私、榜单 | M03证据280/180ms；标记静态 |
| C26 | [能力矩阵](05-component-library-guide.md#C26) | 模型详情与对比 | 内容静态；新增对比列M07淡入，不缩放文字 |
| C27 | [趋势与分位图](05-component-library-guide.md#C27) | 性能、概览、榜单 | M09；整体交叉淡入180ms，无从零生长；tooltip即时 |
| C28 | [指标条](05-component-library-guide.md#C28) | 概览、钱包、渠道 | 值静态替换；M01仅钻取动作 |
| C29 | [榜单记录](05-component-library-guide.md#C29) | P97 P98 | M07整体180ms；无排名数字翻牌或自动爬榜 |
| C30 | [价格延迟散点](05-component-library-guide.md#C30) | P99 | M09点集opacity.7→1，180ms；不插值虚构点 |
| C31 | [模型对比托盘](05-component-library-guide.md#C31) | P02 P99 | M01选中180ms；布局即时更新，不覆盖内容 |
| C32 | [店铺头与货架](05-component-library-guide.md#C32) | P04 P06 | M07货架更新180ms；头部身份静态 |
| C33 | [合格评价](05-component-library-guide.md#C33) | P06 | M10提交180ms；星级即时选中，不喷粒子 |
| C34 | [举报表单](05-component-library-guide.md#C34) | P06 渠道详情 | M03外壳；M15上传；M10受理反馈 |
| C35 | [渠道记录与详情](05-component-library-guide.md#C35) | P04 P49 P50 | M13探测；M08状态180ms；每行稳定，禁止随机数字刷新 |
| C36 | [探测任务](05-component-library-guide.md#C36) | P49 P50 | M13；运行标识可旋转180ms状态反馈，减少动效静态；不写假百分比 |
| C37 | [调度权重](05-component-library-guide.md#C37) | P49 P50 | M01滑块位置跟手不加滞后；提交M10，失败180ms |
| C38 | [路由池成员](05-component-library-guide.md#C38) | P05 P49 管理 | M08选择180ms；无拖拽强制要求；成员重排静态 |
| C39 | [Key记录](05-component-library-guide.md#C39) | P35 | M08状态180ms；复制M01；敏感值静态 |
| C40 | [Key创建表单](05-component-library-guide.md#C40) | P35 | M03外壳；M10提交反馈；秘密内容不动画 |
| C41 | [额度与预算](05-component-library-guide.md#C41) | Key、钱包 | 进度以transform scaleX180ms；文字即时静态 |
| C42 | [会话列表](05-component-library-guide.md#C42) | P36 | M07搜索180ms；新会话M05；删除M04 |
| C43 | [消息与流式输出](05-component-library-guide.md#C43) | P36 | M11；内容逐帧批量写入，无逐字淡入；用户上滚停自动跟随 |
| C44 | [消息输入](05-component-library-guide.md#C44) | P36 | M01按压；输入框高度按内容自然变化无height动画 |
| C45 | [系统词与参数](05-component-library-guide.md#C45) | P36 | M03参数抽屉；M10应用180ms；值静态 |
| C46 | [每轮用量与费用](05-component-library-guide.md#C46) | 日志、会话、钱包 | M08结算状态180ms；金额绝不从0滚动 |
| C47 | [余额分层](05-component-library-guide.md#C47) | P42 | 静态数字，M01钻取反馈 |
| C48 | [充值金额](05-component-library-guide.md#C48) | P42 | M01选中180ms；价格静态；M10校验 |
| C49 | [支付方式与订单状态](05-component-library-guide.md#C49) | P42 | M12；等待静态状态，返回不先显示成功，到账180ms反馈 |
| C50 | [资金流水](05-component-library-guide.md#C50) | P42 P48 | M07列表180ms；账务正文静态 |
| C51 | [首页品牌字形](05-component-library-guide.md#C51) | P01 | M20；opacity.8→1/y12→0，280ms，单次 |
| C52 | [路由物理场](05-component-library-guide.md#C52) | P01 | M21；位移≤16px，mass1/k180/d26，400ms收敛 |
| C53 | [首页模型探索](05-component-library-guide.md#C53) | P01 | M22；结果180ms淡入，最多首6项40ms错峰 |
| C54 | [接入代码与复制](05-component-library-guide.md#C54) | P01 P08 P11 | M24内容180ms；复制成功状态2秒恢复，失败可重复 |
| C55 | [品牌收尾与页脚](05-component-library-guide.md#C55) | 公开页 | 正文静态；M23锚点滚动；无无限跑马灯 |
| C56 | [文档导航与目录](05-component-library-guide.md#C56) | P08 P10 P11 | M02折叠180/120ms；M23目录；正文静态 |
| C57 | [文档检索](05-component-library-guide.md#C57) | P08 阅读入口 | M06 180ms防抖；结果M07，不动画整篇正文 |
| C58 | [文章与政策正文](05-component-library-guide.md#C58) | 公开阅读页 | 静态；阅读不使用隐藏进入、视差或缩放 |
| C59 | [语言选择](05-component-library-guide.md#C59) | 产品与文档 | M01选择180ms，文字即时替换，布局自然重排 |
| C60 | [账号表单](05-component-library-guide.md#C60) | P17–P21 P46 | M10错误180ms，无表单左右摇动 |
| C61 | [OAuth与绑定](05-component-library-guide.md#C61) | P17 P18 P22 P23 | M05状态180ms；无伪造认证成功过渡 |
| C62 | [OTP与验证码](05-component-library-guide.md#C62) | P21 注册 | M10错误180ms；焦点即时无跳跃动画 |
| C63 | [安全会话与Passkey](05-component-library-guide.md#C63) | P46 管理 | M04确认；M08行状态180ms，成功再移除 |
| C64 | [发票与主体](05-component-library-guide.md#C64) | P45 钱包 | M12生成状态；文档版式静态，不改财务字段 |
| C65 | [退款审核](05-component-library-guide.md#C65) | 钱包、用户管理 | M04确认；M12真实状态；金额静态 |
| C66 | [套餐权益](05-component-library-guide.md#C66) | P43 P44 | M01选择；M12提交；禁止收益翻牌动画 |
| C67 | [旧权益转换](05-component-library-guide.md#C67) | P44 | M10试算180ms；M04确认；不动画扣减数字 |
| C68 | [通知订阅](05-component-library-guide.md#C68) | P100 个人 | M08状态180ms；失败回原值，不假称订阅成功 |
| C69 | [上传与附件](05-component-library-guide.md#C69) | P39 举报 | M15真实进度；无扫描线；减少动效静态百分比 |
| C70 | [异步任务](05-component-library-guide.md#C70) | P39 P41 | M08状态180ms；不假进度；离屏减少轮询 |
| C71 | [设置分组](05-component-library-guide.md#C71) | P58–P96 | M14保存180ms；错误静态；不对表单做级联飞入 |
| C72 | [角色与权限](05-component-library-guide.md#C72) | 管理员、授权页 | M04确认；权限清单静态，结果M08 |
| C73 | [批量操作](05-component-library-guide.md#C73) | 日志、渠道、用户 | M08反馈180ms；选中条不覆盖表格；无飞入遮挡 |
| C74 | [空状态](05-component-library-guide.md#C74) | 列表、会话 | 静态，M01动作反馈；无装饰循环 |
| C75 | [错误与恢复](05-component-library-guide.md#C75) | 全站 | 错误即时可读；M10仅重试等待，不抖动 |
| C76 | [外部客户端容器](05-component-library-guide.md#C76) | P37 | M05容器180ms；iframe内容不干预 |
| C77 | [当前账号作用域](05-component-library-guide.md#C77) | 个人、对话、权限 | 静态；不以转场延迟清理敏感数据 |
| C78 | [行操作菜单](05-component-library-guide.md#C78) | 所有数据行 | M02菜单180/120ms；不移动原行 |
| C79 | [收藏与关注](05-component-library-guide.md#C79) | P100 模型店铺 | M08状态180ms；无心形粒子或自动弹窗 |
| C80 | [团队空间与成本中心](05-component-library-guide.md#C80) | P101 | M04授权确认；M08状态；预算数字静态 |

## 布局配方图例

P公开12列；D主/侧详情；R文档三栏；A认证单列；F表单与关联区；C控制台；T表格；W会话三栏；B账务主/摘要；S设置导航与表单；E错误页。配方给外壳，各页layoutDetail规定其内部区块、移动顺序和字段；不能只照字母套通用卡片。

## 覆盖与真实性边界

本表是依据本地路由和feature注册器编制的设计索引，未声称线上路由已部署或工程验收通过。P97–P101及P36原生多会话的新增部分需实施；第三方聊天预设仍为外部iframe。各别名只复用目标，不复活旧活动。首页demo只实现首页范围，进入正文链接读取设计规格，不伪造尚未实现的业务页面。
