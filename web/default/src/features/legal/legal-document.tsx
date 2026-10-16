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
import { FileWarning } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { CompanyIdentity } from '@/components/company-identity'
import { PublicLayout } from '@/components/layout'
import { SiteSeo } from '@/components/seo'
import '@/features/docs/reading.css'
import type { LegalDocumentResponse } from './types'

type LegalDocumentProps = {
  title: string
  seoTitle: string
  seoDescription: string
  seoKeywords: string
  canonicalPath: string
  queryKey: string
  fetchDocument: () => Promise<LegalDocumentResponse>
  emptyMessage: string
  fallbackContent?: string
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

function DocumentHeader(props: { title: string; description: string }) {
  return (
    <div className='solaris-document-heading space-y-3'>
      <h1 className='text-3xl font-semibold tracking-tight'>{props.title}</h1>
    </div>
  )
}

export function LegalDocument(props: LegalDocumentProps) {
  const { t } = useTranslation()
  const { data, isLoading } = useQuery({
    queryKey: [props.queryKey],
    queryFn: props.fetchDocument,
    staleTime: 10 * 60 * 1000,
  })

  const rawContent = data?.data?.trim() ?? ''
  const hasContent = rawContent.length > 0
  const success = data?.success ?? false

  if (isLoading) {
    return (
      <PublicLayout>
        <SiteSeo
          title={props.seoTitle}
          description={props.seoDescription}
          keywords={props.seoKeywords}
          canonicalPath={props.canonicalPath}
        />
        <div className='market-reading market-reading-width flex flex-col gap-4'>
          <DocumentHeader
            title={props.title}
            description={t(props.seoDescription)}
          />
          <Skeleton className='h-8 w-[45%]' />
          <Skeleton className='h-4 w-full' />
          <Skeleton className='h-4 w-[90%]' />
          <Skeleton className='h-4 w-[80%]' />
        </div>
      </PublicLayout>
    )
  }

  const displayContent = hasContent
    ? rawContent
    : (props.fallbackContent?.trim() ?? '')
  const displayIsUrl = displayContent.length > 0 && isValidUrl(displayContent)
  const displayIsHtml =
    displayContent.length > 0 && !displayIsUrl && isLikelyHtml(displayContent)

  if (!success && !displayContent) {
    return (
      <PublicLayout>
        <SiteSeo
          title={props.seoTitle}
          description={props.seoDescription}
          keywords={props.seoKeywords}
          canonicalPath={props.canonicalPath}
        />
        <div className='market-reading market-reading-width space-y-8'>
          <DocumentHeader
            title={props.title}
            description={t(props.seoDescription)}
          />
          <Card className='border-dashed'>
            <CardHeader className='flex flex-row items-center gap-4'>
              <div className='bg-muted rounded-lg p-2'>
                <FileWarning className='text-muted-foreground h-5 w-5' />
              </div>
              <div className='space-y-1'>
                <CardTitle className='text-lg font-semibold'>
                  {props.title}
                </CardTitle>
                <p className='text-muted-foreground text-sm'>
                  {data?.message || props.emptyMessage}
                </p>
              </div>
            </CardHeader>
          </Card>
        </div>
      </PublicLayout>
    )
  }

  if (displayIsUrl) {
    return (
      <PublicLayout>
        <SiteSeo
          title={props.seoTitle}
          description={props.seoDescription}
          keywords={props.seoKeywords}
          canonicalPath={props.canonicalPath}
        />
        <div className='market-reading market-reading-width space-y-8'>
          <DocumentHeader
            title={props.title}
            description={t(props.seoDescription)}
          />
          <Card>
            <CardHeader>
              <CardTitle>{props.title}</CardTitle>
            </CardHeader>
            <CardContent className='space-y-4'>
              <Button
                render={
                  <a
                    href={displayContent}
                    target='_blank'
                    rel='noopener noreferrer'
                  />
                }
              >
                {t('View document')}
              </Button>
            </CardContent>
          </Card>
        </div>
      </PublicLayout>
    )
  }

  return (
    <PublicLayout>
      <SiteSeo
        title={props.seoTitle}
        description={t(props.seoDescription)}
        keywords={props.seoKeywords}
        canonicalPath={props.canonicalPath}
      />
      <div className='market-reading market-reading-width space-y-8'>
        <DocumentHeader
          title={props.title}
          description={t(props.seoDescription)}
        />

        <CompanyIdentity />
        {displayIsHtml ? (
          <div
            className='prose prose-neutral dark:prose-invert max-w-none'
            dangerouslySetInnerHTML={{ __html: displayContent }}
          />
        ) : (
          <div className='prose prose-neutral dark:prose-invert max-w-none'>
            <ReactMarkdown
              remarkPlugins={[remarkGfm]}
              components={{
                h1: (heading) => <h2>{heading.children}</h2>,
                a: (link) => (
                  <a href={link.href} target='_blank' rel='noopener noreferrer'>
                    {link.children}
                  </a>
                ),
              }}
            >
              {displayContent}
            </ReactMarkdown>
          </div>
        )}
      </div>
    </PublicLayout>
  )
}
