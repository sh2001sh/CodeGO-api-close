/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com.
*/
import { useEffect, useMemo, useRef } from 'react'
import { Link } from '@tanstack/react-router'
import {
  Activity,
  ArrowRight,
  ArrowUpRight,
  Sparkles,
  Sunrise,
  Zap,
} from 'lucide-react'
import { useMarketplaceGroups } from '@/features/marketplace/hooks'
import { usePricingData } from '@/features/pricing/hooks/use-pricing-data'
import { countFreeModels } from '@/features/pricing/lib/model-helpers'
import { formatPrice } from '@/features/pricing/lib/price'
import { useHomePageContent } from '@/features/home/hooks'
import { Markdown } from '@/components/ui/markdown'
import { SiteSeo } from '@/components/seo'
import { normalizeSystemName } from '@/lib/branding'
import { useAuthStore } from '@/stores/auth-store'
import { CountUp, Reveal } from '../components/reveal'
import { DawnNav } from '../components/dawn-nav'
import { compactCount, fmtInt, pct, sec } from '../lib/format'

const HOME_FILTERS = {
  search: '',
  model: '',
  source: '',
  provider: '',
  status: '',
  verification: '',
  sort: 'score',
  direction: 'desc',
  window_hours: 24,
  page: 1,
  page_size: 12,
}

