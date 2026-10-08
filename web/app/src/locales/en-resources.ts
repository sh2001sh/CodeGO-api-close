const messages: Record<string, string> = {
  资源: 'Resources',
  更新于: 'Updated',
  资源导航: 'Resource navigation',
  产品: 'Product',
  开发者: 'Developers',
  支持: 'Support',
  公司与政策: 'Company & policies',
  常见问题: 'Frequently asked questions',
  联系支持: 'Contact support',
  '关于 CodeGo': 'About CodeGo',
  隐私政策: 'Privacy policy',
  服务条款: 'Terms of service',
  退款规则: 'Refund policy',
  模型与价格: 'Models & pricing',
  查看模型与价格: 'Explore models & pricing',
  计费说明: 'Billing explained',
  渠道合作: 'Become a provider',
  公司信息: 'Company information',
  联系与合作: 'Contact & partnerships',
  '前往 CodeGo 社区': 'Visit the CodeGo community',
  提交问题前: 'Before asking for help',
  账单与发票: 'Billing & invoices',
  进入渠道工作台: 'Open channel workspace',
  查看全部问题: 'View all questions',
  了解分组选择: 'Understand model groups',
  分组选择: 'Choosing a group',
  套餐与余额: 'Plans & balance',
  客户端接入: 'Connect a client',
  '选模型，也选服务。': 'Choose a model. Choose your provider.',
  核对模型与价格: 'Compare models & prices',
  查看验证与探测: 'Check verification & probes',
  按访问条件接入: 'Check access requirements',
  '价格清楚，账单可查。': 'Clear pricing. Traceable usage.',
  '三步，开始调用。': 'Three steps to your first request.',
  创建账号: 'Create an account',
  准备额度与密钥: 'Add credit & create a key',
  发送第一条请求: 'Send your first request',
  '模型页查看定价，分组行情比较倍率。先确认计价单位，再选择适合用量的路线。':
    'Find prices on the models page and compare group multipliers in the market. Check the billing unit before choosing a route for your workload.',
  '结合模型验证结果、延迟和最近探测时间判断服务状态。探测结果反映检查时刻，不代表持续可用性保证。':
    'Compare model verification, latency and the latest probe time. A probe describes one check, not a guarantee of ongoing availability.',
  '公开分组供用户发现；私有分组需获得访问资格。密钥的分组和模型限制也会影响可调用范围。':
    'Public groups are discoverable; private groups require access. Your key’s group and model restrictions also determine what you can call.',
  '按模型计价规则、实际用量与分组倍率结算。余额与套餐分开管理，每次调用的用量和费用均可在控制台核对。':
    'Charges follow model pricing, actual usage and group multipliers. Manage balance and plans separately, and review each request’s usage and cost in the console.',
  '登录控制台，选择想使用的模型与分组。': 'Sign in to the console and choose your model and group.',
  '按需充值或选购套餐，再创建具有对应访问权限的 API Key。':
    'Top up or purchase a plan, then create an API key with the required access.',
  '配置接口地址与密钥，使用下方示例接入；也可以先在对话页尝试。':
    'Configure your endpoint and key using the examples below, or try the chat page first.',
  '按模型计价规则、用量与分组倍率结算': 'Billed by model pricing, usage and group multiplier',
  '在哪里查看模型价格？': 'Where can I find model prices?',
  '模型页提供模型定价；选择分组时还需核对倍率。实际费用由模型计价规则、用量和所选分组共同决定。':
    'The models page provides model prices. Check the multiplier when choosing a group. Your cost depends on model pricing, usage and the selected group.',
  '同一个模型，为什么有不同分组？': 'Why does one model have multiple groups?',
  '分组对应不同的上游服务和访问条件。比较价格时，也请查看模型验证结果、最近探测时间与延迟；单次探测不代表持续可用性保证。':
    'Groups represent different upstream services and access requirements. Compare verification, the latest probe time and latency alongside price. A single probe does not guarantee ongoing availability.',
  '余额、新套餐和旧套餐有什么区别？': 'How do balance, new plans and legacy plans differ?',
  '余额按调用费用扣除；新套餐按购买时的额度、有效期和适用分组使用，不再采用旧版十倍扣费。旧套餐可按原规则继续使用，符合条件且未到期的旧套餐可在钱包查看转余额报价。转换后对应权益不能再刷新。':
    'Balance is charged per request. New plans follow the credit, validity and groups shown at purchase, without the legacy tenfold charge. Legacy plans keep their original rules. Eligible unexpired plans can receive a balance-conversion quote in the wallet; converted benefits can no longer be reset.',
  查看套餐与余额规则: 'Read plan & balance rules',
  '调用失败或中断，会扣费吗？': 'Am I charged for failed or interrupted requests?',
  '输出前失败不计费；已经产生输出后中断，可能按已产生的用量结算。请用请求编号在使用日志中核对最终状态、用量和费用。':
    'Failures before output are not charged. Requests interrupted after output may be charged for usage already produced. Use the request ID to check final status, usage and cost in your logs.',
  查看错误处理: 'Read error handling guidance',
  '购买后如何下载香港商业发票？': 'How do I download a Hong Kong commercial invoice?',
  '在账单明细的已支付订单中填写个人姓名或公司全称及购买方地址，即可自助开具 PDF。首次开具后抬头与地址固定，后续直接下载；退款中或已退款的订单不可开具或下载。':
    'For a paid order in billing details, enter your full personal or company name and purchaser address to issue a PDF. These details are fixed after first issue. Download the archived PDF afterward. Orders with pending or completed refunds are ineligible.',
  前往账单明细: 'Open billing details',
  '未使用的额度可以退款吗？': 'Can I get a refund for unused credit?',
  '符合条件的已支付订单可在钱包查看可退额度、费用和预计退款金额。已消耗额度不退款，赠送额度不能直接提现；支付渠道和订单状态也会影响退款资格。':
    'Eligible paid orders show refundable credit, fees and an estimated refund in the wallet. Used credit is not refundable and promotional credit cannot be cashed out. Payment method and order status also affect eligibility.',
  查看退款规则: 'Read refund policy',
  '已有 OpenAI SDK 或客户端如何接入？': 'How do I connect an existing OpenAI SDK or client?',
  '将支持自定义接口地址的客户端配置为本站的 /v1 地址，并使用 CodeGo API Key。模型名以模型页为准；工具调用、图像和其他能力取决于所选模型与分组。':
    'Configure a client that supports a custom endpoint with this site’s /v1 URL and a CodeGo API key. Use model IDs from the models page. Tools, images and other capabilities depend on your model and group.',
  查看接入步骤: 'Read connection steps',
  '遇到问题，应该提供哪些信息？': 'What information should I provide for support?',
  '请提供发生时间、模型、分组、请求编号和错误信息。支付问题可补充订单编号。不要公开 API Key、密码、验证码或未经脱敏的对话内容。':
    'Provide the time, model, group, request ID and error. For payment issues, include the order number. Do not publish API keys, passwords, verification codes or unredacted conversations.',
  '从模型选择到第一笔账单。': 'From choosing a model to understanding your first bill.',
  '还没有找到答案？': 'Still need an answer?',
  '在 CodeGo 社区获取接入、账户和账单帮助。':
    'Get integration, account and billing help in the CodeGo community.',
  '请准备发生时间、模型、分组、请求编号和错误信息；支付问题请补充订单编号。服务异常可先查看服务状态。':
    'Have the time, model, group, request ID and error ready. Include the order number for payment issues. For outages, check service status first.',
  '公开帖子只提供脱敏信息。账户资料、付款凭证和其他个人信息请通过社区私信联系管理员；不要发送 API Key、密码或验证码。':
    'Only share redacted information in public posts. Send account details, payment receipts or personal information privately to a community administrator. Never send API keys, passwords or verification codes.',
  '提供上游模型服务的渠道主可在控制台提交渠道，填写模型、价格及访问条件，并完成验证与审核。渠道管理、收益和风控记录统一位于渠道工作台。':
    'Upstream service providers can submit a channel in the console with models, prices and access requirements for verification and review. Manage channels, earnings and risk records in the channel workspace.',
  '已支付订单的商业发票可在账单明细自助开具与下载。退款资格、费用和进度在钱包查看。':
    'Issue and download commercial invoices for paid orders in billing details. Check refund eligibility, fees and progress in the wallet.',
  '连接模型、开发者与服务提供者。': 'Connecting models, developers and providers.',
  'CodeGo AI 提供统一的模型 API 接入与渠道市场。开发者可以发现模型、比较分组、创建密钥，并在控制台核对用量和账单。':
    'CodeGo AI offers unified model API access and a channel marketplace. Developers discover models, compare groups, create keys and review usage and billing in the console.',
  看得见的选择: 'Informed choices',
  '公开分组展示倍率、声明模型、验证结果及最近探测信息。价格与服务状态是选择依据，实际能力以所选模型和分组为准。':
    'Public groups show multipliers, declared models, verification results and recent probes. Compare price and service status; actual capabilities depend on your model and group.',
  'CodeGo AI 由香港公司 CodeGo AI Limited（码高智能有限公司）运营。':
    'CodeGo AI is operated by CodeGo AI Limited (码高智能有限公司), a Hong Kong company.',
  '账户支持、产品反馈与渠道合作目前通过 CodeGo 社区联系。':
    'Account support, product feedback and provider partnerships are currently handled through the CodeGo community.',
  '本页说明 CodeGo AI Limited（码高智能有限公司）在提供网站、模型 API、支付与支持服务时如何处理信息。':
    'This page describes how CodeGo AI Limited (码高智能有限公司) handles information for its website, model API, payment and support services.',
  账户与交易信息: 'Account & transaction information',
  '我们处理你提供的账号资料、邮箱和外部登录标识，以完成登录、账户安全与服务通知。交易记录包括订单、支付状态、额度、退款及发票；开具发票时需要购买方姓名或公司全称与地址。支付由所选支付服务提供方处理。':
    'We process your account details, email and external sign-in identifiers for authentication, account security and service notices. Transaction records include orders, payment status, credit, refunds and invoices. Invoices require the purchaser’s full personal or company name and address. Payments are handled by your selected payment provider.',
  模型请求与上游服务: 'Model requests & upstream services',
  '为完成模型调用，请求内容和必要参数会发送给所选分组的上游服务。不同上游可能位于不同地区，并适用各自的数据处理规则。请确认有权提交相关内容，避免在请求中发送不必要的个人信息或商业秘密。':
    'To fulfill a model request, its content and necessary parameters are sent to the selected group’s upstream service. Providers may operate in different regions under their own data policies. Ensure you have the right to submit the content and avoid unnecessary personal information or trade secrets.',
  用量日志与诊断采样: 'Usage logs & diagnostic sampling',
  '用量与请求记录包括模型、分组、时间、状态、token 用量、费用及错误信息，用于结算、排障与安全管理。系统还支持诊断内容采样；启用时样本可能包含请求和响应内容，已识别的凭据字段会被脱敏。内容采样与普通用量日志是不同记录。':
    'Usage and request records include model, group, time, status, token usage, cost and errors for billing, troubleshooting and security. The system also supports diagnostic content sampling. When enabled, samples may contain request and response content with recognized credential fields redacted. Content samples are separate from ordinary usage logs.',
  浏览器存储: 'Browser storage',
  '网站使用登录会话及浏览器存储维持登录状态、语言、主题和部分本地偏好。社区与外部登录、支付服务有各自的存储和隐私规则。退出登录不会自动删除订单和账单记录。':
    'The site uses sessions and browser storage for sign-in, language, theme and local preferences. The community, external sign-in and payment services have separate storage and privacy policies. Signing out does not delete order or billing records.',
  信息保存与请求处理: 'Retention & information requests',
  '账户、订单、账单及发票信息用于服务履约、财务核对和适用法律要求。诊断样本的留存与清理由站点配置决定，本页不承诺固定自动删除期限。你可以联系支持申请核对、更正或删除个人信息；需先验证身份，依法必须保留的财务或安全记录可能无法立即删除。':
    'Account, order, billing and invoice information supports service delivery, financial reconciliation and applicable legal requirements. Diagnostic sample retention and cleanup depend on site configuration; no fixed automatic deletion period is promised here. Contact support to request access, correction or deletion of personal information. Identity verification is required, and financial or security records subject to retention requirements may not be deleted immediately.',
  联系与更新: 'Contact & updates',
  '如需确认当前内容采样、留存设置或提出隐私请求，请通过支持入口私信联系管理员。服务或数据处理方式变化时，本页会相应更新。':
    'Contact an administrator privately through support to confirm current content sampling and retention settings or submit a privacy request. This page will be updated when services or data handling change.',
  'CodeGo AI 由 CodeGo AI Limited（码高智能有限公司）运营。本页说明账户、模型接入与购买权益的使用规则；具体价格和权益以购买及使用页面显示的信息为准。':
    'CodeGo AI is operated by CodeGo AI Limited (码高智能有限公司). This page explains account, model access and purchased benefit rules. Specific prices and benefits are shown on purchase and usage pages.',
  账户与密钥安全: 'Account & key security',
  '请提供真实且有权使用的账户信息，妥善保管登录凭据与 API Key。密钥泄露后应立即停用并更换；不要将密钥放入公开代码或分享给未经授权的人员。':
    'Provide accurate account information you are authorized to use and protect your credentials and API keys. Disable and replace a compromised key immediately. Do not publish keys or share them with unauthorized people.',
  合理使用: 'Acceptable use',
  '你应有权提交请求内容，并遵守适用法律与所选上游服务的使用要求。不得利用服务侵害他人权益、窃取数据、绕过访问控制或干扰平台及其他用户的服务。':
    'You must have the right to submit content and comply with applicable laws and the chosen upstream service’s requirements. Do not infringe others’ rights, steal data, bypass access controls or disrupt the platform or other users.',
  '价格、额度与套餐': 'Prices, credit & plans',
  '模型定价、分组倍率和计价单位以模型及市场页面为准，支付币种与到账额度在付款前显示。套餐按购买时约定的额度、有效期和适用范围使用；旧套餐的使用、刷新与转换按对应规则执行。余额与套餐额度是平台服务权益，不是银行存款。':
    'Model prices, group multipliers and billing units are shown on model and market pages. Payment currency and credited amounts are shown before payment. Plans follow the credit, validity and scope agreed at purchase. Legacy usage, resets and conversions follow their respective rules. Balance and plan credit are service benefits, not bank deposits.',
  上游能力与可用性: 'Upstream capabilities & availability',
  '模型能力、输出和可用性受所选上游服务影响。验证及探测是特定时间的检查结果，不构成持续可用性或输出准确性保证。请自行核验模型输出，调用异常可通过请求编号核对日志并联系支持。':
    'Model capabilities, output and availability depend on the selected upstream service. Verification and probes reflect checks at a particular time, not a guarantee of ongoing availability or accurate output. Verify model output yourself. For request issues, check logs with your request ID and contact support.',
  退款与商业发票: 'Refunds & commercial invoices',
  '退款依据订单资格、未使用额度及退款报价处理。商业发票由符合条件的已支付订单自助开具，首次开具后购买方信息固定。详细规则见退款说明与常见问题。':
    'Refunds depend on order eligibility, unused credit and the refund quote. Eligible paid orders can issue commercial invoices, with purchaser details fixed after first issue. See the refund policy and FAQs for details.',
  异常使用与服务调整: 'Abuse & service changes',
  '为处理滥用、安全风险或上游异常，平台可能限制相关请求、密钥或渠道。模型、价格或规则调整将在相应页面说明；账户或订单争议请联系支持并提供可核对的记录。':
    'The platform may restrict requests, keys or channels to address abuse, security risks or upstream incidents. Model, price and rule changes will be described on the relevant pages. Contact support with verifiable records for account or order disputes.',
  '先查看报价，再确认退款。': 'Review your quote before confirming a refund.',
  哪些订单可以申请: 'Eligible orders',
  '符合条件的已支付订单且仍有可退的未使用额度，才可申请退款。当前自助退款支持易支付人民币订单；其他支付方式请联系支持。未支付、额度已用完、退款处理中或已完成退款的订单不能重复申请。':
    'Refunds require an eligible paid order with refundable unused credit. Self-service refunds currently support Epay CNY orders; contact support for other payment methods. Unpaid, fully used, pending-refund or already-refunded orders cannot submit another request.',
  退款金额如何计算: 'How the amount is calculated',
  '按订单实付金额与可退未使用额度的比例计算退款毛额，再扣除退款毛额的 2% 费用，按支付币种最小单位取整。最终金额以钱包显示的实时报价为准，已消耗的额度不计入退款。':
    'The gross refund is proportional to the amount paid and refundable unused credit, minus a fee of 2% of the gross refund, rounded to the payment currency’s smallest unit. The live wallet quote is authoritative. Used credit is excluded.',
  '赠送、邀请奖励或兑换获得的额度不能直接作为现金退款。转换后的额度仍需追溯原购买记录，余额总额不等于可退金额。':
    'Promotional, referral or redeemed credit cannot be directly refunded as cash. Converted credit still requires its original purchase record. Total balance is not the same as the refundable amount.',
  如何提交与查看进度: 'Submitting & tracking a refund',
  '在钱包查看可退款订单与报价，确认后系统预留对应额度并向支付渠道提交退款。处理中请勿重复申请；结果以退款记录和支付渠道确认为准，到账时间取决于支付渠道。':
    'Review eligible orders and quotes in the wallet. On confirmation, the system reserves the corresponding credit and submits the refund to the payment provider. Do not resubmit while processing. Refund records and provider confirmation determine the result; arrival time depends on the provider.',
  '套餐、转换与发票': 'Plans, conversions & invoices',
  '套餐退款还受有效期、已用额度及原购买记录限制。到期旧套餐不能转换为余额；符合条件的转换应先核对报价，转换后的对应权益不能再刷新。退款中或已退款的订单不能开具或下载商业发票。':
    'Plan refunds also depend on validity, usage and the original purchase record. Expired legacy plans cannot convert to balance. Check the quote before an eligible conversion; converted benefits can no longer be reset. Orders with pending or completed refunds cannot issue or download invoices.',
  退款异常: 'Refund issues',
  '如果退款失败或长时间没有更新，请携带订单编号、退款编号和发生时间联系支持。请勿在公开社区帖子中提交付款凭证或个人资料。':
    'If a refund fails or stops updating, contact support with the order number, refund number and time. Do not post payment receipts or personal information publicly.',
  '通过 OpenAI 兼容接口接入 CodeGo，模型能力以所选模型和分组为准。':
    'Connect to CodeGo through an OpenAI-compatible API. Capabilities depend on your selected model and group.',
  '输入、输出、缓存及其他用量按模型对应的计价规则计算，再结合分组倍率结算。模型页展示定价，市场展示分组条件，实际结算以使用日志为准。':
    'Input, output, cache and other usage follow each model’s pricing rules and group multiplier. Model pages show prices, the market shows group conditions, and usage logs show final charges.',
  'credits 是平台额度单位，精确到 0.000001 credit；支付币种、实付金额和到账额度以充值或套餐的确认页面为准，不能将 credits 直接视为某一种货币。':
    'Credits are platform units with 0.000001-credit precision. Check payment currency, amount paid and credited amount on the top-up or plan confirmation page. Credits are not directly equivalent to a particular currency.',
  '同一个模型可以由不同分组提供。公开分组可供发现，私有分组需获得访问资格；密钥允许的分组与模型范围决定实际访问权限。':
    'Different groups can offer the same model. Public groups are discoverable; private groups require access. Your key’s allowed groups and models determine actual access.',
  '验证结果表示最近一次模型检查的结果，延迟是该次探测耗时。请同时核对探测时间与具体模型状态，不能把一次验证通过理解为长期可用性或完整功能保证。':
    'Verification reflects the latest model check; latency measures that probe. Check the probe time and each model’s status. A passed check is not a guarantee of long-term availability or all capabilities.',
  '余额按实际调用费用扣除。新套餐按额度、有效期、适用分组及扣费偏好使用，不采用旧版十倍扣费，也不通过邀请发放刷新次数；购买前请核对套餐详情。':
    'Balance is charged for actual requests. New plans follow credit, validity, applicable groups and funding preferences, without legacy tenfold charges or referral resets. Review plan details before purchasing.',
  '旧套餐与已有刷新权益按原规则保留。符合转换条件且未到期的旧套餐可以在钱包查看转余额比例与报价；到期后不能转换，转换后的对应权益不能再刷新。刷新权益的兑换资格与结果以钱包展示为准。':
    'Legacy plans and existing reset benefits retain their original rules. Eligible unexpired legacy plans can show a conversion ratio and quote in the wallet. Expired plans cannot convert; converted benefits cannot be reset. The wallet shows reset-benefit exchange eligibility and results.',
  '选择支持自定义 OpenAI 兼容接口地址的 SDK 或客户端。':
    'Choose an SDK or client that supports a custom OpenAI-compatible endpoint.',
  '将 Base URL 设为本站的 /v1 地址；若客户端要求完整请求地址，再追加 /chat/completions。':
    'Set the Base URL to this site’s /v1 endpoint. Append /chat/completions only if the client requires the full request URL.',
  '填写 CodeGo API Key，并从模型页复制可用模型名，核对密钥分组权限。':
    'Enter your CodeGo API key, copy an available model ID from the models page and check your key’s group permissions.',
  '先发出一条短请求，再到使用日志核对状态和费用。工具调用、图像及其他能力需分别确认所选模型支持。':
    'Send a short request first, then check status and cost in usage logs. Verify support for tools, images and other capabilities separately.',
  'Python、Node.js SDK 和 cURL 的基本调用见上方示例。不要将其他平台的密钥填写为 CodeGo API Key。':
    'See the Python, Node.js SDK and cURL examples above. Do not use another platform’s key as your CodeGo API key.',
  '输出前失败不计费；已输出后中断请核对日志再决定是否重试':
    'No charge before output; after output, check logs before retrying',
}
export default messages
