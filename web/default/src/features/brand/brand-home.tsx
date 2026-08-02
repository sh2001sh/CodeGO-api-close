import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'
import { PublicLayout } from '@/components/layout'
import { Footer } from '@/components/layout/components/footer'
import { SiteSeo } from '@/components/seo'
import { getPricing } from '@/features/pricing/api'
import {
  countFreeModels,
  getFreeEligibleGroups,
} from '@/features/pricing/lib/model-helpers'
import type { PricingModel } from '@/features/pricing/types'
import { OffersSection, SiteOverviewSection } from './brand-sections'
import { DawnHero } from './dawn-hero'

type HomeModel = Pick<PricingModel, 'model_name' | 'vendor_icon' | 'tags'> &
  Partial<Pick<PricingModel, 'enable_groups'>>

const fallbackModels = [
  { model_name: 'GPT-4o', vendor_icon: 'OpenAI', tags: 'multimodal' },
  { model_name: 'Claude-3.5-Sonnet', vendor_icon: 'Claude', tags: 'reasoning' },
  { model_name: 'DeepSeek-V3', vendor_icon: 'DeepSeek', tags: 'code' },
  { model_name: 'Gemini-Pro', vendor_icon: 'Gemini', tags: 'long context' },
  { model_name: 'Qwen-Plus', vendor_icon: 'Qwen', tags: '中文' },
  { model_name: 'Llama-3', vendor_icon: 'Meta', tags: 'open' },
] as HomeModel[]

function getModelTag(modelName: string, tags?: string) {
  const source = `${modelName} ${tags ?? ''}`.toLowerCase()
  if (source.includes('claude')) return 'Claude'
  if (source.includes('codex')) return 'Codex'
  if (source.includes('deepseek') || source.includes('code')) return '代码'
  if (source.includes('gemini')) return '长上下文'
  if (source.includes('gpt')) return 'GPT'
  return 'API'
}

function ModelMarquee({
  models,
  groupRatios,
  reverse,
}: {
  models: HomeModel[]
  groupRatios: Record<string, number>
  reverse?: boolean
}) {
  const loopModels = [...models, ...models, ...models, ...models]
  return (
    <div className='home-marquee-row'>
      <div className={cn('home-marquee-track', reverse && 'is-reverse')}>
        {loopModels.map((model, index) => {
          const freeGroups = model.enable_groups
            ? getFreeEligibleGroups(model as PricingModel, groupRatios)
            : []
          const isFree = freeGroups.length > 0
          return (
            <div
              key={`${model.model_name}-${index}`}
              className={cn('home-model-chip', isFree && 'is-free')}
            >
              <span className='home-model-icon'>
                {model.vendor_icon
                  ? getLobeIcon(model.vendor_icon, 18)
                  : model.model_name.slice(0, 1)}
              </span>
              <span className='truncate font-mono text-sm font-semibold'>
                {model.model_name}
              </span>
              <span className='home-model-tag'>
                {isFree ? '免费' : getModelTag(model.model_name, model.tags)}
              </span>
            </div>
          )
        })}
      </div>
    </div>
  )
}

export function BrandHome() {
  const { data: pricingData } = useQuery({
    queryKey: ['pricing'],
    queryFn: getPricing,
    staleTime: 5 * 60 * 1000,
  })
  const models = useMemo(() => {
    const source =
      pricingData?.data
        ?.filter(
          (model) =>
            model.model_name &&
            !model.model_name.toLowerCase().includes('embedding')
        )
        .slice(0, 18) ?? []
    return source.length > 0 ? source : fallbackModels
  }, [pricingData])
  const tracks = useMemo(() => {
    const midpoint = Math.ceil(models.length / 2)
    return [models.slice(0, midpoint), models.slice(midpoint)]
  }, [models])
  const groupRatios = pricingData?.group_ratio ?? {}
  const freeCount = pricingData
    ? countFreeModels(pricingData.data, groupRatios)
    : 0

  const modelRail = (
    <div className='dawn-model-rail'>
      <div className='mb-3 flex items-center justify-between px-2 text-xs text-white/48'>
        <span>实时模型储备</span>
        <span>
          {models.length}+ 模型
          {freeCount > 0 ? ` · ${freeCount} 免费` : ''}
        </span>
      </div>
      <div className='home-marquee-shell dawn-marquee-shell'>
        <ModelMarquee
          models={tracks[0] ?? fallbackModels}
          groupRatios={groupRatios}
        />
        <ModelMarquee
          models={
            tracks[1]?.length ? tracks[1] : fallbackModels.slice().reverse()
          }
          groupRatios={groupRatios}
          reverse
        />
      </div>
    </div>
  )

  return (
    <PublicLayout
      showMainContainer={false}
      headerProps={{ className: 'eclipse-public-header' }}
    >
      <SiteSeo
        title='CodeGo | Codex API、Claude Code API、Codex 中转、Claude 中转'
        description='CodeGo（Code Go）是面向长期 AI Coding 的统一入口，覆盖 Codex API、Claude Code API、免费模型、模型广场、套餐与桌面端配置。'
        keywords='CodeGo, Code Go, Codex API, Claude Code API, Codex中转, Claude中转, 免费模型, AI Coding'
        canonicalPath='/'
      />
      <main className='bg-background overflow-hidden'>
        <DawnHero modelRail={modelRail} />
        <OffersSection />
        <SiteOverviewSection />
      </main>
      <Footer />
    </PublicLayout>
  )
}
