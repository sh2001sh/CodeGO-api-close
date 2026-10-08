import { traditionalText } from './traditional'

export type ContentLanguage = 'zh-CN' | 'zh-HK' | 'en'
export type Text = { 'zh-CN': string; 'zh-HK': string; en: string }
const text = (chinese: string, english: string): Text => {
  const traditional = traditionalText[chinese]
  if (traditional === undefined) throw new Error('Missing Hong Kong documentation translation')
  return { 'zh-CN': chinese, 'zh-HK': traditional, en: english }
}
export type DocBlock =
  | { kind: 'paragraph'; text: Text }
  | { kind: 'list'; items: Text[]; ordered?: boolean }
  | { kind: 'note'; text: Text }
  | { kind: 'code'; sample: 'quickstart' | 'models' | 'responses' | 'embeddings' }
  | { kind: 'table'; headers: Text[]; rows: Text[][] }
  | { kind: 'links'; items: { href: string; label: Text }[] }
export type DocArticle = {
  slug: string
  group: 'start' | 'users' | 'market' | 'suppliers' | 'api' | 'troubleshooting' | 'updates'
  title: Text
  summary: Text
  sections: { id: string; title: Text; blocks: DocBlock[] }[]
}
const p = (cn: string, en: string): DocBlock => ({ kind: 'paragraph', text: text(cn, en) })
const note = (cn: string, en: string): DocBlock => ({ kind: 'note', text: text(cn, en) })
const list = (items: [string, string][], ordered = false): DocBlock => ({
  kind: 'list',
  items: items.map(([cn, en]) => text(cn, en)),
  ordered,
})
const links = (items: [string, string, string][]): DocBlock => ({
  kind: 'links',
  items: items.map(([href, cn, en]) => ({ href, label: text(cn, en) })),
})

