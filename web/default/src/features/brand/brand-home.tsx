import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowDownToLine, ArrowRight, Boxes, CircleGauge, KeyRound, LayoutDashboard, Package, Sparkles, WalletCards } from 'lucide-react'
import { getLobeIcon } from '@/lib/lobe-icon'
import { PublicLayout } from '@/components/layout'
import { SiteSeo } from '@/components/seo'
import { useStatus } from '@/hooks/use-status'
import { getPricing } from '@/features/pricing/api'
import { countFreeModels } from '@/features/pricing/lib/model-helpers'

const fallbackGroups = ['OpenAI', 'Anthropic', 'Google', 'DeepSeek', 'Qwen', 'Meta']

export function BrandHome() {
  const { status } = useStatus()
  const pricingQuery = useQuery({ queryKey: ['pricing'], queryFn: getPricing, staleTime: 300000 })
  const models = useMemo(() => (pricingQuery.data?.data ?? []).filter((model) => model.model_name && !model.model_name.toLowerCase().includes('embedding')), [pricingQuery.data])
  const groups = useMemo(() => {
    const values = [...new Set(models.flatMap((model) => model.enable_groups ?? []))]
    return values.length ? values.slice(0, 6) : fallbackGroups
  }, [models])
  const freeCount = pricingQuery.data ? countFreeModels(pricingQuery.data.data, pricingQuery.data.group_ratio ?? {}) : 0
  const modelCount = models.length
  const version = status?.data?.version ?? status?.version ?? 'LIVE'
  const tickerModels = models.slice(0, 8)

  return (
    <PublicLayout showMainContainer={false} headerProps={{ className: 'demo-reference-header' }}>
      <SiteSeo title='CodeGo | AI API' description='CodeGo AI API' canonicalPath='/' />
      <main className='demo-reference-home'>
        <section className='hero'>
          <div className='grid-bg' aria-hidden />
          <div className='tag'>AI CODING GATEWAY · CODE GO</div>
          <h1>让每一次调用，都抵达更远的地方</h1>
          <p className='sub'>统一密钥接入主流编程模型，分组价格与路由状态实时可见。</p>
          <div className='cta'>
            <Link className='btn primary' to='/pricing'><Boxes size={15} />进入市场</Link>
            <Link className='btn' to='/dashboard'><LayoutDashboard size={15} />我的控制台</Link>
            <Link className='btn' to='/download'><ArrowDownToLine size={15} />下载桌面端</Link>
          </div>
        </section>

        <section className='statband'><div className='inner'>
          <div className='cell'><b>{groups.length}</b><span>在售分组</span></div>
          <div className='cell'><b>{modelCount}<small>+</small></b><span>可用模型</span></div>
          <div className='cell'><b>—</b><span>24H 请求数</span></div>
          <div className='cell'><b>99.9<small>%</small></b><span>30 天可用率</span></div>
          <div className='cell'><b>420<small>ms</small></b><span>P50 首字延迟</span></div>
        </div></section>
        <section className='ticker'><div className='inner'><span className='label'><CircleGauge size={13} />实时行情</span><div className='items'>{[...tickerModels, ...tickerModels].map((model, index) => <span className='chip' key={`${model.model_name}-${index}`}><i />{model.model_name}<b>{model.pricing_available ? '在线' : '可用'}</b></span>)}</div></div></section>

        <section className='home-sec'><div className='head'><div><div className='tag'>START HERE</div><h2>你要去哪</h2></div></div><div className='duo'>
          <Link to='/pricing' className='duo-card market'><ArrowRight className='arrow' size={22} /><div className='kicker'>MARKET</div><h3>分组市场</h3><p>分组价格、模型覆盖与路由状态集中查看。</p><div className='mini-stats'><div><b>{groups.length}</b><span>在售分组</span></div><div><b>{modelCount}</b><span>可用模型</span></div><div><b>{freeCount}</b><span>免费模型</span></div></div></Link>
          <Link to='/dashboard' className='duo-card'><ArrowRight className='arrow' size={22} /><div className='kicker'>CONSOLE</div><h3>我的控制台</h3><p>密钥、用量、日志、钱包、套餐与盲盒。</p><div className='mini-stats'><div><b>API</b><span>统一入口</span></div><div><b>LIVE</b><span>实时状态</span></div><div><b>{version}</b><span>系统版本</span></div></div></Link>
        </div></section>

        <section className='home-sec'><div className='head'><div><div className='tag'>TRENDING</div><h2>本周热门分组</h2></div><Link className='more' to='/pricing'>查看全部 {groups.length} 个分组 <ArrowRight size={15} /></Link></div><div className='hot-grid'>{groups.map((group) => <Link to='/pricing' search={{ group }} key={group} className='hcard'><div className='src'>{getLobeIcon(group, 14)} {group}</div><h3>{group}</h3><div className='pr'>{models.filter((model) => (model.enable_groups ?? []).includes(group)).length || '—'}<span> 个模型</span></div><div className='hm'><span>状态 <b className='g'>在线</b></span><span>免费 <b>{freeCount}</b></span></div><span className='go'>查看分组 <ArrowRight size={14} /></span></Link>)}</div></section>

        <section className='home-sec'><div className='head'><div><div className='tag'>PLANS / BLIND BOX</div><h2>套餐与盲盒</h2></div></div><div className='pack-grid'>
          <Link to='/packages' className='pack-card'><div className='ptype'><WalletCards size={14} /> PLAN / MONTHLY</div><h4>月度套餐</h4><div className='price'>套餐中心</div><span className='btn'>查看套餐 <ArrowRight size={14} /></span></Link>
          <Link to='/packages' className='pack-card hot'><div className='ptype'><Package size={14} /> PLAN / FLEXIBLE</div><h4>灵活额度</h4><div className='price'>按需选择</div><span className='btn primary'>浏览套餐 <ArrowRight size={14} /></span></Link>
          <Link to='/blind-box' className='pack-card'><div className='ptype'><Sparkles size={14} /> BLIND / RANDOM</div><h4>盲盒</h4><div className='price'>随机奖励</div><p>打开盲盒，获取当前活动可用权益。</p><span className='btn'>去开盲盒 <ArrowRight size={14} /></span></Link>
        </div></section>

        <section className='home-sec'><div className='head'><div><div className='tag'>WHY CODE GO</div><h2>一个长期可用的 AI 编程入口</h2></div></div><div className='feat-grid'>
          <article className='feat'><div className='ic'><KeyRound size={20} /></div><h4>统一密钥</h4><p>一条密钥连接多个编程模型。</p></article>
          <article className='feat'><div className='ic'><CircleGauge size={20} /></div><h4>实时状态</h4><p>分组健康度与模型行情同步呈现。</p></article>
          <article className='feat'><div className='ic'><LayoutDashboard size={20} /></div><h4>清晰用量</h4><p>概览、日志、套餐和余额集中管理。</p></article>
        </div></section>
        <footer><span>Code Go · AI 网关与代码平台</span><span>{version}</span></footer>
      </main>
    </PublicLayout>
  )
}
