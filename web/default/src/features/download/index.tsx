import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowRight, Loader2 } from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'
import { useTranslation } from 'react-i18next'
import { getPublicPageSeoEntry } from '@/lib/public-page-seo'
import { Button } from '@/components/ui/button'
import { PublicLayout } from '@/components/layout'
import { SiteSeo } from '@/components/seo'
import { DownloadPanel } from './download-panel'
import {
  detectDesktopArchitecture,
  detectDesktopPlatform,
  RELEASES_URL,
} from './lib'
import type { DesktopDevice, DesktopRelease, DownloadCopy } from './types'
import { buildDownloadPageViewModel } from './view-model'

const downloadSeo = getPublicPageSeoEntry('/download')

function getDownloadCopy(t: (key: string) => string): DownloadCopy {
  return {
    windowsTitle: t('Windows'),
    windowsDescription: t(
      'Installer with automatic updates for Windows 10 and above.'
    ),
    windowsCta: t('Download for Windows'),
    appleSiliconLabel: t('Apple Silicon'),
    intelLabel: t('Intel'),
    macosAppleSiliconTitle: t('macOS Apple Silicon'),
    macosAppleSiliconDescription: t(
      'Signed DMG installer for macOS 12+ on Apple Silicon (M-series).'
    ),
    macosAppleSiliconCta: t('Download for Apple Silicon'),
    macosIntelTitle: t('macOS Intel'),
    macosIntelDescription: t(
      'Signed DMG installer for macOS 12+ on Intel Macs.'
    ),
    macosIntelCta: t('Download for Intel Mac'),
    macosFallbackCta: t('Install with Homebrew'),
    linuxTitle: t('Linux'),
    linuxDescription: t(
      'Portable AppImage for mainstream Linux distributions, with .deb and .rpm assets in the release.'
    ),
    linuxCta: t('Download for Linux'),
    linuxFallbackCta: t('Browse Linux assets'),
    releaseFallbackCta: t('Open release'),
  }
}

function detectDevice(): DesktopDevice {
  if (typeof navigator === 'undefined') {
    return { platform: 'unknown', arch: 'unknown' }
  }
  const platform = detectDesktopPlatform(navigator.userAgent)
  return {
    platform,
    arch: detectDesktopArchitecture(navigator.userAgent, platform),
  }
}

export function DownloadPage() {
  const { i18n, t } = useTranslation()
  const reduceMotion = Boolean(useReducedMotion())
  const releaseQuery = useQuery({
    queryKey: ['desktop-download-release', i18n.language, t],
    queryFn: async () => {
      const response = await fetch(RELEASES_URL, {
        headers: {
          Accept: 'application/json',
          'User-Agent': 'code-go-download-page',
        },
      })
      if (!response.ok)
        throw new Error(t('Failed to contact the desktop release channel'))
      const release = (await response.json()) as DesktopRelease
      if (!release?.tag_name || !Array.isArray(release.assets)) {
        throw new Error(t('Unexpected release payload'))
      }
      return release
    },
    staleTime: 5 * 60 * 1000,
  })
  const copy = useMemo(() => getDownloadCopy(t), [t])
  const device = useMemo(() => detectDevice(), [])
  const viewModel = useMemo(
    () =>
      buildDownloadPageViewModel({
        release: releaseQuery.data,
        copy,
        device,
        isLoading: releaseQuery.isLoading,
        error: releaseQuery.error,
      }),
    [
      copy,
      device,
      releaseQuery.data,
      releaseQuery.error,
      releaseQuery.isLoading,
    ]
  )
  const setupSteps = [
    [
      t('Install Code Go'),
      t(
        'Download the build for your platform and finish the standard installation.'
      ),
    ],
    [
      t('Authorize in browser'),
      t(
        'Sign in through the website and approve this desktop device without sharing your password with the app.'
      ),
    ],
    [
      t('Apply your tools'),
      t(
        'Import a token, preview the changes, and configure your local AI coding tools in one place.'
      ),
    ],
  ]

  return (
    <PublicLayout showMainContainer={false}>
      <SiteSeo {...downloadSeo} canonicalPath={downloadSeo.path} />
      <main className='bg-background min-h-screen pt-28 pb-20'>
        <motion.div
          className='mx-auto w-full max-w-5xl px-4 md:px-6'
          initial={reduceMotion ? false : { opacity: 0, y: 16 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.45, ease: [0.22, 1, 0.36, 1] }}
        >
          <header className='mx-auto max-w-3xl text-center'>
            <p className='text-primary text-sm font-medium'>
              {t('Code Go Desktop')}
            </p>
            <h1 className='mt-3 text-4xl font-semibold tracking-tight text-balance md:text-5xl'>
              {t('Your AI tools, configured locally.')}
            </h1>
            <p className='text-muted-foreground mx-auto mt-4 max-w-2xl text-base leading-7 md:text-lg'>
              {t(
                'Securely import your Code Go token and configure Codex, Claude Code, Gemini CLI, and more from one desktop app.'
              )}
            </p>
          </header>

          <div className='mt-10'>
            <DownloadPanel
              cards={viewModel.downloadCards}
              recommendedCard={viewModel.recommendedCard}
              detectedPlatform={device.platform}
              release={releaseQuery.data}
            />
            {viewModel.isLoading ? (
              <p className='text-muted-foreground mt-3 flex items-center justify-center gap-2 text-sm'>
                <Loader2 className='size-4 animate-spin' />
                {t('Checking the latest release...')}
              </p>
            ) : null}
            {viewModel.errorMessage ? (
              <p className='text-destructive mt-3 text-center text-sm'>
                {viewModel.errorMessage} ·{' '}
                {t('Release page links are still available.')}
              </p>
            ) : null}
          </div>

          <section className='mt-16'>
            <div className='flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between'>
              <div>
                <p className='text-primary text-sm font-medium'>
                  {t('Three steps')}
                </p>
                <h2 className='mt-2 text-2xl font-semibold tracking-tight'>
                  {t('Ready in a few minutes')}
                </h2>
              </div>
              <Button
                variant='ghost'
                render={<Link to='/sign-in' search={{ redirect: '/keys' }} />}
              >
                {t('Open token console')}
                <ArrowRight className='size-4' />
              </Button>
            </div>
            <ol className='border-border mt-6 grid border-y sm:grid-cols-3'>
              {setupSteps.map(([title, description], index) => (
                <li
                  key={title}
                  className='border-border py-6 max-sm:border-t max-sm:first:border-t-0 sm:border-l sm:px-6 sm:first:border-l-0 sm:first:pl-0 sm:last:pr-0'
                >
                  <span className='text-primary font-mono text-xs'>
                    0{index + 1}
                  </span>
                  <h3 className='mt-3 font-semibold'>{title}</h3>
                  <p className='text-muted-foreground mt-2 text-sm leading-6'>
                    {description}
                  </p>
                </li>
              ))}
            </ol>
          </section>
        </motion.div>
      </main>
    </PublicLayout>
  )
}