export const docArticles: readonly DocArticle[] = [
  {
    slug: 'quickstart',
    group: 'start',
    title: text('完成第一次调用', 'Make your first request'),
    summary: text(
      '创建密钥、选择当前可用模型，用一条短请求验证接入与费用。',
      'Create a key, select an available model, and verify access and billing with a short request.',
    ),
    sections: [
      {
        id: 'prepare',
        title: text('准备账号与密钥', 'Prepare your account and key'),
        blocks: [
          list(
            [
              [
                '注册并登录 CodeGo，阅读当前服务条款和隐私政策。',
                'Register and sign in to CodeGo, and read the current terms and privacy policy.',
              ],
              [
                '在钱包查看余额或适用套餐，再创建 API Key。设置允许的分组、模型、有效期和额度限制。',
                'Check your wallet balance or eligible plan, then create an API key. Set its allowed groups, models, expiry and quota.',
              ],
              [
                '从模型页复制模型 ID。公开目录只用于发现，实际可调用范围由你的密钥和分组权限决定。',
                'Copy a model ID from the model directory. Public discovery does not grant access: your key and group permissions determine availability.',
              ],
            ],
            true,
          ),
          links([
            ['/sign-up', '注册账号', 'Create an account'],
            ['/wallet', '钱包', 'Wallet'],
            ['/keys', '创建 API Key', 'Create an API key'],
            ['/models', '模型与价格', 'Models and pricing'],
          ]),
        ],
      },
      {
        id: 'request',
        title: text('发送一条短请求', 'Send a short request'),
        blocks: [
          p(
            'Base URL 使用本站域名加 /v1。先选一个支持对话的模型；目录中存在模型，不代表所有分组支持相同能力。下方示例不会自动发起付费请求。',
            'Use this site’s origin followed by /v1 as your base URL. Choose a model that supports chat; capabilities may differ between groups. The examples below do not send paid requests automatically.',
          ),
          { kind: 'code', sample: 'quickstart' },
          note(
            '把完整密钥保存在服务端环境变量 CODEGO_API_KEY。不要放在浏览器脚本、公开仓库、截图或支持工单中。',
            'Store your full key in the server-side CODEGO_API_KEY environment variable. Do not include it in browser scripts, public repositories, screenshots or support tickets.',
          ),
        ],
      },
      {
        id: 'verify',
        title: text('核对结果', 'Verify the result'),
        blocks: [
          p(
            '成功后到使用日志核对模型、分组、状态、用量和费用。保存响应头 X-Request-Id，后续排查可以用它定位请求。若调用失败，先确认密钥权限和模型 ID，再查看错误处理指南。',
            'After a successful call, check the model, group, status, usage and charge in usage logs. Save the X-Request-Id response header to locate the request later. If the call fails, verify key permissions and the model ID, then consult the error guide.',
          ),
          links([
            ['/usage-logs', '使用日志', 'Usage logs'],
            ['/docs?article=errors', '处理调用错误', 'Handle request errors'],
          ]),
        ],
      },
    ],
  },
  {
    slug: 'authentication',
    group: 'start',
    title: text('密钥与认证', 'Keys and authentication'),
    summary: text(
      '区分网站登录与 API 认证，设置最小访问权限并处理密钥泄露。',
      'Separate website sessions from API authentication, restrict access and handle exposed keys.',
    ),
    sections: [
      {
        id: 'bearer',
        title: text('认证方式', 'Authentication method'),
        blocks: [
          p(
            'OpenAI 兼容请求使用 Authorization: Bearer <CODEGO_API_KEY>。网站登录会话用于控制台操作，不能替代模型 API 的密钥。不要把上游厂商密钥当作 CodeGo 密钥。',
            'OpenAI-compatible requests use Authorization: Bearer <CODEGO_API_KEY>. Website sessions authenticate console actions and do not replace a model API key. Upstream provider credentials are not CodeGo API keys.',
          ),
          p(
            '密钥管理允许本人查看完整密钥。共享机器上不要保持密钥明文展开；每个应用和环境使用独立密钥，便于停用和核对费用。',
            'The key manager allows the owner to reveal a full key. Avoid leaving it visible on shared devices. Use separate keys for each application and environment so you can disable access and inspect charges independently.',
          ),
        ],
      },
      {
        id: 'scope',
        title: text('访问范围', 'Access scope'),
        blocks: [
          list([
            [
              '允许分组决定能访问哪些供应来源；允许模型决定可请求哪些模型 ID。',
              'Allowed groups determine accessible supply sources; allowed models restrict requested model IDs.',
            ],
            [
              '余额、套餐适用范围、密钥额度和限流都可能影响调用。模型公开展示不等于当前密钥获得授权。',
              'Wallet funds, plan eligibility, key quota and rate limits can all affect access. Public model listings do not authorize the current key.',
            ],
            [
              '用户路由池需要绑定到自己的密钥；别人的路由池不能作为你的访问权限。',
              'Bind your own route pool to your key. Another user’s pool does not grant access.',
            ],
          ]),
        ],
      },
      {
        id: 'rotation',
        title: text('泄露与轮换', 'Exposure and rotation'),
        blocks: [
          list([
            [
              '发现泄露后立即停用旧密钥，再创建新密钥并更新服务端配置。',
              'Disable an exposed key immediately, create a replacement and update your server configuration.',
            ],
            [
              '在使用日志核对异常调用与费用，保留请求 ID 和发生时间。',
              'Inspect usage logs for unexpected requests and charges; retain request IDs and timestamps.',
            ],
            [
              '通过支持入口提交问题时，只提供脱敏信息，切勿发送完整密钥。',
              'When contacting support, provide redacted details and never send a full key.',
            ],
          ]),
          links([
            ['/keys', '密钥管理', 'Key management'],
            ['/support', '联系支持', 'Contact support'],
          ]),
        ],
      },
    ],
  },
  {
    slug: 'client-integration',
    group: 'start',
    title: text('SDK 与客户端接入', 'SDK and client integration'),
    summary: text(
      '配置自定义接口地址、模型与分组，识别兼容范围。',
      'Configure a custom endpoint, model and group, and understand compatibility limits.',
    ),
    sections: [
      {
        id: 'configure',
        title: text('配置连接', 'Configure a connection'),
        blocks: [
          list(
            [
              [
                '选择支持自定义 OpenAI 兼容接口的 SDK 或客户端。Python 和 Node.js 示例使用 openai SDK。',
                'Choose an SDK or client that accepts a custom OpenAI-compatible endpoint. The Python and Node.js examples use the openai SDK.',
              ],
              [
                'Base URL 填本站 /v1 地址；只有客户端明确要求完整路径时才填 /v1/chat/completions，避免重复拼接 /v1。',
                'Use this site’s /v1 address as the base URL. Use /v1/chat/completions only when a client explicitly asks for a full endpoint; avoid duplicating /v1.',
              ],
              [
                '填 CodeGo API Key 和真实模型 ID，先关闭工具和多模态功能做短文本验证。',
                'Enter a CodeGo API key and a real model ID. Start with a short text request before enabling tools or multimodal inputs.',
              ],
              [
                '最后逐项验证流式、工具、图像或结构化输出，并核对使用日志。',
                'Then verify streaming, tools, images or structured output individually, and check usage logs.',
              ],
            ],
            true,
          ),
          { kind: 'code', sample: 'quickstart' },
        ],
      },
      {
        id: 'compatibility',
        title: text('兼容边界', 'Compatibility boundaries'),
        blocks: [
          note(
            'OpenAI 兼容不代表支持 OpenAI 的全部端点、SDK 方法或参数。所选接口、模型、分组和上游共同决定能力；渠道主自报的能力仍需要你验证。',
            'OpenAI compatibility does not mean every OpenAI endpoint, SDK method or parameter is supported. The endpoint, model, group and upstream determine capabilities; verify owner-declared capabilities for your application.',
          ),
          p(
            '不要直接复制 OpenRouter 专属的 provider、transforms 或路由参数。CodeGo 的分组与用户路由池在控制台配置，除非文档明确说明，否则不要假设其他平台的扩展字段生效。',
            'Do not assume OpenRouter-specific provider, transforms or routing parameters work here. Configure CodeGo groups and personal route pools in the console; other platforms’ extension fields are not guaranteed to apply.',
          ),
        ],
      },
    ],
  },
  {
    slug: 'billing',
    group: 'users',
    title: text('价格、用量与费用', 'Pricing, usage and charges'),
    summary: text(
      '读懂 credits、分组价格、缓存用量与逐笔账单。',
      'Understand credits, group pricing, cache usage and per-request billing.',
    ),
    sections: [
      {
        id: 'units',
        title: text('额度与报价单位', 'Credits and pricing units'),
        blocks: [
          p(
            'credits 是平台额度单位，精确到 0.000001 credit。支付币种、实付金额和到账额度以购买确认页为准，不要把 credits 默认当作港币、美元或人民币。',
            'Credits are platform quota units with precision down to 0.000001 credit. The payment currency, amount paid and credits received are shown at checkout; do not assume credits equal HKD, USD or CNY.',
          ),
          p(
            '模型页和市场展示的分组价格已经包含该分组倍率，不要再乘一次。按 token 计价时，普通文本估算为输入 token × 输入单价 + 输出 token × 输出单价，再按页面注明的单价单位换算。',
            'Model and market group prices already include the group multiplier; do not multiply again. For token-priced text, estimate input tokens × input price + output tokens × output price, using the price unit shown on the page.',
          ),
        ],
      },
      {
        id: 'actual',
        title: text('估算与实际结算', 'Estimates and actual settlement'),
        blocks: [
          p(
            '缓存、图像、音频、推理或按次费用应按具体模型规则单独计算。未知 token 数、不同缓存命中和实际输出长度都会改变费用。估算用于比较，不是最终账单。',
            'Cache, image, audio, reasoning or per-request charges follow the specific model’s rules. Unknown token counts, cache hits and actual output length affect the total. Estimates aid comparison and are not final invoices.',
          ),
          p(
            '使用日志展示逐笔请求，钱包账单展示账户额度变化。输出前失败与输出后中断不是同一结算状态；已经产生输出的请求应先核对日志和扣费结果再重试。',
            'Usage logs show individual requests, while wallet records show account credit changes. A failure before output differs from an interrupted response after output. Check logs and charges before retrying a request that has produced output.',
          ),
          links([
            ['/models', '模型与价格', 'Models and pricing'],
            ['/usage-logs', '使用日志', 'Usage logs'],
            ['/wallet', '钱包与账单', 'Wallet and billing'],
          ]),
        ],
      },
      {
        id: 'receipts',
        title: text('发票与争议', 'Invoices and disputes'),
        blocks: [
          p(
            '已完成且可开票的购买可在钱包账单明细下载发票。发票对应支付购买，不代表每条模型请求均单独开票。核对买方名称与购买记录，并通过支持入口报告错误；退款条件见退款规则。',
            'Eligible completed purchases offer invoice downloads in wallet billing details. Invoices document purchases and do not imply a separate invoice for every model request. Check buyer details against the purchase and report errors through support; refund conditions are in the refund policy.',
          ),
          links([
            ['/refund-policy', '退款规则', 'Refund policy'],
            ['/support', '费用问题支持', 'Billing support'],
          ]),
        ],
      },
    ],
  },
  {
    slug: 'plans',
    group: 'users',
    title: text('套餐与历史权益', 'Plans and legacy entitlements'),
    summary: text(
      '选择余额或新套餐，核对旧套餐转换与既有卡权益。',
      'Choose wallet funds or a new plan, and check legacy conversion and card entitlements.',
    ),
    sections: [
      {
        id: 'current',
        title: text('购买新套餐', 'Buy a current plan'),
        blocks: [
          p(
            '购买前核对额度、有效期、适用分组与扣费偏好。新套餐不采用旧版十倍扣费，不通过邀请奖励发放刷新次数，也不新增附赠倍率卡。套餐是否适合你取决于实际用量和可用分组，不只看名义额度。',
            'Check quota, expiry, eligible groups and billing preference before purchase. New plans do not use legacy tenfold charging, award invitation refreshes or include new multiplier cards. Suitability depends on actual usage and eligible groups, not quota alone.',
          ),
          p(
            '余额按实际调用费用扣除。需要多个分组或用量不稳定时，先比较购买确认页与当前模型价格，再决定使用余额还是套餐。',
            'Wallet funds are charged for actual usage. If you need multiple groups or have variable demand, compare checkout terms and current model prices before choosing a plan or wallet funds.',
          ),
        ],
      },
      {
        id: 'legacy',
        title: text('旧套餐与已有卡', 'Legacy plans and existing cards'),
        blocks: [
          list([
            [
              '旧套餐和已有刷新权益可以按原规则继续使用。',
              'Legacy plans and existing refresh entitlements remain usable under their original rules.',
            ],
            [
              '未到期且满足转换条件的旧套餐，可在钱包查看转余额比例与报价；到期后不能转换。',
              'Eligible unexpired legacy plans can show a wallet conversion ratio and quote. Expired plans cannot be converted.',
            ],
            [
              '转换前阅读确认说明；转换后对应权益不能再刷新。已有倍率卡与历史订单承诺按原权益继续履行。',
              'Read the confirmation before converting: the converted entitlement can no longer be refreshed. Existing multiplier cards and historical order commitments remain governed by their original entitlements.',
            ],
          ]),
          note(
            '钱包展示的资格、报价与订单快照决定你的具体权益，文档不会替代购买时确认的条款。',
            'Your eligibility, quote and order snapshot in the wallet determine the specific entitlement. This guide does not replace terms confirmed at purchase.',
          ),
          links([['/wallet', '查看套餐与转换报价', 'View plans and conversion quotes']]),
        ],
      },
    ],
  },
  {
    slug: 'choosing-groups',
    group: 'market',
    title: text('选择适合的分组', 'Choose a suitable group'),
    summary: text(
      '以同模型价格、真实样本、能力与数据声明判断供应来源。',
      'Compare the same model using prices, real samples, capabilities and data disclosures.',
    ),
    sections: [
      {
        id: 'compare',
        title: text('先确定模型与用途', 'Start with the model and task'),
        blocks: [
          list(
            [
              [
                '先筛选真实模型 ID 和所需能力，再比较提供该模型的分组。不要跨不同模型把价格或速度直接排名。',
                'Filter by the exact model ID and required capabilities, then compare groups that provide it. Do not rank price or speed across unrelated models.',
              ],
              [
                '查看同一时间窗口的成功率、请求数量、独立消费者和性能样本数；样本不足时不要把一次成功当作稳定性保证。',
                'Compare success rate, request count, independent consumers and performance samples over the same window. A single success is not a stability guarantee.',
              ],
              [
                '价格已经包含分组倍率。按自己的输入与输出规模估算，先用低成本短请求验证。',
                'Prices already include the group multiplier. Estimate using your input and output sizes, then verify with a low-cost short request.',
              ],
            ],
            true,
          ),
          links([
            ['/channel-market', '比较渠道分组', 'Compare market groups'],
            ['/models', '核对模型价格', 'Check model pricing'],
          ]),
        ],
      },
      {
        id: 'signals',
        title: text('如何读质量指标', 'Read quality signals'),
        blocks: [
          p(
            '近期请求条用于观察最近请求的结果，不代表全部历史。模型维度统计比整个分组的混合统计更适合判断当前模型。首 token 时间衡量开始输出的等待，生成速度衡量输出阶段；总耗时、探测耗时和首 token 时间不能混用。',
            'Recent request bars show recent outcomes, not all historical activity. Model-level statistics are more relevant than mixed group totals. Time to first token measures the wait for output; generation speed measures output delivery. Total duration, probe latency and time to first token are different metrics.',
          ),
          p(
            '无样本或未知指标应显示未知，不能视为零延迟或 100% 成功。平台排除明确不应计入渠道成功率的请求；评分与实测性能回答不同问题。',
            'Missing samples or unknown metrics are unknown, not zero latency or 100% success. Requests explicitly excluded from channel success-rate calculations are not counted. Ratings and measured performance describe different aspects of service.',
          ),
        ],
      },
      {
        id: 'trust',
        title: text('验证与声明的边界', 'Verification and declaration limits'),
        blocks: [
          note(
            '连通验证只表示某次检查的结果。它不证明来源授权、模型真实性、所有能力可用、长期可用性或 SLA。渠道主自报的地区、留存、训练和能力不是平台认证。',
            'Connectivity verification describes one check. It does not prove source authorization, model authenticity, every capability, long-term availability or an SLA. Owner-declared regions, retention, training and capabilities are not platform certification.',
          ),
          p(
            '处理敏感数据前，检查来源与数据策略；未声明代表未知。香港公司注册地不等于请求一定在香港处理。对数据位置有硬性要求时，先取得可核验说明。',
            'Before sending sensitive data, inspect source and data-policy disclosures. Undeclared means unknown. A Hong Kong company registration does not guarantee requests are processed in Hong Kong. Obtain verifiable evidence if data location is a strict requirement.',
          ),
        ],
      },
    ],
  },
  {
    slug: 'route-pools',
    group: 'market',
    title: text('用户路由池与自动更新', 'Personal route pools and automatic updates'),
    summary: text(
      '把多个有权限的分组组合成路由池，并控制选择、失败切换与定时更新。',
      'Combine authorized groups into a pool and control selection, failover and scheduled updates.',
    ),
    sections: [
      {
        id: 'create',
        title: text('创建与绑定', 'Create and bind'),
        blocks: [
          list(
            [
              [
                '进入市场的路由池入口，新建池并选取自己有权访问的分组。公开数字 ID 用于识别，保存与路由使用系统分组 ID。',
                'Open route pools in the market, create a pool and select groups you are authorized to use. Public numeric IDs identify listings; system group IDs are used for saving and routing.',
              ],
              [
                '设定策略、成员顺序或权重、最高倍率、最大尝试次数和失败冷却。先保留少量可靠成员并测试。',
                'Set the strategy, member order or weights, maximum multiplier, attempt limit and failure cooldown. Start with a small set of reliable members and test.',
              ],
              [
                '在 API Key 中选择该路由池并确认允许模型；使用同一密钥发请求，检查实际选中分组和费用。',
                'Select that pool for your API key and confirm allowed models. Send a request with the key and inspect the selected group and charge.',
              ],
            ],
            true,
          ),
          p(
            '可用策略包括优先级、价格优先、综合评分、加权随机、轮询和填满优先。策略决定候选顺序，访问权限、有效凭据、模型支持和冷却仍会过滤不可用候选。',
            'Available strategies include priority, cost, score, weighted random, round robin and fill first. Strategies order candidates; authorization, valid credentials, model support and cooldown still exclude unusable candidates.',
          ),
        ],
      },
      {
        id: 'automatic',
        title: text('开启自动更新', 'Enable automatic updates'),
        blocks: [
          p(
            '自动构建按所选模型和指标权重更新候选。可设置间隔或每日计划、池大小及探索数量；每日计划的时间为 UTC，界面提供最近构建、下次构建与错误状态。保存配置后核对实际成员，不能只看开关。',
            'Automatic builds update candidates using selected models and metric weights. Configure an interval or daily schedule, pool size and exploration count. Daily schedules use UTC; the UI shows the last build, next build and errors. Inspect the actual members after saving, not just the toggle.',
          ),
          note(
            '自动更新不是服务保证，也不会扩大你的授权范围。候选变化可能改变供应来源、数据策略与费用；对来源有限制的应用应审核更新后的成员。',
            'Automatic updates are not a service guarantee and do not expand authorization. Changes may affect supply source, data policy and cost; review updated members when your application restricts sources.',
          ),
        ],
      },
      {
        id: 'failover',
        title: text('失败与重试边界', 'Failover and retry boundaries'),
        blocks: [
          p(
            '失败切换受尝试次数、冷却和请求状态限制。客户端收到部分输出后，不应假设平台或客户端可以无损重试；重试会产生新的请求，可能再次产生费用。结合 X-Request-Id 和使用日志判断。',
            'Failover is limited by attempts, cooldown and request state. After receiving partial output, do not assume the platform or client can retry without consequences. A retry is a new request and may incur another charge. Use X-Request-Id and usage logs to decide.',
          ),
          links([
            ['/channel-market', '配置路由池', 'Configure route pools'],
            ['/keys', '绑定密钥', 'Bind a key'],
          ]),
        ],
      },
    ],
  },
  {
    slug: 'ratings-ranking',
    group: 'market',
    title: text('评分、排名与店铺', 'Ratings, ranking and shops'),
    summary: text(
      '了解真实消费者评价、样本置信度与渠道主店铺的展示规则。',
      'Understand real-consumer reviews, confidence and channel-owner shop listings.',
    ),
    sections: [
      {
        id: 'reviews',
        title: text('谁可以评分', 'Who can submit ratings'),
        blocks: [
          p(
            '评分在主站提交，以真实使用资格为基础，渠道主不能给自己的渠道评分。每位用户对每条渠道保留一条评分，可更新；1–5 星展示为 2–10 分。社区读取主站评分，不能另外改写为第二套评分。',
            'Ratings are submitted on the main site and require real usage. Owners cannot rate their own channels. Each user has one updatable rating per channel; 1–5 stars map to a 2–10 score. The community reads main-site ratings rather than maintaining a separate authority.',
          ),
          p(
            '店铺评分先合并同一消费者对旗下公开渠道的评价，再让消费者等权平均，避免渠道数量多的店铺获得重复权重。按模型或厂商筛选不改变店铺总分。',
            'Shop ratings first combine each consumer’s reviews of the shop’s public channels, then weight consumers equally. More channels do not give a shop extra weight. Model or vendor filters do not change the overall shop score.',
          ),
        ],
      },
      {
        id: 'ranking',
        title: text('排名适合什么用途', 'What ranking is useful for'),
        blocks: [
          p(
            '综合排名结合平台当前公开的服务信号与置信度。排名用于发现候选，不能代替你对具体模型、价格和数据策略的判断。小样本、新分组和不同观察窗口都影响可比性；页面展示的实际指标与当前排名规则为准。',
            'Ranking combines the service signals and confidence currently published by the platform. It helps discover candidates, but cannot replace checks of the model, pricing and data policy. Small samples, new groups and different observation windows affect comparability; use the displayed metrics and current ranking rules.',
          ),
          p(
            '名称和备注用于识别服务，不允许广告、联系方式、冒充官方或违规内容。发现刷评、误导声明、模型不符或广告时，通过支持入口提供分组 ID 和证据。',
            'Names and notes identify services; advertising, contact details, official impersonation and prohibited content are not allowed. Report suspected review manipulation, misleading disclosures, model mismatches or ads with the group ID and evidence.',
          ),
          links([
            ['/terms', '服务与治理规则', 'Service and governance rules'],
            ['/support', '反馈问题', 'Report an issue'],
          ]),
        ],
      },
    ],
  },
  {
    slug: 'supplier-onboarding',
    group: 'suppliers',
    title: text('提交渠道与公开上架', 'Submit a channel and publish a listing'),
    summary: text(
      '从创建渠道、接受供给协议到连通验证与公开披露。',
      'Create a channel, accept supply terms, verify connectivity and disclose service conditions.',
    ),
    sections: [
      {
        id: 'requirements',
        title: text('提交前准备', 'Before submission'),
        blocks: [
          list(
            [
              [
                '确认你有权提供该服务和凭据，阅读并接受当前供给协议。所有人可提交，不代表所有提交均可立即公开。',
                'Confirm you are authorized to provide the service and credentials, and accept the current supplier agreement. Open submissions do not mean automatic public listing.',
              ],
              [
                '准备接口类型、上游地址、凭据、真实模型 ID、价格倍率、容量和可用时间。不要把凭据写在名称或备注。',
                'Prepare the API type, upstream URL, credentials, real model IDs, price multiplier, capacity and availability. Never include credentials in names or notes.',
              ],
              [
                '在渠道主工作台创建渠道，核对私有或公开状态，并设置受控的店铺名、分组名和备注。',
                'Create the channel in the owner workspace, check private or public status, and set compliant shop and group names and notes.',
              ],
            ],
            true,
          ),
          links([
            ['/my-channels', '渠道主工作台', 'Channel owner workspace'],
            ['/terms', '供给与服务规则', 'Supplier and service rules'],
          ]),
        ],
      },
      {
        id: 'validate',
        title: text('检查并披露', 'Verify and disclose'),
        blocks: [
          p(
            '执行模型连通检查，查看具体模型的状态与探测时间。填写来源、处理地区、数据留存、训练用途和每个模型的能力声明；不能确认的项目应保留未知。',
            'Run model connectivity checks and review each model’s result and probe time. Disclose source, processing regions, retention, training use and model capabilities. Leave items unknown when you cannot confirm them.',
          ),
          note(
            '通过连通检查不等于获平台来源认证。你需对凭据授权、模型标识、价格和声明的准确性负责；公开展示仍受平台治理和审核约束。',
            'A passed connectivity check is not platform certification of the source. You are responsible for credential authorization, model identity, pricing and accurate disclosures. Public listings remain subject to platform review and governance.',
          ),
        ],
      },
      {
        id: 'operate',
        title: text('维护供应服务', 'Maintain your service'),
        blocks: [
          p(
            '设置并发、QPS、维护窗口与探测配置，观察真实请求和失败原因。上游变化、模型下线或数据策略变化时，及时更新声明与供给状态。渠道不可用时先暂停供给，避免继续吸收请求。',
            'Configure concurrency, QPS, maintenance windows and probes, and inspect real requests and failure reasons. Update disclosures and availability when the upstream, model or data policy changes. Pause unavailable supply before accepting more requests.',
          ),
          links([
            [
              '/docs?article=supplier-revenue',
              '收入与结算口径',
              'Revenue and settlement definitions',
            ],
            ['/docs?article=supplier-disclosures', '填写服务声明', 'Complete disclosures'],
          ]),
        ],
      },
    ],
  },
  {
    slug: 'supplier-disclosures',
    group: 'suppliers',
    title: text('来源、能力与数据声明', 'Source, capability and data disclosures'),
    summary: text(
      '提供可比较的信息，保留未知项并区分自报与验证。',
      'Provide comparable information, retain unknown values and distinguish declarations from verification.',
    ),
    sections: [
      {
        id: 'source',
        title: text('来源与地区', 'Source and regions'),
        blocks: [
          p(
            '来源类型可为直接接入、转售、自托管或未知。直接接入本身不证明官方授权；转售也不自动证明服务不可靠。地区填写实际处理国家或地区代码，不能用公司注册地替代。',
            'Sources can be direct, reseller, self-hosted or unknown. Direct access alone does not prove official authorization, and resale does not automatically imply unreliability. Report actual processing country or region codes rather than company registration location.',
          ),
        ],
      },
      {
        id: 'data',
        title: text('留存与训练', 'Retention and training'),
        blocks: [
          p(
            '留存可声明未知、不留存或有限留存；有限留存应填写天数。训练用途为未知、不用于训练或用于训练。只有你能核实完整上游链路时才能作明确声明；资料不完整应填未知。可补充真实 HTTPS 数据政策链接，不能放广告或联系方式。',
            'Retention can be unknown, none or limited; limited retention includes days. Training use can be unknown, no or yes. Make definitive statements only when you can verify the upstream chain; otherwise use unknown. A real HTTPS data-policy link may be provided, but not ads or contact links.',
          ),
          note(
            '声明标记为渠道主自报，不等于平台核验。不能通过编辑把自报改成已认证，也不能用含糊名称暗示零留存或官方来源。',
            'Disclosures are labelled owner-declared, not platform-verified. Editing cannot turn a declaration into certification, and vague names must not imply zero retention or an official source.',
          ),
        ],
      },
      {
        id: 'capabilities',
        title: text('逐模型能力', 'Per-model capabilities'),
        blocks: [
          p(
            '对每个实际供应模型分别填写流式、工具调用、结构化输出、视觉能力为支持、不支持或未知；可补充上下文和最大输出 token。模型名相同不保证能力相同，不能把一个模型的测试结果覆盖到全部模型。',
            'For each supplied model, report streaming, tools, structured output and vision as supported, unsupported or unknown. Context and maximum output tokens may be included. The same model name does not guarantee identical capabilities; do not apply one model’s test to every model.',
          ),
        ],
      },
    ],
  },
  {
    slug: 'supplier-revenue',
    group: 'suppliers',
    title: text('渠道收入与结算', 'Channel revenue and settlement'),
    summary: text(
      '统一观察运营指标、消费者费用、供给收入、扣项和站内钱包结算。',
      'Read operational metrics, consumer charges, supply revenue, deductions and wallet settlement consistently.',
    ),
    sections: [
      {
        id: 'scope',
        title: text('统一筛选口径', 'Use consistent filters'),
        blocks: [
          p(
            '渠道主工作台按时间、渠道和模型查看请求量、成功量、消费者、趋势与收入贡献。切换筛选后，核对显示的起止时间。请求用量与结算记录可能发生在不同时间，不要把请求时间汇总与结算时间汇总直接视为同一口径。',
            'Filter the owner workspace by time, channel and model to inspect requests, successes, consumers, trends and revenue contributions. Check the displayed date range after changing filters. Usage and settlement events can occur at different times; request-time and settlement-time totals are not interchangeable.',
          ),
        ],
      },
      {
        id: 'definitions',
        title: text('收入字段含义', 'Revenue field definitions'),
        blocks: [
          {
            kind: 'table',
            headers: [text('字段', 'Field'), text('解释', 'Meaning')],
            rows: [
              [
                text('消费者费用', 'Consumer charge'),
                text(
                  '消费者本次调用被扣除的额度，不等于渠道主收入。',
                  'Credits charged to the consumer for the request; this is not owner revenue.',
                ),
              ],
              [
                text('供给收入', 'Gross supply revenue'),
                text(
                  '扣除渠道结算相关扣项之前的供给收入。',
                  'Supply revenue before settlement deductions.',
                ),
              ],
              [
                text('佣金与手续费', 'Commission and fees'),
                text(
                  '以当前配置和逐笔结算记录为准，不把某一费率视为永久保证。',
                  'Use current configuration and per-settlement records; no single rate is a permanent guarantee.',
                ),
              ],
              [
                text('净收入', 'Net revenue'),
                text(
                  '结算扣项后的收入，未扣除你自己的上游采购成本，因此不是利润。',
                  'Revenue after settlement deductions, excluding your upstream purchase cost; this is not profit.',
                ),
              ],
              [
                text('待释放／已释放／已追回', 'Pending / released / reclaimed'),
                text(
                  '分别观察保留中的收入、已记入站内钱包的收入与按规则追回的收入。',
                  'Distinguishes held revenue, revenue credited to the site wallet and revenue reclaimed under applicable rules.',
                ),
              ],
            ],
          },
          note(
            '当前结算至 CodeGo 站内钱包，不代表提供现金提现。可用时间、保留期和追偿条件以当前供给协议、配置及具体结算记录为准。',
            'Settlement credits your CodeGo site wallet; it does not promise cash withdrawals. Availability times, hold periods and reclamation conditions follow the current supplier agreement, configuration and settlement record.',
          ),
        ],
      },
      {
        id: 'reconcile',
        title: text('核对与导出', 'Reconcile and export'),
        blocks: [
          p(
            '用请求 ID 连接请求日志与逐笔结算。查看消费者费用、供给收入、佣金、手续费、净收入、状态与可用时间。页面只显示有限条近期记录；导出按所选筛选条件获取记录，展示行数不能当作全部历史规模。',
            'Use request IDs to connect logs and settlements. Check consumer charge, gross revenue, commission, fees, net revenue, state and availability time. The page shows a limited recent list; exports follow selected filters. The visible row count is not the entire history.',
          ),
          p(
            '发现汇总与流水不符时，保存筛选起止时间、渠道 ID、模型和相关请求 ID，通过支持入口反馈。不要在工单附上上游凭据或用户请求正文。',
            'If summaries and records disagree, retain the date range, channel ID, model and relevant request IDs for support. Do not attach upstream credentials or user request bodies.',
          ),
          links([
            ['/my-channels', '查看经营概览', 'View owner analytics'],
            ['/support', '反馈结算问题', 'Report settlement issues'],
          ]),
        ],
      },
    ],
  },
  {
    slug: 'api-reference',
    group: 'api',
    title: text('模型 API 参考', 'Model API reference'),
    summary: text(
      '了解发现、对话、Responses 和 embeddings 的基本请求，逐项确认能力。',
      'Learn basic discovery, chat, Responses and embeddings requests, verifying capabilities individually.',
    ),
    sections: [
      {
        id: 'discovery',
        title: text('GET /v1/models', 'GET /v1/models'),
        blocks: [
          p(
            '带 API Key 查询当前密钥能发现的模型。返回是 OpenAI 风格的 data 列表，模型 ID 可用于后续请求。发现结果受权限、分组、凭据和路由状态影响，可能与公开目录不同。',
            'Use your API key to discover available models. The response is an OpenAI-style data list whose IDs can be used in requests. Permissions, groups, credentials and routing state affect discovery, so it may differ from the public catalog.',
          ),
          { kind: 'code', sample: 'models' },
        ],
      },
      {
        id: 'chat',
        title: text('POST /v1/chat/completions', 'POST /v1/chat/completions'),
        blocks: [
          p(
            '基本请求包含 model 和 messages；messages 是包含 role 与 content 的消息数组。stream: true 请求流式响应。工具、图像和其他字段是否可用，需分别核对模型与分组能力。不要假设未知参数会被原样转发。',
            'A basic request includes model and messages, an array of role/content entries. Set stream: true for streaming. Verify model and group support for tools, images and other fields separately. Do not assume unknown parameters are forwarded unchanged.',
          ),
          { kind: 'code', sample: 'quickstart' },
        ],
      },
      {
        id: 'responses',
        title: text('POST /v1/responses', 'POST /v1/responses'),
        blocks: [
          p(
            'Responses 接口使用 model 和 input。它与 chat/completions 的请求和事件结构不同；仅对适用模型与分组调用，先用简单文本验证。接口存在不代表所有 OpenAI 内置工具、存储状态或后台任务均可用。',
            'Responses uses model and input, with a request and event structure different from chat/completions. Use eligible models and groups, starting with simple text. The endpoint’s existence does not guarantee every OpenAI built-in tool, stored state or background task is available.',
          ),
          { kind: 'code', sample: 'responses' },
        ],
      },
      {
        id: 'embeddings',
        title: text('POST /v1/embeddings', 'POST /v1/embeddings'),
        blocks: [
          p(
            '使用支持 embeddings 的模型，传入 model 和文本 input。对话模型不自动支持向量。dimensions、编码格式、批次大小等限制以所选模型和渠道实际支持为准。',
            'Choose an embeddings model and provide model and text input. Chat models do not automatically support embeddings. Dimensions, encoding and batch limits depend on the selected model and channel.',
          ),
          { kind: 'code', sample: 'embeddings' },
        ],
      },
      {
        id: 'protocols',
        title: text('其他协议与能力', 'Other protocols and capabilities'),
        blocks: [
          p(
            '平台还有适用供应的 Anthropic、Gemini 及其他协议适配。它们的认证头、路径和事件结构并不等同于 OpenAI。对话页或某个接口可用，不能推断所有音视频、实时或文件操作都可用；在具体接入说明与模型能力明确后再启用。',
            'The platform also adapts Anthropic, Gemini and other protocols for eligible supply. Their authentication headers, paths and events differ from OpenAI. A working chat page or endpoint does not imply all media, realtime or file operations are supported; enable them after confirming integration instructions and model capabilities.',
          ),
          links([
            ['/docs?article=streaming', '流式与能力验证', 'Streaming and capability verification'],
            ['/docs?article=errors', '错误与重试', 'Errors and retries'],
          ]),
        ],
      },
    ],
  },
  {
    slug: 'streaming',
    group: 'api',
    title: text('流式、工具与结构化输出', 'Streaming, tools and structured output'),
    summary: text(
      '区分事件协议、输出前后错误，并逐项验证高级能力。',
      'Distinguish event protocols and failures before or after output, and validate advanced capabilities.',
    ),
    sections: [
      {
        id: 'events',
        title: text('消费流式事件', 'Consume streaming events'),
        blocks: [
          p(
            'Chat Completions 使用 SSE，读取 data 中的增量内容并处理结束标记。Responses 的事件类型不同，不能共用只读取 choices[0].delta.content 的解析器。用对应 SDK 处理事件，并明确识别网络中断、结束事件与空输出。',
            'Chat Completions uses SSE: read incremental data and handle the end marker. Responses has different event types and cannot use a parser that only reads choices[0].delta.content. Use the matching SDK, and distinguish network interruption, completion and empty output.',
          ),
          p(
            '客户端断开、上游错误或超时可能在响应开始后发生，此时 HTTP 状态不一定能表达最终结果。保留请求 ID，结合事件与使用日志判断，不要只凭最初的 200 判定调用完成。',
            'Client disconnects, upstream errors or timeouts may happen after a response starts; the HTTP status may not describe the final outcome. Retain the request ID and check events and usage logs rather than treating the initial 200 as completion.',
          ),
        ],
      },
      {
        id: 'tools',
        title: text('工具与结构化输出', 'Tools and structured output'),
        blocks: [
          list([
            [
              '先确认声明中支持工具调用或结构化输出，再用小型工具 schema 或 JSON schema 做验证。',
              'Confirm declared tool or structured-output support, then test with a small tool or JSON schema.',
            ],
            [
              '工具参数仍是不可信输入：服务端校验类型、权限和业务范围，不直接执行模型生成的命令。',
              'Tool arguments remain untrusted input: validate types, authorization and business scope server-side; do not directly execute model-generated commands.',
            ],
            [
              '结构化输出需验证 JSON 和 schema。兼容转换、模型能力和渠道限制可能影响返回格式。',
              'Validate structured output against JSON and the schema. Compatibility translation, model capabilities and channel limits may affect the format.',
            ],
            [
              '对图像输入、上下文和输出上限分别测试，不用一个文本请求证明全部能力。',
              'Test image inputs, context and output limits individually; a text request does not prove every capability.',
            ],
          ]),
        ],
      },
    ],
  },
  {
    slug: 'errors',
    group: 'troubleshooting',
    title: text('错误、限流与重试', 'Errors, rate limits and retries'),
    summary: text(
      '先识别鉴权、权限、额度、请求格式与上游失败，再决定是否重试。',
      'Identify authentication, permissions, quota, request format or upstream failures before deciding to retry.',
    ),
    sections: [
      {
        id: 'diagnose',
        title: text('按失败原因处理', 'Diagnose by failure reason'),
        blocks: [
          {
            kind: 'table',
            headers: [
              text('常见 HTTP 状态', 'Common HTTP status'),
              text('检查项目', 'What to check'),
            ],
            rows: [
              [
                text('400', '400'),
                text(
                  'JSON 格式、参数类型、上下文长度和接口能力；修改请求后再发，不原样无限重试。',
                  'JSON, parameter types, context length and endpoint capabilities. Correct the request rather than retrying unchanged indefinitely.',
                ),
              ],
              [
                text('401', '401'),
                text(
                  'Key 是否正确、已停用或已到期；检查 Authorization 头。',
                  'Check the Authorization header and whether the key is correct, disabled or expired.',
                ),
              ],
              [
                text('402 / 403', '402 / 403'),
                text(
                  '余额、额度、套餐适用范围、分组或模型权限；同时看具体错误消息。',
                  'Check funds, quota, plan eligibility and group/model permissions, together with the specific error message.',
                ),
              ],
              [
                text('404', '404'),
                text(
                  '接口路径、模型 ID 与当前分组是否存在该模型，不把模型名自动改成近似名称。',
                  'Check the endpoint, model ID and whether the group offers it. Do not silently substitute a similarly named model.',
                ),
              ],
              [
                text('429', '429'),
                text(
                  '账号、密钥、渠道或上游限流；减少并发并尊重响应中的 Retry-After。',
                  'Check account, key, channel or upstream limits; reduce concurrency and honor Retry-After when present.',
                ),
              ],
              [
                text('5xx / 超时', '5xx / timeout'),
                text(
                  '保存请求 ID，确认是否已经输出和结算，再决定有限重试或换用已授权候选。',
                  'Save the request ID and check output and settlement before a bounded retry or switching to an authorized candidate.',
                ),
              ],
            ],
          },
          p(
            '不同协议和失败阶段可能使用不同的错误结构或状态，具体错误消息与请求日志比单一状态码更准确。模型网关接口与网站控制面接口的响应结构不同，不要把 success/data 网站 envelope 作为模型 API 的固定返回格式。',
            'Protocols and failure stages may return different error shapes or statuses; the specific message and request log are more informative than a status alone. Model gateway and website control APIs use different response shapes. Do not expect the website’s success/data envelope from every model API.',
          ),
        ],
      },
      {
        id: 'retry',
        title: text('设计有限重试', 'Use bounded retries'),
        blocks: [
          list([
            [
              '为暂时失败设置重试上限、总超时和带随机抖动的指数退避。鉴权、权限和格式错误应修正，不直接重试。',
              'Use attempt limits, a total timeout and exponential backoff with jitter for transient failures. Correct authentication, permission and format errors instead of retrying them.',
            ],
            [
              '输出后中断不代表免费或未执行；客户端重试可能重新输出并再次扣费。',
              'An interruption after output does not mean the request was free or unexecuted; a retry may generate output and another charge.',
            ],
            [
              '工具产生外部写入时，在应用层防止重复副作用。不要假设平台为每个端点提供通用幂等保证。',
              'Prevent duplicate side effects in your application when tools write externally. Do not assume universal platform idempotency for every endpoint.',
            ],
          ]),
          links([
            ['/usage-logs', '核对请求结果', 'Inspect request outcomes'],
            ['/support', '提交排查信息', 'Submit diagnostic details'],
          ]),
        ],
      },
    ],
  },
  {
    slug: 'production',
    group: 'troubleshooting',
    title: text('生产使用与数据安全', 'Production use and data safety'),
    summary: text(
      '隔离环境、限制成本、选择供应与提供安全的排查信息。',
      'Separate environments, bound cost, review supply and provide safe diagnostic details.',
    ),
    sections: [
      {
        id: 'rollout',
        title: text('上线前检查', 'Before going live'),
        blocks: [
          list([
            [
              '测试和生产使用不同密钥，设置额度与允许模型；客户端限制并发、重试次数和输出长度。',
              'Use separate test and production keys with quota and model restrictions; bound client concurrency, retries and output length.',
            ],
            [
              '用真实业务输入验证目标模型、工具、流式、失败和长上下文边界，不只测试一条 Hello。',
              'Validate the target model, tools, streaming, failures and long-context boundaries with real application inputs, not just a Hello request.',
            ],
            [
              '为模型不可用、路由池无候选和部分输出中断设计用户可见状态；不要伪造成功。',
              'Design visible states for unavailable models, empty routing pools and partial-output interruptions. Do not fabricate success.',
            ],
            [
              '监控应用端耗时、错误率和平台使用日志，定期查看价格和供应变化。',
              'Monitor application latency, errors and platform usage logs, and review price and supply changes.',
            ],
          ]),
        ],
      },
      {
        id: 'data',
        title: text('敏感数据与供应链', 'Sensitive data and the supply chain'),
        blocks: [
          p(
            '请求可能经 CodeGo、渠道主及上游处理。先做最少数据原则，去除不必要的个人信息和秘密，再检查所选分组的数据声明。未知留存或训练策略不等于不留存或不训练。',
            'Requests may be processed by CodeGo, channel owners and upstreams. Minimize data, remove unnecessary personal details and secrets, then review the group’s data disclosures. Unknown retention or training does not mean no retention or no training.',
          ),
          p(
            '公司注册地、模型品牌和连通标识都不能单独证明数据处理位置或来源授权。对保密、地区和合规有要求的应用，应先取得足够的可核验说明。',
            'Company registration, model branding and connectivity badges do not independently prove processing location or source authorization. Applications with confidentiality, regional or compliance requirements should obtain sufficient verifiable information first.',
          ),
          links([
            ['/privacy', '隐私政策', 'Privacy policy'],
            ['/docs?article=choosing-groups', '选择供应分组', 'Choose supply groups'],
          ]),
        ],
      },
      {
        id: 'support',
        title: text('提供可用的排查信息', 'Provide useful diagnostic information'),
        blocks: [
          p(
            '通过现有社区支持入口提供请求 ID、时间与时区、模型、分组数字 ID、客户端版本、脱敏错误和可复现步骤。删除密钥、上游凭据、个人信息和用户正文。不要公开发布完整日志。',
            'Use the existing community support entry with request ID, time and time zone, model, public numeric group ID, client version, redacted error and reproduction steps. Remove keys, upstream credentials, personal details and user content. Do not publish full logs.',
          ),
          links([['/support', '支持入口', 'Support entry']]),
        ],
      },
    ],
  },
  {
    slug: 'updates',
    group: 'updates',
    title: text('新版使用变更', 'Changes in the current site'),
    summary: text(
      '查找当前文档版本和影响使用方式的变化，保留历史权益。',
      'Find the current documentation version and changes to usage while retaining legacy entitlements.',
    ),
    sections: [
      {
        id: 'version',
        title: text('文档版本：2026-10-07', 'Documentation version: 2026-10-07'),
        blocks: [
          p(
            '本文说明当前站点的产品方式；界面、模型供应和费率可能随实际配置变化。购买与协议接受以当时确认的版本和订单为准。不能把本文日期理解为每个模型在当天都已测试通过。',
            'This guide describes the current site experience. UI, model supply and rates may vary with configuration. Purchases and agreement acceptance follow the confirmed version and order. The date does not imply every model was tested that day.',
          ),
          list([
            [
              '市场保留数字 ID，按分组发现服务并可进入渠道主店铺；评分由主站维护，社区只读。',
              'The market retains numeric IDs and supports group discovery and owner shops. Ratings are maintained on the main site and read by the community.',
            ],
            [
              '用户可以配置路由池与自动更新；使用自己有权限的候选并在密钥中绑定。',
              'Users can configure personal route pools and automatic updates, using authorized candidates and binding them to keys.',
            ],
            [
              '新套餐不新增倍率卡；旧套餐、已有卡和历史订单承诺继续按原规则处理。',
              'New plans do not include new multiplier cards. Legacy plans, existing cards and historical order commitments retain their original rules.',
            ],
            [
              '文档按任务分为七个分区，旧章节链接继续定位到相应文章。',
              'Documentation is organized into seven task-based sections; legacy section links resolve to the corresponding articles.',
            ],
          ]),
        ],
      },
      {
        id: 'legacy',
        title: text('旧手册与已移除入口', 'Legacy guides and removed entries'),
        blocks: [
          p(
            '旧版手册描述的积分、宠物、微信小程序和桌面入口不属于当前站点说明。历史文件只用于回顾，不作为当前服务承诺。套餐权益仍以钱包和历史订单为准，功能移除不代表可以改写已购权益。',
            'Legacy guides describing points, pets, WeChat mini programs or desktop entries do not document the current site. Historical files are references rather than current service commitments. Plan entitlements still follow the wallet and historical orders; removed features do not rewrite purchased rights.',
          ),
          links([
            ['/docs?article=plans', '套餐与历史权益', 'Plans and legacy entitlements'],
            ['/terms', '当前服务条款', 'Current service terms'],
          ]),
        ],
      },
    ],
  },
]

export const legacyDocLinks: Readonly<Record<string, string>> = {
  quickstart: 'quickstart',
  auth: 'authentication',
  examples: 'client-integration',
  billing: 'billing',
  groups: 'choosing-groups',
  plans: 'plans',
  clients: 'client-integration',
  errors: 'errors',
}

export function resolveDocArticle(slug?: string, hash?: string) {
  if (slug) return docArticles.find((article) => article.slug === slug)
  const anchor = hash?.replace(/^#/, '')
  const mapped = anchor && legacyDocLinks[anchor]
  return docArticles.find((article) => article.slug === mapped) ?? docArticles[0]
}

export function searchDocArticles(query: string, language: ContentLanguage) {
  const terms = query.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean)
  if (!terms.length) return [...docArticles]
  return docArticles
    .filter((article) => {
      // Search both source languages so model IDs, translated titles and Chinese keywords work together.
      const haystack = JSON.stringify(article).toLocaleLowerCase()
      return terms.every((term) => haystack.includes(term))
    })
    .sort((a, b) => {
      const titleMatch = (article: DocArticle) =>
        terms.filter((term) => article.title[language].toLocaleLowerCase().includes(term)).length
      return titleMatch(b) - titleMatch(a)
    })
}
