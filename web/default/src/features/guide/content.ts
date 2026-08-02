export type GuideImage = {
  src: string
  alt: string
  caption: string
}

export type GuideSection = {
  id: string
  eyebrow: string
  title: string
  summary: string
  steps?: string[]
  notes?: string[]
  diagram_title?: string
  diagram_steps?: string[]
  images: GuideImage[]
}

export const guideSections: GuideSection[] = [
  {
    id: 'support',
    eyebrow: '01',
    title: '售后 QQ 群',
    summary:
      '说明文档最前面先保留售后支持入口。遇到配置、套餐、盲盒、宠物升级或脚本使用问题时，可以直接扫码进群。',
    steps: [
      '优先使用手机 QQ 扫描下方二维码加入售后群。',
      '如果当前设备不方便扫码，也可以直接在 QQ 中搜索群号 996040309。',
      '进群后建议附上账号、页面位置和问题截图，方便快速定位。',
    ],
    images: [
      {
        src: '/guide/16-support-qq-group.png',
        alt: 'Code Go 售后 QQ 群二维码',
        caption: '售后 QQ 群号：996040309。扫码或手动搜索群号都可以进入。',
      },
    ],
    notes: [
      '群号：996040309',
      '建议把报错页面、控制台提示或具体操作步骤一并发群，沟通会更快。',
    ],
  },
  {
    id: 'home',
    eyebrow: '02',
    title: '主页与入口',
    summary:
      '主页会集中展示 Code Go 的定位、模型入口和桌面端下载路径，帮助新用户快速找到下一步。',
    steps: [
      '先从首页了解站点定位、主要功能入口和模型广场。',
      '根据当前目标选择配置密钥、查看模型或下载桌面端，再进入对应流程。',
    ],
    images: [
      {
        src: '/guide/01-home.png',
        alt: 'Code Go 首页',
        caption: '首页首屏展示产品定位、核心入口与当前模型概览。',
      },
    ],
  },
  {
    id: 'register',
    eyebrow: '03',
    title: '注册账号',
    summary: '新用户先完成注册，再进入控制台配置自己的开发环境。',
    steps: [
      '打开注册页，填写用户名、邮箱和密码。',
      '提交后即可完成注册，并进入后续登录流程。',
    ],
    images: [
      {
        src: '/guide/02-sign-up.png',
        alt: '注册页面',
        caption: '注册页用于创建新的 Code Go 账号。',
      },
    ],
  },
  {
    id: 'signin',
    eyebrow: '04',
    title: '登录控制台',
    summary: '注册完成后使用账号密码登录，即可进入用户控制台。',
    steps: [
      '输入用户名或邮箱与密码。',
      '登录成功后会进入控制台概览页或系统指定页面。',
    ],
    images: [
      {
        src: '/guide/03-sign-in.png',
        alt: '登录页面',
        caption: '登录页支持使用账号密码进入站点。',
      },
    ],
  },
  {
    id: 'dashboard',
    eyebrow: '05',
    title: '控制台概览',
    summary: '概览页会展示当前资金状态和常用功能，适合作为日常使用的起点。',
    steps: [
      '登录后先查看概览页，确认余额、套餐和盲盒入口。',
      '左侧侧边栏已经将套餐购买和盲盒活动单独拆分出来，钱包只保留资金与扣费策略。',
    ],
    images: [
      {
        src: '/guide/04-dashboard-overview.png',
        alt: '控制台概览',
        caption: '控制台概览用于统一查看日常入口与当前状态。',
      },
    ],
  },
  {
    id: 'keys',
    eyebrow: '06',
    title: '创建 API Key',
    summary: '进入 API 密钥页面，创建用于脚本和客户端接入的访问密钥。',
    steps: [
      '打开 API 密钥页面，点击创建。',
      '填写名称并选择分组后保存。',
      '创建成功后在列表中确认新密钥已经出现。',
    ],
    images: [
      {
        src: '/guide/05-create-key-drawer.png',
        alt: '创建 API Key 抽屉',
        caption: '通过抽屉表单创建新的 API Key。',
      },
      {
        src: '/guide/06-key-created.png',
        alt: 'API Key 创建成功',
        caption: '保存后可在列表中看到新创建的密钥。',
      },
    ],
  },
  {
    id: 'script',
    eyebrow: '07',
    title: '下载 Codex 配置脚本',
    summary:
      '在 API Key 列表的操作菜单中可以直接下载 Codex 配置脚本，用于快速完成本地 Codex 初始化。',
    steps: [
      '在目标 Key 的操作菜单中打开 Codex 配置脚本选项。',
      '按操作系统下载对应脚本。',
      'Windows 脚本双击运行后会自动配置好 Codex，并保留窗口显示成功提示，按任意键退出。',
      'Linux 脚本执行完成后会直接输出 Codex 配置成功提示。',
    ],
    images: [
      {
        src: '/guide/07-download-script-menu.png',
        alt: '下载脚本菜单',
        caption: '支持从 Key 列表直接下载 Codex 配置脚本。',
      },
    ],
    notes: [
      '脚本内容会基于当前 API Key 自动生成。',
      '下载后不需要再手动拼接 Codex 认证信息。',
    ],
  },
  {
    id: 'packages',
    eyebrow: '08',
    title: '套餐选择',
    summary:
      '套餐购买已经从钱包中拆分出来，页面会单独展示月卡、日卡和右侧状态面板。',
    steps: [
      '从左侧进入套餐购买页面。',
      '比较月卡和日卡的额度、周期和适用场景。',
      '按需选择目标套餐，进入购买前确认。',
    ],
    images: [
      {
        src: '/guide/08-packages-page.png',
        alt: '套餐购买页面',
        caption: '套餐页单独展示月卡与日卡，不再混入钱包。',
      },
      {
        src: '/guide/09-package-subscribe-dialog.png',
        alt: '套餐订阅弹窗',
        caption: '点击订阅后会先进入购买前确认弹窗。',
      },
    ],
    notes: ['选择前先确认周期、总额度和适用场景，再进入订阅确认弹窗。'],
  },
  {
    id: 'blind-box',
    eyebrow: '09',
    title: '盲盒活动',
    summary: '盲盒页会单独展示购买入口、奖励规则、结果弹窗和开奖记录。',
    steps: [
      '从左侧进入盲盒页面。',
      '查看可抽奖励、中奖规则和当前状态。',
      '选择购买数量后完成支付，结果会在弹窗中展示。',
    ],
    diagram_title: '盲盒规则',
    diagram_steps: [
      '按页面价格购买',
      '普通额度直接入钱包',
      'Claude 额度直接入 Claude 额度池',
      '结果弹窗确认后完成入账',
    ],
    images: [
      {
        src: '/guide/11-blind-box-page-enabled.png',
        alt: '盲盒活动页面',
        caption: '盲盒页集中展示规则、概率和活动状态。',
      },
    ],
    notes: [
      '购买前先确认当前价格、数量和支付方式。',
      '抽到普通额度会直接进入钱包，Claude 额度会直接进入 Claude 额度池。',
      '结果弹窗可以直接关闭，也可以点击确定确认领取。',
    ],
  },
  {
    id: 'wallet',
    eyebrow: '10',
    title: '钱包与扣费顺序',
    summary:
      '钱包页现在只负责余额、兑换码、账单和扣费优先顺序设置，右侧栏继续保留策略配置。',
    steps: [
      '进入钱包页查看余额和账单状态。',
      '在右侧栏设置订阅优先或余额兜底等扣费策略。',
      '通过兑换码入口补充额度或完成后续资金操作。',
    ],
    images: [
      {
        src: '/guide/13-wallet-page.png',
        alt: '钱包页面',
        caption: '钱包页保留资金与扣费策略管理，不再承载套餐与盲盒入口。',
      },
    ],
    notes: [
      '钱包页重点查看余额信息、兑换码入口和扣费优先顺序。',
      '设置完成后，系统会按保存后的扣费策略执行额度消耗。',
    ],
  },
]
