import { useEffect } from 'react'
import { Link } from '@tanstack/react-router'
import { ArrowUpRight, Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getPublicPageSeoEntry } from '@/lib/public-page-seo'
import { PublicLayout } from '@/components/layout'
import { SiteSeo } from '@/components/seo'
import '@/features/docs/reading.css'

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
  const { t } = useTranslation()
  useEffect(() => {
    const openLinkedAnswer = () => {
      const answer = document.getElementById(window.location.hash.slice(1))
      if (answer instanceof HTMLDetailsElement) answer.open = true
    }
    openLinkedAnswer()
    window.addEventListener('hashchange', openLinkedAnswer)
    return () => window.removeEventListener('hashchange', openLinkedAnswer)
  }, [])
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
            name: t(item.q),
            acceptedAnswer: { '@type': 'Answer', text: t(item.a) },
          })),
        }}
      />
      <main
        id='content'
        tabIndex={-1}
        className='market-reading px-5 pt-8 pb-24 sm:px-8 md:pt-16'
      >
        <div className='market-faq'>
          <h1>{t(faqSeo.h1)}</h1>
          <div className='market-faq-list'>
            {faqs.map((item, index) => (
              <details
                id={`faq-${index + 1}`}
                key={item.q}
                className='market-faq-item'
              >
                <summary>
                  {t(item.q)}
                  <Plus aria-hidden='true' />
                </summary>
                <p>{t(item.a)}</p>
              </details>
            ))}
          </div>
          <nav className='market-reading-links'>
            <Link to='/guide'>
              {t('使用说明')}
              <ArrowUpRight size={20} aria-hidden='true' />
            </Link>
            <Link to='/about'>
              {t('关于 Code Go')}
              <ArrowUpRight size={20} aria-hidden='true' />
            </Link>
          </nav>
        </div>
      </main>
    </PublicLayout>
  )
}
