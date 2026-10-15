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
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { normalizeSystemName } from '@/lib/branding'
import { Markdown } from '@/components/ui/markdown'
import { SiteSeo } from '@/components/seo'
import { useHomePageContent } from '@/features/home/hooks'
import { useMarketplaceGroups } from '@/features/marketplace/hooks'
import {
  localizedGroupName,
  localizedSourceLabel,
} from '@/features/marketplace/lib/localized-group-name'
import { usePricingData } from '@/features/pricing/hooks/use-pricing-data'
import { availablePricingModels } from '@/features/pricing/lib/merge-pricing-models'
import { countFreeModels } from '@/features/pricing/lib/model-helpers'
import { formatPrice } from '@/features/pricing/lib/price'
import { DawnNav } from '../components/dawn-nav'
import { CountUp, Reveal } from '../components/reveal'
import { compactCount, fmtInt, pct, sec } from '../lib/format'

const COMMUNITY_URL = 'https://community.codegoai.com'

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
  const { t, i18n } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const { content, isLoaded, isUrl } = useHomePageContent()
  const marketplace = useMarketplaceGroups(HOME_FILTERS)
  const pricing = usePricingData()

  const groups = useMemo(
    () => marketplace.data?.items ?? [],
    [marketplace.data]
  )

  const models = useMemo(
    () =>
      availablePricingModels(
        pricing.models,
        pricing.pricedModelDetails,
        pricing.availableModels
      )
        .filter((model) => model.model_name)
        .filter(
          (model) => !model.model_name.toLowerCase().includes('embedding')
        ),
    [pricing.models, pricing.pricedModelDetails, pricing.availableModels]
  )
  const freeCount = countFreeModels(models, pricing.groupRatio)

  const aggregates = useMemo(() => {
    const withTraffic = groups.filter((group) => group.request_count > 0)
    const totalRequests = groups.reduce((sum, g) => sum + g.request_count, 0)
    let availability: number | null = null
    let ttft: number | null = null
    if (withTraffic.length) {
      availability =
        withTraffic.reduce((sum, g) => sum + g.success_rate * 100, 0) /
        withTraffic.length
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
      hero.querySelectorAll<HTMLElement>('[data-speed]').forEach((element) => {
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
        <SiteSeo
          title={`${normalizeSystemName()} | AI API`}
          description={t(
            'AI coding gateway, group marketplace and model pricing'
          )}
          canonicalPath='/'
        />
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
      <SiteSeo
        title={`${brand} | ${t('AI coding gateway')}`}
        description={t(
          'AI coding gateway, group marketplace and model pricing'
        )}
        canonicalPath='/'
      />
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
            {t('One connection,')}
            <br />
            <span className='gold'>{t('every model.')}</span>
          </h1>
          <div className='cta reveal in'>
            <Link className='btn primary' to='/market'>
              {t('Explore the market')} <ArrowUpRight size={16} />
            </Link>
            <Link className='btn' to={user ? '/dashboard' : '/sign-in'}>
              {user ? t('My console') : `${t('Sign in')} / ${t('Sign up')}`}
            </Link>
            <a
              className='btn community-cta'
              href={COMMUNITY_URL}
              target='_blank'
              rel='noopener noreferrer'
            >
              {t('Visit the community')} <ArrowRight size={15} />
            </a>
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
              <span className='o'>{t('One connection, every model')}</span>{' '}
              <Sunrise />
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
            {t('Every model,')} <em>{t('ready to compare')}</em>.
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
                <h2>{t('Live now')}</h2>
              </div>
            </div>
          </Reveal>
          <Reveal>
            <div className='statband'>
              <div className='cell'>
                <b>
                  <CountUp to={marketplace.data?.total ?? 0} />
                </b>
                <span>{t('Groups for sale')}</span>
              </div>
              <div className='cell'>
                <b>
                  <CountUp to={models.length} />
                </b>
                <span>{t('Available models')}</span>
              </div>
              <div className='cell'>
                <b>
                  <CountUp to={aggregates.totalRequests} />
                </b>
                <span>{t('24h requests')}</span>
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
                <span>{t('24h success rate')}</span>
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
                <span>{t('Average time to first token')}</span>
              </div>
            </div>
          </Reveal>
          {tickerModels.length > 0 && (
            <Reveal>
              <div className='ticker'>
                <span className='label'>
                  <Activity size={13} />
                  {t('Market activity')}
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
                            ? t('Usage based')
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
            <span className='stroke'>{t('Routing')}</span>,{' '}
            <em>{t('made visible')}</em>.
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
                <h2>{t('Trending')}</h2>
              </div>
              <Link className='btn' to='/market'>
                {t('All {{count}} groups', {
                  count: fmtInt(marketplace.data?.total ?? 0),
                })}{' '}
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
                    <span className='src'>
                      {localizedSourceLabel(group.source_label, i18n.language)}
                    </span>
                    <h3>
                      {localizedGroupName(
                        group.system_display_name,
                        group.source_label,
                        i18n.language
                      )}
                    </h3>
                    <div className='pr'>
                      {group.multiplier}
                      <span> × {t('multiplier')}</span>
                    </div>
                    <div className='hm'>
                      <span className='g'>
                        {t('24h success')}
                        <b>{pct(group.success_rate)}%</b>
                      </span>
                      <span>
                        {t('First token')}
                        <b>{sec(group.avg_ttft_ms)}s</b>
                      </span>
                      <span>
                        {t('Models')}
                        <b>{group.models.length}</b>
                      </span>
                      <span>
                        24H<b>{compactCount(group.request_count)}</b>
                      </span>
                    </div>
                    <span className='go'>
                      {t('Enter')} <ArrowRight size={14} />
                    </span>
                  </Link>
                ))}
              </div>
            ) : (
              <div className='empty'>
                <span className='eic'>
                  <Sunrise size={20} />
                </span>
                <b>{t('Market groups are coming soon')}</b>
                <span>
                  {t('{{count}} model prices are ready', { count: freeCount })}
                </span>
                <Link className='btn mini' to='/pricing'>
                  {t('View model prices')} <ArrowRight size={13} />
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
                <span>{t('Available models')}</span>
              </div>
              <div>
                <b>{fmtInt(freeCount)}</b>
                <span>{t('Free models')}</span>
              </div>
              <div>
                <b>{fmtInt(marketplace.data?.total ?? 0)}</b>
                <span>{t('Groups for sale')}</span>
              </div>
            </div>
          </Reveal>
        </div>
        <footer>
          <span className='flogo'>
            <span className='dot'>G</span>
            {brand}
          </span>
          <span>{t('AI gateway and coding platform')}</span>
          <span>© {new Date().getFullYear()}</span>
        </footer>
      </section>
    </div>
  )
}
