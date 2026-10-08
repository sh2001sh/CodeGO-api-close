import type { Locale } from '../../lib/locales'

export const policyContentVersion = '2026-10-07'
export const companyAddress =
  'UNIT 1618A, 16/F, PIONEER CENTRE, 750 NATHAN ROAD, MONG KOK, HONG KONG'

export type PolicyLanguage = 'zh-CN' | 'zh-HK' | 'en'
type Copy = Record<PolicyLanguage, string>
export type PolicyDocumentID = 'terms' | 'privacy' | 'supplier' | 'market' | 'refund'
export interface PolicySection {
  id: string
  title: Copy
  paragraphs: Copy[]
}
export interface PolicyDocument {
  id: PolicyDocumentID
  href: string
  title: Copy
  introduction: Copy
  sections: PolicySection[]
}
const copy = (cn: string, hk: string, en: string): Copy => ({ 'zh-CN': cn, 'zh-HK': hk, en })
const section = (id: string, title: Copy, ...paragraphs: Copy[]): PolicySection => ({
  id,
  title,
  paragraphs,
})

export function policyLanguage(locale: Locale): PolicyLanguage {
  return locale === 'zh-CN' || locale === 'zh-HK' ? locale : 'en'
}

// A fallback is visible in the selected UI language; a missing translation is never presented
// as though the policy were written in that language.
export const policyInterface: Record<
  Locale,
  { version: string; contents: string; documents: string; support: string; fallback: string }
> = {
  'zh-CN': {
    version: '版本',
    contents: '本页目录',
    documents: '规则与政策',
    support: '联系支持',
    fallback: '',
  },
  'zh-HK': {
    version: '版本',
    contents: '本頁目錄',
    documents: '規則與政策',
    support: '聯絡支援',
    fallback: '',
  },
  en: {
    version: 'Version',
    contents: 'On this page',
    documents: 'Rules and policies',
    support: 'Contact support',
    fallback: '',
  },
  ja: {
    version: 'バージョン',
    contents: 'このページの内容',
    documents: '規則とポリシー',
    support: 'サポートに連絡',
    fallback:
      'この文書の日本語版はまだありません。英語版を表示しています。簡体字中国語・繁体字中国語・英語版はサイトの言語メニューから選択できます。',
  },
  ru: {
    version: 'Версия',
    contents: 'На этой странице',
    documents: 'Правила и политики',
    support: 'Связаться с поддержкой',
    fallback:
      'Русский перевод этого документа пока недоступен. Показана английская версия. В меню языка доступны английский, упрощённый и традиционный китайский.',
  },
  ko: {
    version: '버전',
    contents: '페이지 목차',
    documents: '규칙 및 정책',
    support: '지원 문의',
    fallback:
      '이 문서의 한국어 번역은 아직 제공되지 않습니다. 영어 버전을 표시합니다. 사이트 언어 메뉴에서 영어, 중국어 간체 또는 번체를 선택할 수 있습니다.',
  },
  fr: {
    version: 'Version',
    contents: 'Sur cette page',
    documents: 'Règles et politiques',
    support: 'Contacter le support',
    fallback:
      'La traduction française de ce document n’est pas encore disponible. La version anglaise est affichée. Le menu des langues permet de choisir l’anglais, le chinois simplifié ou traditionnel.',
  },
  de: {
    version: 'Version',
    contents: 'Auf dieser Seite',
    documents: 'Regeln und Richtlinien',
    support: 'Support kontaktieren',
    fallback:
      'Eine deutsche Übersetzung dieses Dokuments ist noch nicht verfügbar. Die englische Fassung wird angezeigt. Im Sprachmenü stehen Englisch sowie vereinfachtes und traditionelles Chinesisch zur Auswahl.',
  },
  ar: {
    version: 'الإصدار',
    contents: 'في هذه الصفحة',
    documents: 'القواعد والسياسات',
    support: 'التواصل مع الدعم',
    fallback:
      'الترجمة العربية لهذه الوثيقة غير متاحة بعد. تُعرض النسخة الإنجليزية. يمكن اختيار الإنجليزية أو الصينية المبسطة أو التقليدية من قائمة لغة الموقع.',
  },
}

