export const SITE_NAME = 'Code Go'
export const SITE_ORIGIN = 'https://shu26.cfd'

export type PublicPageSection = {
  heading: string
  paragraphs: string[]
}

export type PublicPageSeoEntry = {
  path:
    | '/about'
    | '/download'
    | '/faq'
    | '/guide'
    | '/pricing'
    | '/privacy-policy'
    | '/user-agreement'
  title: string
  description: string
  keywords: string
  h1: string
  eyebrow: string
  intro: string
  sections: PublicPageSection[]
}

export const publicPageSeoEntries: PublicPageSeoEntry[] = [
  {
    path: '/pricing',
    title: 'Code Go 模型广场 | 免费模型、价格对比与 Codex / Claude 选择',
    description:
      'Code Go 模型广场集中展示免费模型、Codex、Claude、GPT 与多供应商价格结构，方便你按分组、标签、额度和输出成本快速筛选，找到适合长期 AI 编程的模型入口。',
    keywords:
      'Code Go 模型广场,免费模型,价格对比,Codex,Claude,GPT,AI API 价格,模型筛选',
    h1: 'Code Go 模型广场与价格总览',
    eyebrow: '模型目录',
    intro:
      '这个页面把当前可用模型、免费模型、价格维度和筛选方式放在同一处。你可以先看免费模型能否承担前置任务，再决定什么时候切到 Claude、GPT 或更适合代码工作的模型。',
    sections: [
      {
        heading: '先按任务类型筛，而不是先按模型名筛',
        paragraphs: [
          '真正高频使用 AI 编程的开发者，往往不是一开始就锁定某个模型，而是先判断当前任务更偏向信息整理、轻量改写、复杂推理，还是长上下文代码协作。模型广场的价值，就是把这些判断动作提前做掉。',
          '在 Code Go 里，你可以从供应商、分组、标签、额度类型和价格维度一起看，避免在多个页面之间来回切换，再决定是否从免费模型切到更高阶模型。',
        ],
      },
      {
        heading: '价格透明比“低价”口号更重要',
        paragraphs: [
          'Bing 和用户都会更偏好信息明确的页面。这里会直接展示输入价、输出价、缓存价和可见模型数，而不是只写一句笼统的低价或稳定。',
          '对长期使用者来说，真正重要的是把模型能力、调用成本和任务价值对齐，而不是每次都从零开始比较。',
        ],
      },
      {
        heading: '模型广场不是终点，而是下一步决策入口',
        paragraphs: [
          '看完模型广场后，通常会回到教程页完成配置，或者直接进入专题页继续判断 Codex API、Claude Code API、中转、排障和选择逻辑。',
          '因此这页既承担公开流量入口，也承担站内分流节点的角色，适合从搜索结果直接进入后继续阅读。',
        ],
      },
    ],
  },
  {
    path: '/guide',
    title: 'Code Go 使用说明 | API 密钥、套餐、盲盒与配置指南',
    description:
      'Code Go 使用说明覆盖账号注册、API 密钥创建、Codex 配置脚本、套餐购买、盲盒活动、钱包策略和宠物图鉴，帮助你把站点入口真正接入长期 AI 编程工作流。',
    keywords:
      'Code Go 使用说明,API 密钥,配置指南,Codex 脚本,套餐,盲盒,钱包,宠物图鉴',
    h1: 'Code Go 使用说明与配置路径',
    eyebrow: '使用指南',
    intro:
      '这不是只讲一个按钮怎么点的说明页，而是把注册、登录、API Key、脚本下载、套餐、盲盒、钱包和宠物体系串成一条完整路径，让新用户可以顺着站点结构真正开始使用。',
    sections: [
      {
        heading: '从注册到本地工具配置，要有完整链路',
        paragraphs: [
          '搜索教程类关键词的用户，通常不是在找一个抽象介绍，而是在找“接下来应该做什么”。因此这页把注册、登录、控制台、密钥创建和脚本下载按实际顺序展开。',
          '用户读完之后，不需要自己再猜页面关系，就能继续进入模型广场、钱包或桌面端下载页。',
        ],
      },
      {
        heading: '套餐、盲盒、钱包和宠物都属于真实使用流程',
        paragraphs: [
          'Code Go 的公开页面不只是展示模型，也承接套餐订阅、额度管理、盲盒活动和宠物成长机制。教程页会把这些功能入口和使用场景解释清楚，而不是留成不可理解的站内术语。',
          '对搜索引擎来说，这类结构化说明也比单纯堆关键词更容易被识别为有用内容。',
        ],
      },
      {
        heading: '教程页本身也承担 SEO 着陆页作用',
        paragraphs: [
          '很多人会直接搜索“Codex 配置”“API Key”“使用说明”这类词进入站点，因此这页需要在初始 HTML 里就具备清晰标题、描述、H1 和足够正文，而不是等前端加载完成后再补。',
          '这次静态化处理的目标，就是让 Bing 抓取时也能直接看到完整内容框架。',
        ],
      },
    ],
  },
  {
    path: '/faq',
    title: 'Code Go 常见问题 | Codex、Claude Code 与 AI 编程问答',
    description:
      'Code Go 常见问题页面解释平台定位、Codex 与 Claude Code 适用场景、长期 AI 编程积累方式，以及模型入口、套餐、记录和工作流之间的关系。',
    keywords:
      'Code Go 常见问题,常见问题,Codex,Claude Code,AI 编程,长期工作流,模型入口',
    h1: 'Code Go 常见问题与核心说明',
    eyebrow: 'FAQ',
    intro:
      'FAQ 页不是为了堆问题数量，而是为了快速回答用户最先关心的事情：Code Go 是什么、适合谁、和普通 AI API 平台有什么不同，以及为什么它强调长期使用与记录积累。',
    sections: [
      {
        heading: 'FAQ 负责解释站点定位',
        paragraphs: [
          '很多用户第一次进入站点时，还没有完全理解 Code Go 是模型中转、工作流入口，还是长期记录平台。FAQ 的第一职责，就是先把这个定位讲清楚。',
          '当页面能回答“这是什么”和“为什么值得继续看”这两个问题时，用户停留和继续浏览的概率都会更高。',
        ],
      },
      {
        heading: 'FAQ 也要承接真实搜索意图',
        paragraphs: [
          '像“Code Go 是什么”“适合哪些人”“和普通平台有什么区别”这类问题，既是用户会问的问题，也是 Bing 可能从页面里提取理解主题的关键线索。',
          '因此 FAQ 页不仅要有问答结构，还要在初始 HTML 中带有清晰的 title、description 和主体内容。',
        ],
      },
      {
        heading: '问答之后还要给出下一步路径',
        paragraphs: [
          'FAQ 页读完之后，用户通常会继续看教程页、关于页或模型广场。一个好的 FAQ 页面不该把用户留在原地，而是应该自然导向下一步动作。',
          '这也是这次修复里一并强化的部分：FAQ 不再只是短标题和短描述，而是有明确入口价值的公开页。',
        ],
      },
    ],
  },
  {
    path: '/about',
    title: '关于 Code Go | 长期 AI 编程、品牌定位与产品理念',
    description:
      '关于 Code Go 页面说明平台为什么强调长期 AI 编程、持续记录和多模型工作流，也介绍品牌定位、售后支持和面向 Codex、Claude Code 用户的核心产品理念。',
    keywords:
      '关于 Code Go,品牌定位,AI 编程,长期使用,产品理念,Codex,Claude Code',
    h1: '关于 Code Go 与长期 AI 编程的产品理念',
    eyebrow: '关于我们',
    intro:
      '关于页的目标不是一句空泛品牌文案，而是解释 Code Go 为什么存在、适合什么样的开发者，以及为什么它把“长期使用感”和“持续积累”放在公开表达最前面。',
    sections: [
      {
        heading: 'Code Go 不是只解决一次调用',
        paragraphs: [
          '对于高频使用 Codex、Claude Code 或多模型协作的开发者来说，真正重要的问题不是能不能调用某个模型，而是能不能把模型纳入长期工作流。',
          'Code Go 试图解决的，就是这种更接近真实开发过程的问题：入口是否统一、记录是否连续、模型选择是否清楚、长期使用是否顺手。',
        ],
      },
      {
        heading: '公开页需要承接品牌理解和支持入口',
        paragraphs: [
          '关于页同时承担两种职责：一是向搜索引擎和新用户解释品牌定位，二是向已经进入站点的用户展示售后支持和进一步阅读路径。',
          '这也是为什么关于页不能只是一个很短的标题或空白容器，而需要在 HTML 初始输出里就具备完整文本。',
        ],
      },
      {
        heading: '品牌表达要和实际页面结构一致',
        paragraphs: [
          '如果首页、教程页、模型页和常见问题页都围绕长期 AI 编程展开，那么关于页也必须与这条主线保持一致，而不是变成泛化介绍。',
          '这次调整会让关于页在不同加载分支下都保留统一 H1 和完整元信息，避免被 Bing 判成内容过少或缺少主体结构的页面。',
        ],
      },
    ],
  },
  {
    path: '/download',
    title: 'Code Go Desktop 下载 | Windows、macOS、Linux 安装入口',
    description:
      '下载 Code Go Desktop，通过浏览器授权、Token 导入和本地工具配置，把 Code Go 与 Codex、Claude Code、Gemini CLI、OpenCode、OpenClaw 和 Hermes 连接起来。',
    keywords:
      'Code Go Desktop 下载,Windows,macOS,Linux,Codex,Claude Code,Gemini CLI,OpenCode',
    h1: 'Code Go Desktop 下载与安装说明',
    eyebrow: '桌面端下载',
    intro:
      '下载页不仅要提供平台安装包，也要解释桌面端在浏览器授权、Token 导入和本地工具配置中的作用。对从搜索直接进入的用户来说，这页应该能直接回答“下载后怎么接上 Code Go”。',
    sections: [
      {
        heading: '桌面端承担本地工具接入职责',
        paragraphs: [
          'Code Go Desktop 的作用不是单纯下载一个壳，而是把网站账号、浏览器授权和本地工具配置连接起来，让 Codex、Claude Code、Gemini CLI 等工具可以在一处统一管理。',
          '因此下载页需要同时说明安装包、授权方式和配置路径，而不能只有一个下载按钮。',
        ],
      },
      {
        heading: '平台、校验和回退路径都要讲清楚',
        paragraphs: [
          '高质量下载页通常会解释 Windows、macOS、Linux 的安装形式，说明发布来源、摘要校验和回退到 release 页的方式。这些信息既能提升用户信任，也更适合被搜索引擎理解为完整下载文档。',
          'Code Go 的下载页会把这些信息和后续配置说明放在一起，避免用户下载后还要回头找文档。',
        ],
      },
      {
        heading: '下载页也是站内分流节点',
        paragraphs: [
          '用户完成下载后，下一步往往是打开 token console、查看 FAQ，或继续回到模型页和教程页。因此下载页本身也应该具备良好的内部链接和足够正文，而不是孤立页面。',
          '这次静态页面修复会一并保留这些公开链接结构。',
        ],
      },
    ],
  },
  {
    path: '/privacy-policy',
    title: 'Code Go 隐私政策 | 账号、调用记录与数据处理说明',
    description:
      'Code Go 隐私政策说明账号信息、调用记录、支付订单、偏好配置和必要风控数据的处理方式，并解释数据保留、安全措施、第三方服务和用户可申请的隐私权利。',
    keywords: 'Code Go 隐私政策,账号信息,调用记录,数据处理,支付订单,用户隐私',
    h1: 'Code Go 隐私政策与数据处理说明',
    eyebrow: '隐私政策',
    intro:
      '隐私政策页需要在公开抓取时就清楚说明站点会收集哪些数据、为什么收集、如何保留和如何保护。否则搜索引擎很容易把它判定为只有极少内容的模板页。',
    sections: [
      {
        heading: '隐私页要说明收集范围和业务必要性',
        paragraphs: [
          'Code Go 会涉及账号、调用、支付、偏好设置和必要的安全日志，因此隐私页需要明确这些信息分别用在什么地方，而不是只给出泛化条款。',
          '对用户来说，这些内容直接影响是否信任站点；对 Bing 来说，这也是判断页面完整度的重要因素。',
        ],
      },
      {
        heading: '数据保留、安全和第三方服务要可读',
        paragraphs: [
          '如果隐私页只是一段极短的占位说明，很容易被认定为描述缺失或内容不足。更合理的方式，是把数据保留、安全措施和第三方模型或支付服务的关系交代清楚。',
          '这类法律页虽然不是营销页，但同样需要独立 title、description、H1 和可见正文。',
        ],
      },
      {
        heading: '用户权利与联系路径不能缺',
        paragraphs: [
          '隐私政策的最后一部分通常要告诉用户如何查看、更新或申请删除部分信息，以及通过哪些渠道联系站点处理数据相关问题。',
          '这既是内容完整性的要求，也能减少被搜索引擎判成低价值法律页的概率。',
        ],
      },
    ],
  },
  {
    path: '/user-agreement',
    title: 'Code Go 用户协议 | 额度、套餐、模型调用与使用规则',
    description:
      'Code Go 用户协议说明账号安全、额度和套餐规则、模型调用使用边界、盲盒和活动机制、中断与变更说明，以及用户在站点内使用服务时应遵守的责任与限制。',
    keywords: 'Code Go 用户协议,额度规则,套餐规则,模型调用,账号安全,使用规范',
    h1: 'Code Go 用户协议与使用规则说明',
    eyebrow: '用户协议',
    intro:
      '用户协议页不仅是法律文本，也是解释站点使用边界的重要公开页面。它需要明确账号安全、额度规则、模型调用、活动机制和服务变更说明，避免只有很短的通用描述。',
    sections: [
      {
        heading: '协议页要覆盖真实使用场景',
        paragraphs: [
          'Code Go 既有模型调用，也有套餐、盲盒、宠物和额度体系，所以协议页必须覆盖这些真实功能，而不是只保留笼统的互联网服务条款。',
          '只有把使用边界和责任讲清楚，协议页才是对用户和搜索引擎都完整的页面。',
        ],
      },
      {
        heading: '账号安全、额度和活动规则都需要清晰',
        paragraphs: [
          '用户最关心的通常是账号能不能共享、套餐和余额怎么处理、活动奖励如何生效、模型调用有什么限制。协议页应该正面回答这些问题。',
          '如果这些内容缺失，既不利于用户理解，也会让公开页看起来像薄内容模板。',
        ],
      },
      {
        heading: '协议更新和服务变更需要提前交代',
        paragraphs: [
          '由于模型价格、上游可用性、支付渠道和站内活动都可能变化，用户协议必须说明服务中断、规则调整和继续使用的默认接受逻辑。',
          '这也是让法律页在公开抓取中显得完整、具体、与产品真实能力一致的重要部分。',
        ],
      },
    ],
  },
]

export function getPublicPageSeoEntry(path: PublicPageSeoEntry['path']) {
  const entry = publicPageSeoEntries.find((item) => item.path === path)
  if (!entry) {
    throw new Error(`Missing public page SEO entry for ${path}`)
  }
  return entry
}
