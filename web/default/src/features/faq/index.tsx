import { Link } from '@tanstack/react-router'
import { ChevronRight, Code2, MessageSquareQuote, Search } from 'lucide-react'
import { getPublicPageSeoEntry } from '@/lib/public-page-seo'
import { PublicLayout } from '@/components/layout'
import { SiteSeo } from '@/components/seo'

const faqs = [
  {
    q: 'Code Go 是什么？',
    a: 'Code Go 是一个围绕 AI 编程工作流构建的平台，帮助你把使用过程持续积累下来。',
  },
  {
    q: 'Code Go 的核心理念是什么？',
    a: '让 AI 编程的每一步，都算数。',
  },
  {
    q: 'Code Go 适合哪些人？',
    a: '适合长期使用 Codex、Claude Code 和多模型工作流的开发者与团队。',
  },
  {
    q: 'Code Go 支持哪些使用场景？',
    a: '包括 API 调用、套餐订阅、额度管理、脚本下载、成就记录和持续使用记录。',
  },
  {
    q: '为什么要强调长期积累感？',
    a: '因为 AI 编程不只是一次性完成任务，更重要的是持续使用和持续进步。',
  },
  {
    q: 'Code Go 和普通 AI API 平台有什么区别？',
    a: '它除了接入和计费，也会把使用过程、记录和进度一起保留下来。',
  },
]

const faqSeo = getPublicPageSeoEntry('/faq')

export function FAQPage() {
  return (
    <PublicLayout showMainContainer={false}>
      <SiteSeo
        title={faqSeo.title}
        description={faqSeo.description}
        keywords={faqSeo.keywords}
        canonicalPath={faqSeo.path}
        jsonLd={{
          '@context': 'https://schema.org',
          '@type': 'FAQPage',
          mainEntity: faqs.map((item) => ({
            '@type': 'Question',
            name: item.q,
            acceptedAnswer: {
              '@type': 'Answer',
              text: item.a,
            },
          })),
        }}
      />
      <main className='px-6 pt-28 pb-16 md:px-10 md:pt-32'>
        <div className='mx-auto max-w-5xl'>
          <div className='max-w-3xl space-y-4'>
            <div className='border-border bg-muted text-muted-foreground inline-flex items-center gap-2 rounded-full border px-3 py-1 text-xs font-semibold'>
              <Search className='size-3.5' />
              {faqSeo.eyebrow}
            </div>
            <h1 className='text-foreground text-4xl font-semibold tracking-tight'>
              {faqSeo.h1}
            </h1>
            <p className='text-muted-foreground text-base leading-8'>
              {faqSeo.intro}
            </p>
          </div>

          <div className='mt-10 grid gap-4'>
            {faqs.map((item) => (
              <section
                key={item.q}
                className='border-border bg-card text-card-foreground rounded-3xl border p-6'
              >
                <div className='text-foreground text-lg font-semibold'>
                  {item.q}
                </div>
                <p className='text-muted-foreground mt-3 text-sm leading-7'>
                  {item.a}
                </p>
              </section>
            ))}
          </div>

          <div className='mt-10 grid gap-4 md:grid-cols-2'>
            <Link
              to='/guide'
              className='border-border bg-card hover:bg-muted/40 rounded-3xl border p-6 transition-colors'
            >
              <div className='flex items-center gap-2 text-sm font-semibold'>
                <Code2 className='text-muted-foreground size-4' />
                使用说明
              </div>
              <p className='text-muted-foreground mt-3 text-sm leading-7'>
                看实际使用流程、脚本下载和平台入口。
              </p>
              <div className='mt-4 inline-flex items-center gap-1 text-sm font-medium'>
                查看使用说明
                <ChevronRight className='size-4' />
              </div>
            </Link>
            <Link
              to='/about'
              className='border-border bg-card hover:bg-muted/40 rounded-3xl border p-6 transition-colors'
            >
              <div className='flex items-center gap-2 text-sm font-semibold'>
                <MessageSquareQuote className='text-muted-foreground size-4' />
                关于 Code Go
              </div>
              <p className='text-muted-foreground mt-3 text-sm leading-7'>
                看品牌概念、核心卖点和公开表达口径。
              </p>
              <div className='mt-4 inline-flex items-center gap-1 text-sm font-medium'>
                了解 Code Go
                <ChevronRight className='size-4' />
              </div>
            </Link>
          </div>
        </div>
      </main>
    </PublicLayout>
  )
}