export const policyDocuments: PolicyDocument[] = [
  {
    id: 'terms',
    href: '/terms',
    title: copy('服务条款', '服務條款', 'Terms of service'),
    introduction: copy(
      '使用 CodeGo 前，请了解谁提供服务、请求如何计费，以及平台与渠道主分别承担什么责任。',
      '使用 CodeGo 前，請了解由誰提供服務、請求如何計費，以及平台與渠道主各自的責任。',
      'Before using CodeGo, understand who supplies the service, how requests are billed, and the responsibilities of the platform and channel owners.',
    ),
    sections: [
      section(
        'operator',
        copy('1. 运营主体与适用范围', '1. 營運主體與適用範圍', '1. Operator and scope'),
        copy(
          'CodeGo AI 由香港公司 CodeGo AI Limited（码高智能有限公司）运营。本条款适用于网站、控制台、模型 API 和渠道市场。公司公开地址见本页末尾。社区有独立的内容规则；模型提供者和支付服务也可能有各自的条款。',
          'CodeGo AI 由香港公司 CodeGo AI Limited（碼高智能有限公司）營運。本條款適用於網站、控制台、模型 API 及渠道市場。公司公開地址見本頁末尾。社群有獨立的內容規則；模型提供者及支付服務亦可能有各自的條款。',
          'CodeGo AI is operated by CodeGo AI Limited, a Hong Kong company (码高智能有限公司). These terms cover the website, console, model API and channel marketplace. Our public address appears below. Community content rules and third-party model or payment terms may also apply.',
        ),
        copy(
          '注册时需要明确接受当前服务条款和隐私政策；提交或公开发布渠道还需要接受渠道供给协议。历史账户不被视为已接受从未确认的新版本，重要规则变化将在对应页面说明。',
          '註冊時需要明確接受現行服務條款及私隱政策；提交或公開發佈渠道亦需要接受渠道供給協議。舊有帳戶不會被視為已接受未曾確認的新版本，重要規則變動會在相關頁面說明。',
          'Registration requires explicit acceptance of the current terms and privacy policy. Submitting or publishing a channel also requires the supplier agreement. Existing accounts are not treated as having accepted a new version they never confirmed. Material changes are explained on the relevant pages.',
        ),
      ),
      section(
        'accounts',
        copy('2. 账户与密钥', '2. 帳戶與金鑰', '2. Accounts and API keys'),
        copy(
          '请使用有权使用的身份及邮箱，保护密码、验证码和 API Key。你应管理自己授权的人员、应用和密钥权限；密钥泄露时立即停用、更换并核对请求日志。不得出售他人账户、冒用身份或绕过权限、配额和风控限制。',
          '請使用有權使用的身份及電郵，保護密碼、驗證碼及 API Key。你應管理已授權的人員、應用程式及金鑰權限；金鑰外洩時須立即停用、更換並核對請求紀錄。不得出售他人帳戶、冒用身份或繞過權限、配額及風控限制。',
          'Use an identity and email address you are entitled to use. Protect passwords, verification codes and API keys, and manage the people and applications you authorize. Disable and replace a leaked key immediately and review request records. Do not impersonate others, sell accounts belonging to others, or bypass authorization, quotas or abuse controls.',
        ),
      ),
      section(
        'market-choice',
        copy(
          '3. 平台、渠道与模型选择',
          '3. 平台、渠道與模型選擇',
          '3. Platform, channels and model selection',
        ),
        copy(
          'CodeGo 提供统一接口、路由、账单和市场，渠道主提供所提交的上游接入。允许用户提交渠道不等于所有渠道自动公开上架。店铺、分组名称和厂商标签不代表厂商官方授权；“验证”或探测通过仅说明对应时间与测试条件下的连通结果。',
          'CodeGo 提供統一介面、路由、帳單及市場，渠道主提供所提交的上游接入。允許用戶提交渠道不等於全部渠道自動公開上架。店鋪、分組名稱及廠商標籤不代表廠商官方授權；「驗證」或探測通過只說明當時及相關測試條件下的連線結果。',
          'CodeGo provides a unified interface, routing, billing and marketplace; channel owners supply the upstream access they submit. Open submissions do not imply automatic public listing. Shop names, group names and provider tags do not establish official provider authorization. A passed verification or probe establishes connectivity only at that time and under those test conditions.',
        ),
        copy(
          '选择前请核对模型、计价单位、实际价格、能力声明、近期真实请求、数据政策和访问条件。同一模型在不同分组可能有不同能力与服务质量。未知、无样本或渠道主自报的信息不应当作平台认证；对敏感数据先确认所选上游符合你的要求。',
          '選擇前請核對模型、計價單位、實際價格、能力聲明、近期真實請求、資料政策及存取條件。同一模型在不同分組可能有不同能力及服務質素。未知、沒有樣本或渠道主自行申報的資訊不應視為平台認證；提交敏感資料前須確認所選上游符合你的要求。',
          'Review the model, pricing unit, effective price, capabilities, recent real requests, data policy and access conditions before choosing. The same model can behave differently across groups. Unknown values, missing samples and owner declarations are not platform certification. Check that the selected upstream meets your requirements before submitting sensitive data.',
        ),
      ),
      section(
        'billing',
        copy(
          '4. 价格、请求计费与购买权益',
          '4. 價格、請求計費與購買權益',
          '4. Prices, request billing and purchased entitlements',
        ),
        copy(
          '模型页和市场显示价格及计价单位；付款前显示支付币种和到账权益。市场的有效单价如已包含分组倍率，不应再乘一次。费用估算基于输入的用量假设，实际扣费依据所用模型、分组、计费来源、缓存、额外工具费用及可用用量记录。请求被取消、超时或部分输出时不一定免费，请以请求账单和终态核对。',
          '模型頁及市場顯示價格與計價單位；付款前顯示支付幣種及到帳權益。市場的有效單價如已包含分組倍率，不應再次相乘。費用估算基於輸入的用量假設，實際扣費依據所用模型、分組、計費來源、快取、額外工具費用及可用用量紀錄。取消、逾時或部分輸出的請求不一定免費，請按請求帳單及終態核對。',
          'Model and marketplace pages show prices and units; checkout shows the payment currency and purchased entitlements. Do not apply a group multiplier again when it is already included in the effective price. Estimates depend on your usage assumptions. Actual charges depend on the model, group, billing source, cache usage, additional tool fees and available usage records. Cancellation, timeout or partial output does not necessarily mean a free request; inspect its bill and terminal state.',
        ),
        copy(
          '余额和套餐额度是站内服务权益，不是银行存款。套餐按购买时的有效期、额度和范围使用；仍保留的旧套餐、刷新次数和已有倍率卡按对应权益规则执行。到期旧套餐不能转换；符合条件的旧套餐转换后，相应权益不能继续刷新。新套餐不再附赠倍率卡。',
          '餘額及套餐額度是站內服務權益，並非銀行存款。套餐按購買時的有效期、額度及範圍使用；仍保留的舊套餐、刷新次數及已有倍率卡按相應權益規則執行。已到期的舊套餐不能轉換；符合條件的舊套餐轉換後，相關權益不能繼續刷新。新套餐不再附送倍率卡。',
          'Balances and plan credits are service entitlements, not bank deposits. Plans follow the validity, allowance and scope recorded at purchase. Retained legacy plans, refresh rights and existing multiplier cards follow their applicable rules. Expired legacy plans cannot be converted; converting an eligible plan ends refresh rights for the converted entitlement. New plans no longer include multiplier cards.',
        ),
      ),
      section(
        'use',
        copy('5. 内容与合理使用', '5. 內容與合理使用', '5. Content and acceptable use'),
        copy(
          '你必须有权提交输入内容并遵守适用法律与上游使用规则。不得侵害隐私或知识产权，传播违法内容，实施欺诈、恶意攻击、凭据盗用、刷量、刷评价或规避安全控制。不得利用名称、备注、模型名、店铺或政策链接发布广告、联系方式、招揽交易或违法内容。',
          '你必須有權提交輸入內容並遵守適用法律及上游使用規則。不得侵害私隱或知識產權、散播違法內容、欺詐、惡意攻擊、盜用憑證、刷量、刷評價或規避安全控制。不得利用名稱、備註、模型名稱、店鋪或政策連結發佈廣告、聯絡方式、招攬交易或違法內容。',
          'You must have the right to submit your input and comply with applicable law and upstream use rules. Do not violate privacy or intellectual property, distribute unlawful content, commit fraud, attack systems, misuse credentials, manipulate traffic or ratings, or bypass safety controls. Names, remarks, model names, shops and policy links must not be used for advertising, contact details, off-platform solicitation or unlawful content.',
        ),
        copy(
          '模型输出可能错误、不完整或不适合专业决策。你应核验输出并对自己的使用负责；公开模型名称不构成医疗、法律、投资等专业保证。',
          '模型輸出可能錯誤、不完整或不適合專業決策。你應核實輸出並對自己的使用負責；公開模型名稱並不構成醫療、法律、投資等專業保證。',
          'Model output may be inaccurate, incomplete or unsuitable for a professional decision. Validate it and take responsibility for how you use it. A listed model name is not a medical, legal or investment assurance.',
        ),
      ),
      section(
        'availability',
        copy(
          '6. 可用性、安全措施与争议',
          '6. 可用性、安全措施與爭議',
          '6. Availability, safety measures and disputes',
        ),
        copy(
          '服务受上游限制、维护、网络和容量影响。除非另有明确书面约定，本页面不提供固定 SLA、持续可用性或输出准确性承诺。平台可因安全、滥用、授权或服务质量问题限制有关密钥、请求、渠道或公开展示；处理范围应与问题有关。',
          '服務受上游限制、維護、網絡及容量影響。除非另有明確書面約定，本頁不提供固定 SLA、持續可用性或輸出準確性承諾。平台可因安全、濫用、授權或服務質素問題限制相關金鑰、請求、渠道或公開展示；處理範圍應與問題相關。',
          'Upstream restrictions, maintenance, networks and capacity affect availability. Unless expressly agreed in writing, this page offers no fixed SLA or assurance of continuous availability or accurate output. CodeGo may restrict affected keys, requests, channels or listings to address safety, abuse, authorization or service quality issues, with measures tied to the issue.',
        ),
        copy(
          '账单、账户或服务争议请通过支持入口提交时间、模型、数字分组 ID、请求编号或订单编号。不要公开凭据或付款资料。我们根据可核对的日志和订单处理；退款条件见退款规则。本条款不排除适用法律赋予且不能排除的权利。',
          '帳單、帳戶或服務爭議請透過支援入口提交時間、模型、數字分組 ID、請求編號或訂單編號。不要公開憑證或付款資料。我們按可核對的紀錄及訂單處理；退款條件見退款規則。本條款不排除適用法律賦予且不能排除的權利。',
          'For billing, account or service disputes, use support and provide the time, model, numeric group ID, request ID or order ID. Never publish credentials or payment information. We assess verifiable records and orders; refund conditions appear in the refund policy. These terms do not exclude rights that applicable law does not allow us to exclude.',
        ),
      ),
    ],
  },
  {
    id: 'privacy',
    href: '/privacy',
    title: copy('隐私政策', '私隱政策', 'Privacy policy'),
    introduction: copy(
      '了解账户、支付、模型请求和诊断信息的处理方式。上游数据政策因渠道而异；选择渠道也是选择数据处理路径。',
      '了解帳戶、支付、模型請求及診斷資訊的處理方式。上游資料政策因渠道而異；選擇渠道亦是選擇資料處理路徑。',
      'Understand how account, payment, model request and diagnostic information is handled. Upstream policies vary by channel; choosing a channel also chooses a data-processing path.',
    ),
    sections: [
      section(
        'identity',
        copy('1. 处理主体与联系', '1. 處理主體與聯絡', '1. Operator and contact'),
        copy(
          'CodeGo AI Limited（码高智能有限公司）负责平台侧的信息处理。公司位于香港，地址见本页末尾。账户支持和隐私请求目前通过支持页进入 CodeGo 社区，私信管理员并说明请求类型。我们不要求你在公开帖子披露完整个人资料。',
          'CodeGo AI Limited（碼高智能有限公司）負責平台側的資訊處理。公司位於香港，地址見本頁末尾。帳戶支援及私隱要求目前透過支援頁進入 CodeGo 社群，私訊管理員並說明要求類型。我們不要求你在公開帖子披露完整個人資料。',
          'CodeGo AI Limited (码高智能有限公司) handles information on the platform side. Our Hong Kong address appears below. For account support or privacy requests, enter the CodeGo community through the support page and privately contact an administrator with the type of request. Do not publish full personal information.',
        ),
      ),
      section(
        'account-payment',
        copy(
          '2. 账户、交易与协议记录',
          '2. 帳戶、交易與協議紀錄',
          '2. Accounts, transactions and agreement records',
        ),
        copy(
          '我们处理你提供的账号、邮箱、外部登录标识、账户安全及登录记录，以完成认证、安全检查和服务通知。订单、支付状态、额度、退款及商业发票用于履约和财务核对；发票需要购买方姓名或公司全称与地址。支付服务提供方处理你在其页面提交的支付信息，并适用其政策。',
          '我們處理你提供的帳戶、電郵、外部登入識別、帳戶安全及登入紀錄，用於認證、安全檢查及服務通知。訂單、支付狀態、額度、退款及商業發票用於履約及財務核對；發票需要買方姓名或公司全稱與地址。支付服務提供方處理你在其頁面提交的支付資訊，並適用其政策。',
          'We process account details, email addresses, external sign-in identifiers, account security and sign-in records for authentication, safety checks and service notices. Orders, payment states, credits, refunds and commercial invoices support fulfillment and financial reconciliation. Invoices require the buyer’s name or full company name and address. Payment providers handle the payment information submitted on their own pages under their policies.',
        ),
        copy(
          '明确接受协议时保存本人、文档类型、版本、接受时间和语言，用于核对所确认的规则；不会把旧用户批量标记为接受新协议。',
          '明確接受協議時會保存本人、文件類型、版本、接受時間及語言，以核對已確認的規則；不會批量將舊用戶標記為接受新協議。',
          'Explicit agreement acceptance records identify the user, document, version, time and language so we can verify the accepted rules. Existing users are not bulk-marked as accepting new agreements.',
        ),
      ),
      section(
        'requests',
        copy(
          '3. 模型请求、上游与跨境处理',
          '3. 模型請求、上游與跨境處理',
          '3. Model requests, upstreams and cross-border processing',
        ),
        copy(
          '完成模型调用需要向所选渠道的上游发送输入、消息、文件或图片引用及必要参数，并接收输出。启用路由池和失败重试可能将同一请求发送给多个符合配置的上游；请按最严格的数据要求选择池成员。请求通过香港公司运营的平台不等于数据仅在香港处理。',
          '完成模型調用需要向所選渠道的上游傳送輸入、訊息、檔案或圖片引用及必要參數，並接收輸出。啟用路由池及失敗重試可能將同一請求傳送予多個符合設定的上游；請按最嚴格的資料要求選擇池成員。請求經由香港公司營運的平台，並不代表資料只在香港處理。',
          'Model calls send input, messages, file or image references and necessary parameters to the selected upstream and receive output. Route pools and retries may send a request to multiple upstreams allowed by your configuration; choose pool members against your strictest data requirements. Operation by a Hong Kong company does not mean that data is processed only in Hong Kong.',
        ),
        copy(
          '渠道主的数据地区、留存和训练声明属于自报，未知应按未确认处理。CodeGo 不凭渠道名称、标签或连通测试承诺上游零留存、不训练或授权来源。请勿提交你无权分享的资料；对个人信息、商业秘密和受监管数据，先核实上游要求或联系支持。',
          '渠道主申報的資料地區、保存及訓練政策屬自行申報，未知應視為未確認。CodeGo 不會憑渠道名稱、標籤或連線測試承諾上游零保存、不訓練或已獲授權。請勿提交你無權分享的資料；涉及個人資料、商業機密及受規管資料時，須先核實上游要求或聯絡支援。',
          'An owner’s region, retention and training disclosures are self-declarations; unknown means unconfirmed. CodeGo does not establish zero retention, no training or authorized provenance from a name, tag or connectivity test. Do not submit data you have no right to share. Verify upstream requirements before sending personal, confidential or regulated data.',
        ),
      ),
      section(
        'logs',
        copy(
          '4. 请求记录与诊断样本',
          '4. 請求紀錄與診斷樣本',
          '4. Request records and diagnostic samples',
        ),
        copy(
          '常规记录包括请求编号、模型、分组、时间、状态、错误分类、用量、费用及可测量性能，用于结算、路由质量、安全和排障。公开市场只展示汇总与公开资料，不提供其他用户的私有请求正文。渠道主可核对与自己渠道有关的经营记录，不因此获得用户账户或 API Key。',
          '常規紀錄包括請求編號、模型、分組、時間、狀態、錯誤分類、用量、費用及可量度性能，用於結算、路由質素、安全及排障。公開市場只展示匯總及公開資料，不提供其他用戶的私人請求正文。渠道主可核對與自身渠道相關的營運紀錄，但不會因此取得用戶帳戶或 API Key。',
          'Routine records include request IDs, models, groups, times, states, error categories, usage, charges and measurable performance for billing, routing quality, safety and troubleshooting. Public market views expose aggregates and public information, not other users’ private request bodies. Owners can reconcile records associated with their channels; that does not grant access to user accounts or API keys.',
        ),
        copy(
          '系统支持独立的诊断内容采样。开启采样时，样本可能包含请求和响应正文，已识别凭据字段会脱敏；脱敏不保证识别全部个人信息或秘密。采样是否开启、比例、访问权限和清理设置受站点配置控制，请通过支持确认当前设置，不能把“普通日志”理解成“任何情况下都不存正文”。',
          '系統支援獨立的診斷內容抽樣。啟用時，樣本可能包含請求及回應正文，已識別的憑證欄位會遮罩；遮罩不保證識別全部個人資料或秘密。是否啟用、比例、存取權限及清理設定由站點設定控制，請透過支援確認現行設定，不能把「普通紀錄」理解為「任何情況都不保存正文」。',
          'Separate diagnostic content sampling is supported. When enabled, samples may include request and response bodies; recognized credential fields are redacted, which cannot guarantee removal of every personal detail or secret. Sampling enablement, rate, access and cleanup depend on site configuration. Ask support for the current settings; routine metadata logging does not mean bodies can never be stored.',
        ),
      ),
      section(
        'sharing-storage',
        copy(
          '5. 公开信息、服务提供方与浏览器存储',
          '5. 公開資訊、服務提供方與瀏覽器儲存',
          '5. Public information, service providers and browser storage',
        ),
        copy(
          '你选择公开的店铺、渠道名称、备注、声明和评价可在市场展示；评分摘要由主站提供给社区并在评分变化时同步。不要把私人联系方式或敏感资料写入公开字段。认证、支付、托管与上游处理所需信息按对应服务流程传输，不代表所有个人资料都向渠道主或社区公开。',
          '你選擇公開的店鋪、渠道名稱、備註、聲明及評價可在市場展示；評分摘要由主站提供予社群，並在評分變更時同步。不要在公開欄位填寫私人聯絡方式或敏感資料。認證、支付、託管及上游處理所需資訊按相關流程傳送，不代表全部個人資料均向渠道主或社群公開。',
          'Shop details, channel names, remarks, disclosures and reviews you publish may appear in the market. The main site supplies rating summaries to the community and synchronizes rating changes. Do not place private contact details or sensitive data in public fields. Authentication, payment, hosting and upstream processes transmit the information they require; this is not blanket disclosure of personal data to owners or the community.',
        ),
        copy(
          '网站使用会话和浏览器存储维持登录、语言、主题与本地偏好。你可以清除本地数据，但这可能退出登录或重置设置，不会删除服务端账单。社区、第三方登录及支付页面适用各自的存储规则。',
          '網站使用工作階段及瀏覽器儲存維持登入、語言、主題及本機偏好。你可清除本機資料，但可能會登出或重設設定，並不會刪除伺服器端帳單。社群、第三方登入及支付頁面適用各自的儲存規則。',
          'Sessions and browser storage maintain sign-in, language, theme and local preferences. Clearing local data can sign you out or reset preferences but does not remove server-side bills. Community, external sign-in and payment pages follow their own storage rules.',
        ),
      ),
      section(
        'retention-rights',
        copy(
          '6. 保存、更正与删除请求',
          '6. 保存、更正與刪除要求',
          '6. Retention, correction and deletion requests',
        ),
        copy(
          '账户、订单、账单、发票和安全记录按履约、财务核对及适用法律要求保存；诊断样本的留存与清理由当前配置决定。本页不承诺统一固定期限或零留存。限制权限、凭据加密与脱敏用于降低风险，不构成绝对安全保证。',
          '帳戶、訂單、帳單、發票及安全紀錄按履約、財務核對及適用法律要求保存；診斷樣本的保存及清理由現行設定決定。本頁不承諾統一固定期限或零保存。權限限制、憑證加密及遮罩有助降低風險，但不構成絕對安全保證。',
          'Account, order, billing, invoice and security records are retained for fulfillment, reconciliation and applicable legal obligations. Diagnostic retention and cleanup depend on current configuration. This page promises neither a uniform retention period nor zero retention. Access controls, credential encryption and redaction reduce risks but are not an absolute security guarantee.',
        ),
        copy(
          '你可通过支持申请查阅、核对、更正或删除个人信息，撤回可撤回的授权并了解影响。我们先验证身份和请求范围；依法必须保留的财务、安全或争议记录可能不能立即删除。账户删除与订单退款是不同事项，也不会自动删除已经发送给独立上游的数据。隐私政策变化时会更新版本；重大处理变化会在有关页面说明。',
          '你可透過支援申請查閱、核對、更正或刪除個人資料，撤回可撤回的授權並了解影響。我們會先核實身份及要求範圍；依法須保留的財務、安全或爭議紀錄可能無法即時刪除。帳戶刪除與訂單退款是不同事項，亦不會自動刪除已傳送予獨立上游的資料。私隱政策變更時會更新版本；重大處理變動會在相關頁面說明。',
          'Use support to request access, review, correction or deletion of personal information, or to withdraw revocable permissions and understand the consequences. We verify identity and scope first. Financial, security or dispute records required by law may not be immediately deletable. Account deletion is separate from refunds and does not automatically delete data already sent to independent upstreams. Policy versions are updated when processing changes, with material changes explained on relevant pages.',
        ),
      ),
    ],
  },
  {
    id: 'supplier',
    href: '/supplier-agreement',
    title: copy(
      '渠道供给与结算协议',
      '渠道供給與結算協議',
      'Channel supply and settlement agreement',
    ),
    introduction: copy(
      '适用于提交与运营渠道的账户。开放供给需要可追溯的服务、准确的披露和可核对的结算。',
      '適用於提交及營運渠道的帳戶。開放供給需要可追溯的服務、準確的披露及可核對的結算。',
      'For accounts submitting and operating channels. Open supply requires accountable service, accurate disclosures and reconcilable settlements.',
    ),
    sections: [
      section(
        'eligibility',
        copy('1. 提交资格与公开发布', '1. 提交資格與公開發佈', '1. Eligibility and publication'),
        copy(
          '你须拥有或已获得提供该上游服务的合法权限，遵守上游条款并能够维护服务。任何用户都可以按平台入口提交渠道，但连通测试、名称审核、风控和公开展示要求仍适用；平台不因接收提交而确认你的来源或授权。首次提交或改为公开发布前，需要明确接受当前供给协议。',
          '你須擁有或已取得提供該上游服務的合法權限，遵守上游條款並能維護服務。任何用戶均可按平台入口提交渠道，但連線測試、名稱審核、風控及公開展示要求仍然適用；平台接收提交並不代表確認你的來源或授權。首次提交或改為公開發佈前，需要明確接受現行供給協議。',
          'You must own or be authorized to supply the upstream service, comply with upstream terms and maintain it. Any user can submit through the platform, subject to connectivity tests, name review, abuse controls and publication requirements. Receiving a submission does not certify its origin or authorization. Explicit acceptance of the current supplier agreement is required before first submission or public publication.',
        ),
      ),
      section(
        'truth',
        copy(
          '2. 模型、价格与声明真实性',
          '2. 模型、價格與聲明真確性',
          '2. Accurate models, prices and disclosures',
        ),
        copy(
          '维护实际可用模型、计价单位、倍率、访问条件、容量、维护窗口和凭据。不得用无权供应的模型名称误导用户，不得把兼容接口、转售或自托管伪装成厂商官方直连。模型能力、来源类型、处理地区、留存及训练声明必须符合你掌握的事实；不确定就标为未知，变更后及时更新。',
          '維護實際可用模型、計價單位、倍率、存取條件、容量、維護時段及憑證。不得以無權供應的模型名稱誤導用戶，不得把相容介面、轉售或自行託管偽裝成廠商官方直連。模型能力、來源類型、處理地區、保存及訓練聲明必須符合已知事實；未能確定就標示未知，變動後須及時更新。',
          'Maintain available models, units, multipliers, access conditions, capacity, maintenance windows and credentials. Do not mislead with model names you cannot legitimately supply, or present compatible interfaces, resale or self-hosting as an official direct connection. Capability, source, processing region, retention and training disclosures must reflect known facts. Mark uncertain information as unknown and update changes promptly.',
        ),
        copy(
          '自定义店铺名、分组名和备注不能改变数字 ID，也不得包含广告、联系方式、外部招揽、假冒官方、违法或侵权内容。厂商标签只用于发现，不是来源证明。你不能把自报标为平台已核验；平台审核后才公开的名称，以当前已发布版本为准。',
          '自訂店鋪名、分組名及備註不會改變數字 ID，亦不得包含廣告、聯絡方式、外部招攬、冒充官方、違法或侵權內容。廠商標籤只供搜尋，並非來源證明。你不能把自行申報標示為平台已核實；需經審核才公開的名稱，以現行已發佈版本為準。',
          'Custom shop names, group names and remarks do not change numeric IDs and must not contain advertising, contact details, off-platform solicitation, official impersonation, unlawful or infringing content. Provider tags aid discovery, not provenance verification. You cannot relabel a self-declaration as platform-verified. Names requiring review remain subject to the currently published version.',
        ),
      ),
      section(
        'data-security',
        copy('3. 凭据与用户数据', '3. 憑證與用戶資料', '3. Credentials and user data'),
        copy(
          '只提交有权使用的上游凭据，不得提交盗用、公开泄露或违反供应权限的凭据。渠道凭据不会通过公开市场展示；你仍负责上游账号安全、权限和更换。不得保存、转售或利用消费者的请求内容与个人信息从事未披露或未经授权的用途，不得用请求内容向用户发广告。',
          '只可提交有權使用的上游憑證，不得提交盜用、公開外洩或違反供應權限的憑證。渠道憑證不會在公開市場展示；你仍須負責上游帳戶安全、權限及更換。不得保存、轉售或利用消費者的請求內容及個人資料作未披露或未獲授權用途，亦不得利用請求內容向用戶發送廣告。',
          'Submit only upstream credentials you are entitled to use, never stolen, leaked or unauthorized credentials. Credentials are not displayed in the public market; you remain responsible for upstream account safety, permissions and rotation. Do not retain, resell or use consumer content or personal data for undisclosed or unauthorized purposes, or use request content to advertise to users.',
        ),
        copy(
          '上游的数据政策和跨境处理需如实披露。发现凭据泄露、服务替换、模型不符或数据安全事件时，应停止受影响供给并通过支持说明影响、时间和处理措施；不要在公开帖子发布凭据或用户正文。',
          '上游資料政策及跨境處理須如實披露。發現憑證外洩、服務替換、模型不符或資料安全事件時，應停止受影響供給，並透過支援說明影響、時間及處理措施；不要在公開帖子發佈憑證或用戶正文。',
          'Disclose upstream data policies and cross-border processing accurately. If credentials leak, services are substituted, models mismatch or a data incident occurs, stop the affected supply and report its scope, timing and mitigation through support. Never publish credentials or user bodies.',
        ),
      ),
      section(
        'settlement',
        copy(
          '4. 收入计算与结算口径',
          '4. 收入計算與結算口徑',
          '4. Income and settlement accounting',
        ),
        copy(
          '结算以实际请求与账本记录为准，工作台分别列出用户消费、渠道毛收入、平台佣金、其他费用及渠道净收入；这些金额可能因计费来源和权益不同而不同。净收入尚未扣除你的上游采购成本，因此不是利润。每笔结算按请求记录核对，不凭流量或模型宣称直接产生收入。',
          '結算以實際請求及帳本紀錄為準，工作台分別列出用戶消費、渠道毛收入、平台佣金、其他費用及渠道淨收入；因計費來源及權益不同，這些金額可能不同。淨收入尚未扣除你的上游採購成本，因此並非利潤。每筆結算按請求紀錄核對，不能只憑流量或模型聲稱便產生收入。',
          'Settlements follow actual requests and ledger entries. The workspace separates consumer spend, channel gross income, platform commission, other fees and channel net income; amounts can differ by billing source and entitlements. Net income excludes your upstream procurement cost and is not profit. Reconcile each settlement against its request; claimed traffic or model listings alone do not earn income.',
        ),
        copy(
          '佣金、费用、保留期和可释放时间以当前规则及逐笔结算记录为准。本页不把某次默认配置写成永久费率或固定到账保证。待结算收入需经过保留期和必要核对；符合释放条件后转入你的站内钱包，页面展示的可用时间不是现金到账时间。当前站内结算不代表提供银行卡或现金提现。',
          '佣金、費用、保留期及可釋放時間以現行規則及逐筆結算紀錄為準。本頁不會把某次預設設定寫成永久費率或固定到帳保證。待結算收入須經保留期及必要核對；符合釋放條件後轉入你的站內錢包，頁面顯示的可用時間並非現金到帳時間。現行站內結算不代表提供銀行卡或現金提款。',
          'Commission, fees, hold periods and availability follow current rules and per-settlement records. A default configuration is not a permanent rate or guaranteed arrival time. Pending income passes its hold and necessary reconciliation before release to your platform wallet. Availability timestamps are not cash-arrival timestamps; platform settlement does not offer bank or cash withdrawal.',
        ),
        copy(
          '请求重复、计费更正、退款、违规供给或争议可能导致有关结算暂停、核对或追回。处理应关联可核对的请求和账本；已释放收入也可能存在对应追回记录。你可提供请求编号与结算编号申请复核，不应把“已释放”解释为任何情况下都不可更正。',
          '重複請求、計費更正、退款、違規供給或爭議可能導致相關結算暫停、核對或追回。處理應關聯可核對的請求及帳本；已釋放收入亦可能有對應追回紀錄。你可提供請求編號及結算編號申請覆核，不應把「已釋放」理解為任何情況均不可更正。',
          'Duplicate requests, billing corrections, refunds, non-compliant supply or disputes may require affected settlements to be held, reviewed or reclaimed against traceable requests and ledger entries. Released income can have subsequent reclamation entries. Request review with request and settlement IDs; released does not mean immune to correction.',
        ),
      ),
      section(
        'operation',
        copy(
          '5. 服务维护、排名与停止供给',
          '5. 服務維護、排名與停止供給',
          '5. Maintenance, ranking and ending supply',
        ),
        copy(
          '及时维护模型与凭据、设置容量和维护窗口，处理错误与异常。不得刷请求、诱导好评、自评、串谋评分或通过更名掩盖质量记录。平台可按核实的问题限制公开展示、暂停供给或处理相关收益；评价和排名规则见市场规则。',
          '請及時維護模型與憑證、設定容量及維護時段，處理錯誤及異常。不得刷請求、誘導好評、自評、串謀評分或透過更名掩蓋質素紀錄。平台可按已核實問題限制公開展示、暫停供給或處理相關收益；評價及排名規則見市場規則。',
          'Maintain models and credentials, configure capacity and maintenance, and address errors. Do not manipulate traffic, induce favorable reviews, self-review, coordinate ratings or rename to conceal quality history. Verified issues may lead to listing restrictions, suspension or related income handling. Review and ranking rules are in the marketplace rules.',
        ),
        copy(
          '你可停用渠道以停止新供给；停用或删除不会自动抹除历史请求、结算或争议责任。合作结束后仍按原请求核对历史账本，并处理尚未完成的结算与争议。联系支持请提供数字渠道 ID 和可核对证据，敏感材料通过私信提交。',
          '你可停用渠道以停止新供給；停用或刪除不會自動抹除歷史請求、結算或爭議責任。合作結束後仍按原請求核對歷史帳本，並處理未完成的結算及爭議。聯絡支援時請提供數字渠道 ID 及可核對證據，敏感材料透過私訊提交。',
          'Disable a channel to stop new supply. Disabling or deleting does not erase historical requests, settlements or dispute responsibilities. Historical accounting and outstanding settlements or disputes still follow the original requests after supply ends. Contact support with numeric channel IDs and verifiable evidence; submit sensitive materials privately.',
        ),
      ),
    ],
  },
  {
    id: 'market',
    href: '/market-rules',
    title: copy(
      '市场治理、评价与排名规则',
      '市場治理、評價與排名規則',
      'Marketplace, review and ranking rules',
    ),
    introduction: copy(
      '价格、质量、评分和声明提供不同证据。了解各项指标的来源和边界，再选择适合自己的渠道。',
      '價格、質素、評分及聲明提供不同證據。先了解各項指標的來源及界限，再選擇合適渠道。',
      'Price, quality, ratings and disclosures provide different evidence. Understand their sources and limits before selecting a channel.',
    ),
    sections: [
      section(
        'identity',
        copy(
          '1. 数字 ID、店铺与分组',
          '1. 數字 ID、店鋪與分組',
          '1. Numeric IDs, shops and groups',
        ),
        copy(
          '数字渠道与分组 ID 用于识别和核对；自定义展示名称不会改变 ID。店铺用于查看同一渠道主的公开供给，分组用于选择具体价格、访问条件和路由。私人或仅授权分组不会因为所属店铺公开而自动公开。',
          '數字渠道及分組 ID 用於識別及核對；自訂展示名稱不會改變 ID。店鋪用於查看同一渠道主的公開供給，分組用於選擇具體價格、存取條件及路由。私人或僅獲授權的分組不會因所屬店鋪公開而自動公開。',
          'Numeric channel and group IDs identify records regardless of display-name changes. Shops collect an owner’s public supply; groups specify prices, access and routing. A public shop does not make private or restricted groups public.',
        ),
      ),
      section(
        'evidence',
        copy(
          '2. 声明、探测与真实请求',
          '2. 聲明、探測與真實請求',
          '2. Declarations, probes and real requests',
        ),
        copy(
          '模型列表、厂商标签、来源类型及数据政策可能来自渠道主声明。连通探测反映一次测试，不是授权证明、模型身份认证或长期质量保证。真实请求指标来自记录的请求结果；没有样本、没有测量或不足以比较时，应显示未知或观察中，而不是虚构 100% 成功率或速度。',
          '模型清單、廠商標籤、來源類型及資料政策可能來自渠道主聲明。連線探測反映一次測試，並非授權證明、模型身份認證或長期質素保證。真實請求指標來自已記錄的結果；沒有樣本、沒有量度或不足以比較時，應顯示未知或觀察中，不應虛構 100% 成功率或速度。',
          'Model lists, provider tags, source types and data policies may be owner declarations. Connectivity probes are individual tests, not authorization evidence, model identity certification or long-term quality assurances. Real-request metrics come from recorded outcomes. Missing or insufficient samples and unmeasured performance must remain unknown or under observation, not fabricated 100% success or speed.',
        ),
      ),
      section(
        'ranking',
        copy('3. 服务质量排名', '3. 服務質素排名', '3. Service quality ranking'),
        copy(
          '当前分组质量排名基于近期 24 小时的已记录请求，使用成功率的 Wilson 下界降低少量样本偶然成功带来的偏差；它是保守可靠性指标，不是模型回答质量得分。系统标记为不计入成功率的请求不会计入该统计，具体失败分类应与请求记录核对。',
          '現行分組質素排名按近期 24 小時已記錄的請求，使用成功率的 Wilson 下界，減少少量樣本偶然成功造成的偏差；這是保守的可靠性指標，並非模型回答質素分數。系統標記為不計入成功率的請求不會計入該統計，具體失敗分類應按請求紀錄核對。',
          'Current group quality ranking uses recorded requests from the last 24 hours and the Wilson lower bound of success rate to reduce small-sample bias. It estimates reliability conservatively, not answer quality. Requests marked as excluded from success-rate accounting do not enter that statistic; reconcile failure categories against request records.',
        ),
        copy(
          '当前少于 10 条计入统计的请求处于观察状态。排名是带计算时间的快照，不是每次请求后的实时保证；同模型比较应使用相同时间窗，不要把整个分组的跨模型结果当作特定模型表现。价格、独立消费者数量、首字延迟和生成速度需要分别查看。',
          '現行少於 10 條計入統計的請求屬觀察狀態。排名是附計算時間的快照，並非每次請求後的即時保證；比較同一模型時應使用相同時間窗，不要把整個分組跨模型的結果當作特定模型表現。價格、獨立消費者數目、首字延遲及生成速度須分別查看。',
          'Fewer than 10 counted requests currently place a group under observation. Ranking is a timestamped snapshot, not a live guarantee after every request. Compare the same model over the same window rather than interpreting cross-model group results as model-specific performance. Review price, independent consumer count, time to first token and generation speed separately.',
        ),
      ),
      section(
        'reviews',
        copy(
          '4. 消费者评价与店铺评分',
          '4. 消費者評價與店鋪評分',
          '4. Consumer reviews and shop ratings',
        ),
        copy(
          '评价在主站提交，须满足真实使用资格，不能评价自己的渠道；每个用户对每个渠道保留一条当前评价并保留变更历史。1–5 星映射为 2–10 分。评价应基于实际服务，不得发布私人资料、广告、无关内容、威胁或伪造使用经历。渠道主不得付费买好评或以补偿为条件要求修改评价。',
          '評價在主站提交，須符合真實使用資格，不能評價自己的渠道；每名用戶對每個渠道保留一條現行評價，並保存變更歷史。1–5 星對應 2–10 分。評價應按實際服務，不得發佈私人資料、廣告、無關內容、威脅或虛構使用經歷。渠道主不得付費購買好評，或以補償為條件要求修改評價。',
          'Reviews are submitted on the main site, require verified usage eligibility and cannot target your own channel. Each user has one current review per channel with change history retained. One to five stars map to two to ten points. Reviews must reflect actual service and exclude private data, ads, irrelevant material, threats and fabricated experiences. Owners must not buy favorable reviews or make compensation conditional on changing a review.',
        ),
        copy(
          '店铺评分先合并每位消费者对该店铺旗下公开渠道的评价，再让消费者等权平均，减少同一人评价多条渠道造成的重复影响。筛选模型或厂商不改变店铺总分。主站是评分权威；社区查询主站摘要，并在评分变动时更新，不是另一套独立评分。',
          '店鋪評分先合併每位消費者對該店鋪旗下公開渠道的評價，再讓消費者等權平均，減少同一人評價多條渠道造成的重複影響。篩選模型或廠商不會改變店鋪總分。主站是評分權威；社群查詢主站摘要，並在評分變更時更新，並非另一套獨立評分。',
          'Shop ratings first combine each consumer’s reviews across that shop’s public channels, then weight consumers equally so reviewing multiple channels does not give a person extra influence. Model or provider filters do not change the overall shop score. The main site is authoritative; the community reads its summaries and updates on rating changes rather than maintaining a separate rating system.',
        ),
      ),
      section(
        'moderation',
        copy(
          '5. 名称审核、投诉与复核',
          '5. 名稱審核、投訴與覆核',
          '5. Name review, reports and appeals',
        ),
        copy(
          '店铺名、分组名、备注和政策链接不得用于广告、联系方式、引流交易、冒充官方、违法、歧视或侵权内容。自动规则与人工审核可拒绝或暂缓公开修改；审核期间以已发布版本为准。来源或数据政策链接只能说明相关政策，不能作为推广入口。',
          '店鋪名、分組名、備註及政策連結不得用作廣告、聯絡方式、引流交易、冒充官方、違法、歧視或侵權內容。自動規則及人工審核可拒絕或暫緩公開修改；審核期間以已發佈版本為準。來源或資料政策連結只可說明相關政策，不能作推廣入口。',
          'Shop names, group names, remarks and policy links must not advertise, expose contact details, solicit off-platform transactions, impersonate officials or contain unlawful, discriminatory or infringing content. Automated checks and human review may reject or defer public edits; the published version remains authoritative during review. Policy links must explain relevant policies, not function as promotional destinations.',
        ),
        copy(
          '发现模型不符、错误披露、刷量、异常评价或侵权，请通过支持提交数字 ID、时间、请求编号和可核对证据。平台可以检查相关记录、限制受影响展示或请求、要求修正并处理关联结算；不会把投诉本身当作已证实事实。受影响用户或渠道主可补充证据申请复核。投诉中的私有凭据和正文只通过私信提交。',
          '發現模型不符、錯誤披露、刷量、異常評價或侵權時，請透過支援提交數字 ID、時間、請求編號及可核對證據。平台可檢查相關紀錄、限制受影響展示或請求、要求修正並處理關聯結算；投訴本身不等於已證實事實。受影響用戶或渠道主可補充證據申請覆核。投訴中的私人憑證及正文只透過私訊提交。',
          'Report model mismatches, inaccurate disclosures, manipulated traffic, suspicious reviews or infringement through support with numeric IDs, times, request IDs and verifiable evidence. CodeGo may review records, restrict affected listings or requests, request correction and handle linked settlements. A complaint alone is not a proven fact. Affected users and owners can request review with additional evidence. Submit private credentials and bodies only through private support.',
        ),
      ),
    ],
  },
  {
    id: 'refund',
    href: '/refund-policy',
    title: copy('退款规则', '退款規則', 'Refund policy'),
    introduction: copy(
      '先查看钱包中的订单资格与实时报价，再确认退款。余额总额不等于可退现金。',
      '先查看錢包內的訂單資格及即時報價，再確認退款。餘額總額不等於可退現金。',
      'Review order eligibility and the current quote in your wallet before confirming a refund. Total balance is not the same as refundable cash.',
    ),
    sections: [
      section(
        'eligibility',
        copy('1. 可申请的订单', '1. 可申請的訂單', '1. Eligible orders'),
        copy(
          '符合条件的已支付订单且仍有可退的未使用额度，才可申请。当前自助退款支持易支付人民币订单；其他支付方式请联系支持。未支付、额度已用完、退款处理中或已退款的订单不能重复申请。赠送、邀请奖励或兑换额度不能直接作为现金退款。',
          '只有符合條件的已支付訂單且仍有可退的未使用額度，才可申請。現行自助退款支援易支付人民幣訂單；其他支付方式請聯絡支援。未支付、額度已用完、退款處理中或已退款的訂單不能重複申請。贈送、邀請獎勵或兌換額度不能直接作現金退款。',
          'An eligible paid order must retain refundable unused credits. Self-service refunds currently support CNY orders paid through Epay; contact support for other methods. Unpaid, exhausted, refunding or refunded orders cannot be submitted again. Gifts, referral rewards and redeemed credits are not directly refundable as cash.',
        ),
      ),
      section(
        'quote',
        copy('2. 金额与处理流程', '2. 金額與處理流程', '2. Amount and processing'),
        copy(
          '当前报价按订单实付金额与可退未使用额度的比例计算毛额，再扣除毛额的 2% 费用，按支付币种最小单位取整；最终金额以钱包实时报价为准。确认后系统预留相关额度并向支付渠道提交退款，到账依赖支付渠道确认，不能保证固定时间。处理中不要重复申请。',
          '現行報價按訂單實付金額與可退未使用額度的比例計算毛額，再扣除毛額的 2% 費用，按支付幣種最小單位取整；最終金額以錢包即時報價為準。確認後系統預留相關額度並向支付渠道提交退款，到帳須由支付渠道確認，不能保證固定時間。處理中請勿重複申請。',
          'The current quote prorates the amount paid against refundable unused credits, deducts a 2% fee from that gross refund and rounds in the currency’s minor unit. The live wallet quote is authoritative. Confirmation reserves the relevant credits and submits the refund to the payment provider; arrival depends on provider confirmation, not a fixed time guarantee. Do not repeat a pending application.',
        ),
      ),
      section(
        'plans-invoices',
        copy('3. 套餐、转换与发票', '3. 套餐、轉換與發票', '3. Plans, conversion and invoices'),
        copy(
          '套餐退款受有效期、已用额度和原购买记录限制；转换余额也须追溯购买记录。到期旧套餐不能转换；符合条件的转换先核对报价，转换后的相应权益不能再刷新。退款处理中或已退款订单不能开具或下载商业发票。符合条件的已支付订单可在账单明细自助开具发票，首次开具后购买方信息固定。',
          '套餐退款受有效期、已用額度及原購買紀錄限制；轉換餘額亦須追溯購買紀錄。已到期的舊套餐不能轉換；符合條件的轉換須先核對報價，轉換後相關權益不能再刷新。退款處理中或已退款訂單不能開具或下載商業發票。符合條件的已支付訂單可在帳單明細自助開票，首次開具後買方資訊固定。',
          'Plan refunds depend on validity, consumed credits and the original purchase record; converted balance must also trace to that record. Expired legacy plans cannot be converted. Review an eligible conversion quote first; converted rights cannot be refreshed. Refunding or refunded orders cannot issue or download a commercial invoice. Eligible paid orders support self-service invoicing in billing details; buyer information is fixed after first issue.',
        ),
      ),
      section(
        'disputes',
        copy('4. 异常与复核', '4. 異常與覆核', '4. Problems and review'),
        copy(
          '退款失败、状态长期未变或报价有疑问时，通过支持提供订单编号、退款编号和时间，私信提交必要的脱敏凭证。请求费用争议和订单退款分开核对；不要把所有请求失败都视为整单可退款。本规则不排除适用法律赋予且不能排除的退款权利。',
          '退款失敗、狀態長期未變或報價有疑問時，請透過支援提供訂單編號、退款編號及時間，私訊提交必要且已遮罩的憑證。請求費用爭議與訂單退款分開核對；不要把所有請求失敗均視為整張訂單可退款。本規則不排除適用法律賦予且不能排除的退款權利。',
          'If a refund fails, remains unchanged or a quote is unclear, provide order and refund IDs and timing through support, with necessary redacted evidence privately. Request charge disputes and order refunds are reconciled separately; not every failed request makes an entire order refundable. This policy does not exclude refund rights that applicable law does not allow us to exclude.',
        ),
      ),
    ],
  },
]

export function getPolicyDocument(id: PolicyDocumentID): PolicyDocument {
  return policyDocuments.find((document) => document.id === id)!
}
