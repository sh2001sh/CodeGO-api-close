# CodeGo 全站重设计交付索引

本轮交付：完整设计方案、每个页面布局/状态/动效、80项组件契约，以及可交互首页demo。全部文件位于当前Open Design项目，原E盘仓库保持只读。

## 打开方式

- [首页demo](../../index.html)：双击或通过本地静态服务打开；无构建步骤、无付费API调用。
- [完整方案阅读页](../../design.html)：含下列全部Markdown正文，可离线导航和搜索。
- 本目录Markdown为可编辑源文档；README不替代正文。

## 专题

- [总体设计与信息架构](00-overall-design-and-information-architecture.md)
- [设计系统与变量](01-design-system-and-tokens.md)
- [全局外壳与布局](02-layout-and-shell.md)
- [首页布局与交互](03-immersive-curtain-home.md)
- [控制台逐页规范](04-console-pages-specification.md)
- [80项组件契约](05-component-library-guide.md)
- [公开、账号与错误页面](06-public-pages-specification.md)
- [Playground与开发者中心](07-playground-and-developer-center.md)
- [渠道、治理与39项系统设置](08-channel-workbench-and-admin.md)
- [钱包、订阅与计费保护](09-billing-wallet-and-subscriptions.md)
- [榜单、对比与数据可信度](10-ranking-comparison-and-trust.md)
- [21项动效与渲染素材规范](11-motion-rendering-and-assets.md)
- [文案边界、政策与可访问性](12-content-policy-and-accessibility.md)
- [差距映射与实施路线](13-gap-mapping-and-roadmap.md)
- [101项页面与80项组件覆盖](14-route-and-component-coverage.md)

## 首页可操作范围

响应式导航、路由选择与暂停、物理路由示意、模型搜索/提供方筛选/重置、详情面板、接入参数、代码语言切换、复制与手动选择回退、方案阅读入口。真实品牌标志与字体本地化；模型名称为真实 referent，内容标记示例，API ID与价格待真实接入。不登录、不付款、不发送模型请求，不把本地筛选伪装成实时网关数据。

## 重要约束

产品页面只保留标题、动作、字段、实际数据、状态与必要错误；无说明副标题/营销小字，禁用含指定词的转折句。设计文档保留详细解释。CodeGo暖白/铜橙与字体沿用原品牌；参考站借用排版与机制呈现，不复制商业数字或承诺。

所有后端功能、新账务、榜单和团队能力按对应专题标现有/规划/待确认。政策、价格、限额、服务承诺需真实证据。首页仅本地演示，其余业务页本轮交付设计规格。原仓库中的未知线上状态不视为已发布。

## 交付方式与检查边界

按冻结的生产流程一次性编写源文件，写入主HTML即交付；未对生成物运行浏览器预览、截图、渲染、测试或生成后修补。各章列出的验收要求用于后续真实工程实施，不能理解为本轮已经完成的验证。