export function DawnHome() {
  const user = useAuthStore((state) => state.auth.user)
  const { content, isLoaded, isUrl } = useHomePageContent()
  const marketplace = useMarketplaceGroups(HOME_FILTERS)
  const pricing = usePricingData()

  const groups = useMemo(() => marketplace.data?.items ?? [], [marketplace.data])

  const models = useMemo(
    () =>
      (pricing.models.length ? pricing.models : pricing.pricedModelDetails)
        .filter((model) => model.model_name)
        .filter(
          (model) => !model.model_name.toLowerCase().includes('embedding')
        ),
    [pricing.models, pricing.pricedModelDetails]
  )
  const freeCount = pricing.pricedModelDetails.length
    ? countFreeModels(pricing.pricedModelDetails, pricing.groupRatio)
    : 0

  const aggregates = useMemo(() => {
    const withTraffic = groups.filter((group) => group.request_count > 0)
    const totalRequests = groups.reduce((sum, g) => sum + g.request_count, 0)
    let availability: number | null = null
    let ttft: number | null = null
    if (withTraffic.length) {
      availability =
        (withTraffic.reduce((sum, g) => sum + g.success_rate * 100, 0) /
          withTraffic.length)
      ttft =
        withTraffic.reduce((sum, g) => sum + g.avg_ttft_ms, 0) /
        withTraffic.length
    }
    return { totalRequests, availability, ttft }
  }, [groups])

  const tickerModels = useMemo(() => models.slice(0, 8), [models])

  const heroRef = useRef<HTMLElement>(null)

  useEffect(() => {
    const hero = heroRef.current
    if (!hero) return
    if (
      typeof window.matchMedia === 'function' &&
      window.matchMedia('(prefers-reduced-motion: reduce)').matches
    ) {
      return
    }
    const onScroll = () => {
      const y = window.scrollY
      if (y > window.innerHeight) return
      hero
        .querySelectorAll<HTMLElement>('[data-speed]')
        .forEach((element) => {
          const speed = Number(element.dataset.speed ?? 0)
          element.style.marginBottom = `${-y * speed}px`
        })
    }
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => window.removeEventListener('scroll', onScroll)
  }, [])

  if (!isLoaded) {
    return (
      <div className='dawn'>
        <main className='flex min-h-screen items-center justify-center'>
          <Activity className='animate-pulse' color='#b8562e' />
        </main>
      </div>
    )
  }

  if (content) {
    return (
      <div className='dawn'>
        <SiteSeo title={`${normalizeSystemName()} | AI API`} description='AI 编程网关 · 分组市场与模型价目' canonicalPath='/' />
        <main className='overflow-x-hidden'>
          {isUrl ? (
            <iframe
              src={content}
              className='h-screen w-full border-none'
              title='Home'
            />
          ) : (
            <div className='container mx-auto py-8'>
              <Markdown className='custom-home-content'>{content}</Markdown>
            </div>
          )}
        </main>
      </div>
    )
  }

  const brand = normalizeSystemName()

  return (
    <div className='dawn'>
      <SiteSeo title={`${brand} | AI 编程网关`} description='AI 编程网关 · 分组市场与模型价目' canonicalPath='/' />
      <DawnNav variant='hero' />

      {/* 幕 0 · 日出 */}
      <section className='scene s-hero' ref={heroRef}>
        <div
          className='orbit'
          data-speed='0.06'
          style={{
            width: '88vmin',
            height: '88vmin',
            top: '50%',
            transform: 'translate(-50%, -58%)',
          }}
        />
        <div
          className='orbit'
          data-speed='0.03'
          style={{
            width: '124vmin',
            height: '124vmin',
            top: '50%',
            transform: 'translate(-50%, -56%)',
            opacity: 0.6,
          }}
        />
        <div className='sun' data-speed='0.12' style={{ bottom: '-6vmin' }} />
        <div className='horizon' style={{ bottom: '12.4vmin' }} />
        <div className='haze' />
        <div className='inner'>
          <div className='kick reveal in'>
            <span className='n'>00</span>
            AI CODING GATEWAY
          </div>
          <h1 className='reveal in'>
            一线，
            <br />
            <span className='gold'>连万象。</span>
          </h1>
          <div className='cta reveal in'>
            <Link className='btn primary' to='/market'>
              进入市场 <ArrowUpRight size={16} />
            </Link>
            <Link className='btn' to={user ? '/dashboard' : '/sign-in'}>
              {user ? '我的控制台' : '登录 / 注册'}
            </Link>
          </div>
        </div>
        <div className='scroll'>SCROLL</div>
      </section>

      {/* 巨型字标跑马灯 */}
      <div className='band' aria-hidden>
        <div className='items'>
          {[0, 1].map((index) => (
            <span key={index}>
              AI RESOURCES MARKET <Sparkles />
              <span className='o'>一线连万象</span> <Sunrise />
              {brand.toUpperCase()} <span className='o'>LIVE</span> <Zap />
            </span>
          ))}
        </div>
      </div>

      {/* 幕 1 */}
      <section className='scene s-cream'>
        <Reveal>
          <div className='kick'>
            <span className='n'>01</span>
            INTERLUDE
          </div>
          <h2>
            万模，<em>皆可比较</em>。
          </h2>
        </Reveal>
      </section>

      {/* 幕 2 · 实时大盘 */}
      <section className='scene s-body'>
        <div className='dawn-wrap narrow'>
          <Reveal>
            <div className='s-head'>
              <div>
                <div className='kick'>
                  <span className='n'>02</span>
                  LIVE
                </div>
                <h2>此刻</h2>
              </div>
            </div>
          </Reveal>
          <Reveal>
            <div className='statband'>
              <div className='cell'>
                <b>
                  <CountUp to={marketplace.data?.total ?? 0} />
                </b>
                <span>在售分组</span>
              </div>
              <div className='cell'>
                <b>
                  <CountUp to={models.length} />
                </b>
                <span>可用模型</span>
              </div>
              <div className='cell'>
                <b>
                  <CountUp to={aggregates.totalRequests} />
                </b>
                <span>24H 请求</span>
              </div>
              <div className='cell'>
                <b>
                  {aggregates.availability != null ? (
                    <>
                      <CountUp to={aggregates.availability * 10} div={10} />
                      <span className='u'>%</span>
                    </>
                  ) : (
                    '—'
                  )}
                </b>
                <span>24H 成功率</span>
              </div>
              <div className='cell'>
                <b>
                  {aggregates.ttft != null ? (
                    <>
                      <CountUp to={aggregates.ttft / 10} div={10} />
                      <span className='u'>s</span>
                    </>
                  ) : (
                    '—'
                  )}
                </b>
                <span>首字均值</span>
              </div>
            </div>
          </Reveal>
          {tickerModels.length > 0 && (
            <Reveal>
              <div className='ticker'>
                <span className='label'>
                  <Activity size={13} />
                  行情
                </span>
                <div className='win'>
                  <div className='items'>
                    {[...tickerModels, ...tickerModels].map((model, index) => (
                      <span
                        className='chip'
                        key={`${model.model_name}-${index}`}
                      >
                        <i />
                        {model.model_name}
                        <b>
                          {model.quota_type === 1
                            ? '按量'
                            : formatPrice(model, 'input', 'M')}
                          /1M
                        </b>
                      </span>
                    ))}
                  </div>
                </div>
              </div>
            </Reveal>
          )}
        </div>
      </section>

      {/* 幕 3 */}
      <section className='scene s-cream'>
        <Reveal>
          <div className='kick'>
            <span className='n'>03</span>
            INTERLUDE
          </div>
          <h2>
            <span className='stroke'>路由</span>，<em>可见</em>。
          </h2>
        </Reveal>
      </section>

      {/* 幕 4 · 热门分组 */}
      <section className='scene s-body' style={{ paddingTop: '10vh' }}>
        <div className='dawn-wrap narrow'>
          <Reveal>
            <div className='s-head'>
              <div>
                <div className='kick'>
                  <span className='n'>04</span>
                  MARKET
                </div>
                <h2>热</h2>
              </div>
              <Link className='btn' to='/market'>
                全部 {fmtInt(marketplace.data?.total ?? 0)} 组{' '}
                <ArrowRight size={15} />
              </Link>
            </div>
          </Reveal>
          <Reveal>
            {groups.length ? (
              <div className='hot'>
                {groups.slice(0, 3).map((group) => (
                  <Link to='/market' className='hcard' key={group.id}>
                    <div className='halo' />
                    <span className='src'>{group.source_label}</span>
                    <h3>{group.system_display_name}</h3>
                    <div className='pr'>
                      {group.multiplier}
                      <span> × 倍率</span>
                    </div>
                    <div className='hm'>
                      <span className='g'>
                        24H 成功<b>{pct(group.success_rate)}%</b>
                      </span>
                      <span>
                        首字<b>{sec(group.avg_ttft_ms)}s</b>
                      </span>
                      <span>
                        模型<b>{group.models.length}</b>
                      </span>
                      <span>
                        24H<b>{compactCount(group.request_count)}</b>
                      </span>
                    </div>
                    <span className='go'>
                      进入 <ArrowRight size={14} />
                    </span>
                  </Link>
                ))}
              </div>
            ) : (
              <div className='empty'>
                <span className='eic'>
                  <Sunrise size={20} />
                </span>
                <b>市场分组上架中</b>
                <span>{freeCount} 个模型价目已就绪</span>
                <Link className='btn mini' to='/pricing'>
                  查看模型价目 <ArrowRight size={13} />
                </Link>
              </div>
            )}
          </Reveal>
        </div>
      </section>

      {/* 幕 5 · 夜面 */}
      <section className='scene s-night'>
        <div className='dawn-wrap narrow'>
          <Reveal>
            <div className='kick'>
              <span className='n'>05</span>
              TONIGHT
            </div>
            <div className='big'>
              <CountUp to={models.length} />
            </div>
            <div className='row'>
              <div>
                <b>{fmtInt(models.length)}</b>
                <span>可用模型</span>
              </div>
              <div>
                <b>{fmtInt(freeCount)}</b>
                <span>免费模型</span>
              </div>
              <div>
                <b>{fmtInt(marketplace.data?.total ?? 0)}</b>
                <span>在售分组</span>
              </div>
            </div>
          </Reveal>
        </div>
        <footer>
          <span className='flogo'>
            <span className='dot'>G</span>
            {brand}
          </span>
          <span>AI 网关与代码平台</span>
          <span>© {new Date().getFullYear()}</span>
        </footer>
      </section>
    </div>
  )
}
