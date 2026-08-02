import { useMemo, useState } from 'react'
import { Apple, ChevronDown, ExternalLink, Laptop, Monitor } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { formatFileSize, RELEASE_PAGE_URL } from './lib'
import type { DesktopPlatform, DesktopRelease, DownloadCard } from './types'

type DownloadPanelProps = {
  cards: DownloadCard[]
  recommendedCard: DownloadCard | null
  detectedPlatform: DesktopPlatform
  release?: DesktopRelease
}

const PLATFORM_ORDER = ['windows', 'macos', 'linux'] as const

const PLATFORM_ICONS = {
  windows: Monitor,
  macos: Apple,
  linux: Laptop,
}

function getInitialCard(
  cards: DownloadCard[],
  recommendedCard: DownloadCard | null,
  platform: DesktopPlatform
) {
  return (
    recommendedCard ??
    cards.find((card) => card.platform === platform) ??
    cards[0] ??
    null
  )
}

export function DownloadPanel({
  cards,
  recommendedCard,
  detectedPlatform,
  release,
}: DownloadPanelProps) {
  const { t } = useTranslation()
  const initialCard = getInitialCard(cards, recommendedCard, detectedPlatform)
  const [selectedKey, setSelectedKey] = useState(initialCard?.key ?? '')
  const [detailsOpen, setDetailsOpen] = useState(false)

  const selectedCard =
    cards.find((card) => card.key === selectedKey) ?? initialCard
  const platformCards = useMemo(
    () =>
      PLATFORM_ORDER.map((platform) => ({
        platform,
        card:
          cards.find(
            (card) =>
              card.platform === platform &&
              (platform !== 'macos' || card.key === selectedKey)
          ) ?? cards.find((card) => card.platform === platform),
      })).filter((item) => item.card),
    [cards, selectedKey]
  )

  if (!selectedCard) return null

  const SelectedIcon = PLATFORM_ICONS[selectedCard.platform]
  const macCards = cards.filter((card) => card.platform === 'macos')
  const isRecommended = recommendedCard?.key === selectedCard.key

  return (
    <section className='border-border bg-card overflow-hidden rounded-2xl border shadow-sm'>
      <div
        className='border-border flex overflow-x-auto border-b p-2'
        role='tablist'
        aria-label={t('Choose your platform')}
      >
        {platformCards.map(({ platform, card }) => {
          if (!card) return null
          const Icon = PLATFORM_ICONS[platform]
          const active = selectedCard.platform === platform
          return (
            <button
              key={platform}
              type='button'
              role='tab'
              aria-selected={active}
              className={cn(
                'focus-visible:ring-ring flex min-w-28 flex-1 items-center justify-center gap-2 rounded-lg px-4 py-2.5 text-sm font-medium transition-colors focus-visible:ring-2 focus-visible:outline-none',
                active
                  ? 'bg-foreground text-background'
                  : 'text-muted-foreground hover:bg-muted hover:text-foreground'
              )}
              onClick={() => setSelectedKey(card.key)}
            >
              <Icon className='size-4' />
              {platform === 'macos' ? 'macOS' : t(card.title)}
            </button>
          )
        })}
      </div>

      <div className='p-6 sm:p-8'>
        <div className='flex flex-col gap-7 sm:flex-row sm:items-start sm:justify-between'>
          <div className='max-w-xl space-y-4'>
            <div className='flex items-center gap-3'>
              <span className='bg-muted inline-flex size-11 items-center justify-center rounded-xl'>
                <SelectedIcon className='size-5' />
              </span>
              <div>
                <div className='flex flex-wrap items-center gap-2'>
                  <h2 className='text-xl font-semibold'>
                    {selectedCard.title}
                  </h2>
                  {isRecommended ? (
                    <span className='text-primary text-xs font-medium'>
                      {t('Recommended for this device')}
                    </span>
                  ) : null}
                </div>
                <p className='text-muted-foreground mt-0.5 text-sm'>
                  {[
                    release?.tag_name,
                    selectedCard.archLabel || selectedCard.arch,
                  ]
                    .filter(Boolean)
                    .join(' · ')}
                </p>
              </div>
            </div>
            <p className='text-muted-foreground text-sm leading-6'>
              {selectedCard.description}
            </p>
            {selectedCard.platform === 'macos' && macCards.length > 1 ? (
              <div
                className='flex flex-wrap gap-2'
                aria-label={t('Mac architecture')}
              >
                {macCards.map((card) => (
                  <button
                    key={card.key}
                    type='button'
                    className={cn(
                      'focus-visible:ring-ring rounded-full border px-3 py-1.5 text-xs font-medium focus-visible:ring-2 focus-visible:outline-none',
                      card.key === selectedCard.key
                        ? 'border-primary bg-primary/8 text-primary'
                        : 'border-border text-muted-foreground hover:text-foreground'
                    )}
                    onClick={() => setSelectedKey(card.key)}
                  >
                    {card.archLabel || card.arch}
                  </button>
                ))}
              </div>
            ) : null}
          </div>

          <div className='w-full shrink-0 sm:w-64'>
            <Button
              size='lg'
              className='w-full justify-center'
              variant={selectedCard.fallback ? 'outline' : 'default'}
              render={
                <a
                  href={selectedCard.href}
                  target='_blank'
                  rel='noopener noreferrer'
                />
              }
            >
              {selectedCard.cta}
              {selectedCard.fallback ? (
                <ExternalLink className='size-4' />
              ) : null}
            </Button>
            <p className='text-muted-foreground mt-2 text-center text-xs'>
              {selectedCard.fileName
                ? `${selectedCard.fileName} · ${formatFileSize(selectedCard.fileSize ?? 0)}`
                : t('Opens the latest release page')}
            </p>
          </div>
        </div>

        <Collapsible open={detailsOpen} onOpenChange={setDetailsOpen}>
          <CollapsibleTrigger className='text-muted-foreground hover:text-foreground focus-visible:ring-ring mt-7 flex items-center gap-1.5 text-sm font-medium focus-visible:ring-2 focus-visible:outline-none'>
            {t('More download details')}
            <ChevronDown
              className={cn(
                'size-4 transition-transform',
                detailsOpen && 'rotate-180'
              )}
            />
          </CollapsibleTrigger>
          <CollapsibleContent className='border-border mt-4 border-t pt-4'>
            <dl className='grid gap-4 text-sm sm:grid-cols-2'>
              <div>
                <dt className='text-muted-foreground'>
                  {t('SHA256 checksum')}
                </dt>
                <dd className='mt-1 font-mono text-xs break-all'>
                  {selectedCard.digest?.replace('sha256:', '') ||
                    t('Not provided')}
                </dd>
              </div>
              <div>
                <dt className='text-muted-foreground'>{t('Other packages')}</dt>
                <dd className='mt-1'>
                  <a
                    className='text-primary inline-flex items-center gap-1 font-medium hover:underline'
                    href={release?.html_url || RELEASE_PAGE_URL}
                    target='_blank'
                    rel='noopener noreferrer'
                  >
                    {t('Browse all release assets')}
                    <ExternalLink className='size-3.5' />
                  </a>
                </dd>
              </div>
            </dl>
          </CollapsibleContent>
        </Collapsible>
      </div>
    </section>
  )
}
