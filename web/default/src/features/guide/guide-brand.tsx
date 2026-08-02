import { Link } from '@tanstack/react-router'
import { ArrowRight, Code2, Terminal, Wrench } from 'lucide-react'
import { PublicLayout } from '@/components/layout'
import { SiteSeo } from '@/components/seo'
import { Button } from '@/components/ui/button'
import { getPublicPageSeoEntry } from '@/lib/public-page-seo'

const sections = [
  {
    title: 'Codex 路线',
    text: '适合想把 AI Coding 变成可持续习惯的人。重点不是炫技，而是流程、记录和持续进步。',
  },
  {
    title: 'Claude Code 路线',
    text: '适合偏终端、偏任务流、偏长时工作的开发者。把每次完成都当成一次新的积累。',
  },
  {
    title: '推广路线',
    text: '用一句话打动人：让 AI Coding 的每一步，都算数。',
  },
]

const guideSeo = getPublicPageSeoEntry('/guide')

export function GuideBrand() {
  return (
    <PublicLayout showMainContainer={false}>
      <SiteSeo
        title={guideSeo.title}
        description={guideSeo.description}
        keywords={guideSeo.keywords}
        canonicalPath={guideSeo.path}
      />
      <main className='px-6 pb-16 pt-28 md:px-10 md:pt-32'>
        <div className='mx-auto max-w-6xl space-y-10'>
          <div className='max-w-3xl space-y-4'>
            <div className='inline-flex items-center gap-2 rounded-full border px-3 py-1 text-xs font-semibold'>
              <Terminal className='size-3.5' />
              推广指南
            </div>
            <h1 className='text-foreground text-4xl font-semibold tracking-tight'>
              把 AI Coding 讲成一件会持续累积的事
            </h1>
            <p className='text-base leading-8 text-muted-foreground'>
              这页的目标很简单：让用户一眼知道 Code Go 在做什么，以及为什么它和 Codex / Claude Code 的语境是连着的。
            </p>
          </div>

          <div className='grid gap-4 md:grid-cols-3'>
            {sections.map((item) => (
              <div key={item.title} className='app-subtle-panel p-6'>
                <div className='flex items-center gap-2 text-sm font-semibold'>
                  <Code2 className='text-muted-foreground size-4' />
                  {item.title}
                </div>
                <p className='mt-3 text-sm leading-7 text-muted-foreground'>
                  {item.text}
                </p>
              </div>
            ))}
          </div>

          <div className='grid gap-4 lg:grid-cols-2'>
            <div className='app-subtle-panel p-6'>
              <div className='flex items-center gap-2 text-sm font-semibold'>
                <Wrench className='text-muted-foreground size-4' />
                可直接复用的口径
              </div>
              <ul className='mt-4 space-y-3 text-sm leading-7 text-muted-foreground'>
                <li>Code Go，让 AI Coding 的每一步都算数。</li>
                <li>让 Codex / Claude Code 不只是工具，而是长期积累的过程。</li>
                <li>把调用、成就、记录和进度放到同一条线里。</li>
              </ul>
            </div>
            <div className='app-subtle-panel p-6'>
              <div className='flex items-center gap-2 text-sm font-semibold'>
                <ArrowRight className='text-muted-foreground size-4' />
                推广动作
              </div>
              <p className='mt-4 text-sm leading-7 text-muted-foreground'>
                在首页、社媒简介、产品介绍、FAQ 和教程标题中，都保持同一句核心表达，先统一认知，再放大流量。
              </p>
              <Button className='mt-5' render={<Link to='/faq' />}>
                看 FAQ
              </Button>
            </div>
          </div>
        </div>
      </main>
    </PublicLayout>
  )
}
