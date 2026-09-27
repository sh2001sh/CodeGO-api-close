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

For commercial licensing, please contact support@quantumnous.com
*/
import { useQuery } from '@tanstack/react-query'
import i18n from '@/i18n/config'
import { useTranslation, Trans } from 'react-i18next'
import { getPublicPageSeoEntry } from '@/lib/public-page-seo'
import { Markdown } from '@/components/ui/markdown'
import { PublicLayout } from '@/components/layout'
import { SiteSeo } from '@/components/seo'
import { getAboutContent } from './api'

const aboutSeo = getPublicPageSeoEntry('/about')

function AboutHero() {
  const { t } = useTranslation()
  return (
    <div className='space-y-4'>
      <div className='space-y-3'>
        <h1 className='text-foreground text-4xl font-semibold tracking-tight md:text-5xl'>
          {t(aboutSeo.h1)}
        </h1>
      </div>
    </div>
  )
}

function SupportGroupCard() {
  useTranslation()
  return (
    <div className='border-border bg-card text-card-foreground overflow-hidden rounded-xl border'>
      <div className='grid gap-6 p-6 md:grid-cols-[minmax(0,1fr)_240px] md:items-center'>
        <div className='space-y-3'>
          <div className='text-primary text-xs font-semibold tracking-[0.24em] uppercase'>
            <Trans i18nKey={'售后支持'} />
          </div>
          <h2 className='text-foreground text-2xl font-semibold tracking-tight'>
            <Trans i18nKey={'售后 QQ 群'} />
          </h2>
          <div className='bg-muted/60 text-foreground rounded-lg px-4 py-3 text-sm leading-7'>
            <Trans i18nKey={'群号：'} />
            <span className='font-semibold'>996040309</span>
          </div>
        </div>

        <div className='border-border bg-background mx-auto w-full max-w-[220px] rounded-xl border p-3'>
          <img
            src='/guide/16-support-qq-group.png'
            alt={i18n.t('Code Go 售后 QQ 群二维码')}
            className='h-auto w-full rounded-lg'
            loading='lazy'
          />
        </div>
      </div>
    </div>
  )
}

function isValidUrl(value: string) {
  try {
    const url = new URL(value)
    return url.protocol === 'http:' || url.protocol === 'https:'
  } catch {
    return false
  }
}

function isLikelyHtml(value: string) {
  return /<\/?[a-z][\s\S]*>/i.test(value)
}

export function About() {
  const { t, i18n: currentI18n } = useTranslation()
  const { data, isError, isLoading } = useQuery({
    queryKey: ['about-content'],
    queryFn: getAboutContent,
  })

  const rawContent = data?.data?.trim() ?? ''
  const hasContent =
    rawContent.length > 0 &&
    (currentI18n.language === 'zh' || !/[\u3400-\u9fff]/u.test(rawContent))
  const isUrl = hasContent && isValidUrl(rawContent)
  const isHtml = hasContent && !isUrl && isLikelyHtml(rawContent)

  if (isLoading || !hasContent || isError) {
    return (
      <PublicLayout>
        <SiteSeo
          title={aboutSeo.title}
          description={aboutSeo.description}
          keywords={aboutSeo.keywords}
          canonicalPath={aboutSeo.path}
        />
        <div className='mx-auto max-w-6xl space-y-6 px-4 py-8'>
          <AboutHero />
          <SupportGroupCard />
          <Markdown className='codego-public-prose prose-neutral dark:prose-invert max-w-none'>
            {t('about.fallbackMarkdown')}
          </Markdown>
        </div>
      </PublicLayout>
    )
  }

  if (isUrl) {
    return (
      <PublicLayout showMainContainer={false}>
        <SiteSeo
          title={aboutSeo.title}
          description={aboutSeo.description}
          keywords={aboutSeo.keywords}
          canonicalPath={aboutSeo.path}
        />
        <div className='space-y-4 px-4 py-6 md:px-6'>
          <div className='mx-auto max-w-6xl space-y-6'>
            <AboutHero />
            <SupportGroupCard />
          </div>
          <iframe
            src={rawContent}
            className='h-[calc(100vh-18rem)] w-full border-0'
            title={i18n.t('Code Go 关于内容')}
          />
        </div>
      </PublicLayout>
    )
  }

  return (
    <PublicLayout>
      <SiteSeo
        title={aboutSeo.title}
        description={aboutSeo.description}
        keywords={aboutSeo.keywords}
        canonicalPath={aboutSeo.path}
      />
      <div className='mx-auto max-w-6xl space-y-6 px-4 py-8'>
        <AboutHero />
        <SupportGroupCard />
        {isHtml ? (
          <div
            className='prose prose-neutral dark:prose-invert max-w-none'
            dangerouslySetInnerHTML={{ __html: rawContent }}
          />
        ) : (
          <Markdown className='codego-public-prose prose-neutral dark:prose-invert max-w-none'>
            {rawContent}
          </Markdown>
        )}
      </div>
    </PublicLayout>
  )
}
