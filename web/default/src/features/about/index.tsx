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
import { getPublicPageSeoEntry } from '@/lib/public-page-seo'
import { Markdown } from '@/components/ui/markdown'
import { PublicLayout } from '@/components/layout'
import { SiteSeo } from '@/components/seo'
import { getAboutContent } from './api'

const aboutSeo = getPublicPageSeoEntry('/about')

const fallbackAboutMarkdown = `## 品牌主张

让 AI 编程的每一步，都算数。

## Code Go 在做什么

Code Go 让 AI 编程更适合长期使用。

## 为什么这样做

如果你长期使用 Codex、Claude Code 或多模型工作流，你会需要一个更稳定的使用入口。

## Code Go 的差异化

- 不只是接入模型
- 不只是管理额度
- 也不只是看调用结果

我们更关心的是：你每天做 AI 编程时，是否能感受到进度在持续累积。

## 适合谁

- 长期使用 Codex 的开发者
- 长期使用 Claude Code 的开发者
- 需要多模型、额度管理、成长反馈和工作流记录的团队

## 对外表达

如果只用一句话介绍 Code Go，就是：

**让 AI 编程的每一步，都算数。**
`

function AboutHero() {
  return (
    <div className='space-y-4'>
      <div className='border-primary/20 bg-primary/8 text-primary inline-flex items-center rounded-full border px-3 py-1 text-xs font-semibold'>
        {aboutSeo.eyebrow}
      </div>
      <div className='space-y-3'>
        <h1 className='text-foreground text-4xl font-semibold tracking-tight md:text-5xl'>
          {aboutSeo.h1}
        </h1>
        <p className='text-muted-foreground max-w-3xl text-base leading-8 md:text-lg'>
          {aboutSeo.intro}
        </p>
      </div>
    </div>
  )
}

function SupportGroupCard() {
  return (
    <div className='border-border bg-card text-card-foreground overflow-hidden rounded-3xl border shadow-sm'>
      <div className='grid gap-6 p-6 md:grid-cols-[minmax(0,1fr)_240px] md:items-center'>
        <div className='space-y-3'>
          <div className='text-primary text-xs font-semibold tracking-[0.24em] uppercase'>
            售后支持
          </div>
          <h2 className='text-foreground text-2xl font-semibold tracking-tight'>
            售后 QQ 群
          </h2>
          <p className='text-muted-foreground text-sm leading-7'>
            注册、套餐、盲盒、宠物升级、脚本配置或控制台使用遇到问题时，可以直接进群处理。
          </p>
          <div className='bg-muted/60 text-foreground rounded-2xl px-4 py-3 text-sm leading-7'>
            群号：<span className='font-semibold'>996040309</span>
          </div>
        </div>

        <div className='border-border bg-background mx-auto w-full max-w-[220px] rounded-3xl border p-3'>
          <img
            src='/guide/16-support-qq-group.png'
            alt='Code Go 售后 QQ 群二维码'
            className='h-auto w-full rounded-2xl'
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
  const { data, isError, isLoading } = useQuery({
    queryKey: ['about-content'],
    queryFn: getAboutContent,
  })

  const rawContent = data?.data?.trim() ?? ''
  const hasContent = rawContent.length > 0
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
          <Markdown className='prose-neutral dark:prose-invert max-w-none'>
            {fallbackAboutMarkdown}
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
            <p className='text-muted-foreground max-w-3xl text-sm leading-7'>
              当前关于内容由外部地址承载。为了保持公开页结构稳定，这里会保留统一标题、说明和支持入口，再跳转到外部内容容器展示。
            </p>
          </div>
          <iframe
            src={rawContent}
            className='h-[calc(100vh-18rem)] w-full border-0'
            title='Code Go 关于内容'
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
          <Markdown className='prose-neutral dark:prose-invert max-w-none'>
            {rawContent}
          </Markdown>
        )}
      </div>
    </PublicLayout>
  )
}
