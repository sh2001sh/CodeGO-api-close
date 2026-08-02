import { Link } from '@tanstack/react-router'
import {
  ArrowRight,
  BookOpenText,
  Compass,
  FolderKanban,
  ShieldAlert,
} from 'lucide-react'
import { motion, useReducedMotion, type Variants } from 'motion/react'
import { MOTION_TRANSITION } from '@/lib/motion'
import { getPublicPageSeoEntry } from '@/lib/public-page-seo'
import { Button } from '@/components/ui/button'
import { PublicLayout } from '@/components/layout'
import { SiteSeo } from '@/components/seo'
import { guideSections } from './content'

const guideSeo = getPublicPageSeoEntry('/guide')

const SECTION_REVEAL: Variants = {
  hidden: { opacity: 0, y: 24 },
  visible: { opacity: 1, y: 0, transition: MOTION_TRANSITION.slow },
}

function GuideDiagram(props: { title: string; steps: string[] }) {
  return (
    <div className='overview-soft-card p-4'>
      <div className='text-foreground text-sm font-medium'>{props.title}</div>
      <div className='mt-3 grid gap-3 md:grid-cols-2 xl:grid-cols-4'>
        {props.steps.map((step, index) => (
          <div
            key={step}
            className='app-subtle-panel relative p-3'
          >
            <div className='text-muted-foreground text-[11px] font-semibold tracking-[0.22em] uppercase'>
              第 {index + 1} 步
            </div>
            <div className='mt-2 text-sm leading-6 font-medium'>{step}</div>
            {index < props.steps.length - 1 ? (
              <div className='pointer-events-none absolute top-1/2 -right-2 hidden -translate-y-1/2 xl:block'>
                <div className='bg-background flex size-8 items-center justify-center rounded-full border shadow-xs'>
                  <ArrowRight className='text-muted-foreground size-4' />
                </div>
              </div>
            ) : null}
          </div>
        ))}
      </div>
    </div>
  )
}

function GuideSectionBlock(props: { section: (typeof guideSections)[number] }) {
  const { section } = props
  const shouldReduceMotion = Boolean(useReducedMotion())

  return (
    <motion.section
      id={section.id}
      className='border-border/50 scroll-mt-24 border-t py-10 first:border-t-0 first:pt-0'
      variants={SECTION_REVEAL}
      initial={shouldReduceMotion ? false : 'hidden'}
      whileInView='visible'
      viewport={{ once: true, margin: '-60px' }}
    >
      <div className='grid gap-8 xl:grid-cols-[minmax(0,0.9fr)_minmax(360px,1.1fr)]'>
        <div className='space-y-5'>
          <div className='space-y-3'>
            <div className='text-muted-foreground text-xs font-semibold tracking-[0.24em] uppercase'>
              {section.eyebrow}
            </div>
            <h2 className='text-foreground text-2xl font-semibold tracking-tight'>
              {section.title}
            </h2>
            <p className='text-muted-foreground text-sm leading-7'>
              {section.summary}
            </p>
          </div>

          {section.steps && section.steps.length > 0 ? (
            <ol className='space-y-3'>
              {section.steps.map((step, index) => (
                <li
                  key={`${section.id}-step-${index}`}
                  className='text-foreground/80 flex gap-3 text-sm leading-7'
                >
                  <span className='bg-foreground text-background mt-1 inline-flex size-6 shrink-0 items-center justify-center rounded-full text-xs font-semibold'>
                    {index + 1}
                  </span>
                  <span>{step}</span>
                </li>
              ))}
            </ol>
          ) : null}

          {section.diagram_title && section.diagram_steps?.length ? (
            <GuideDiagram
              title={section.diagram_title}
              steps={section.diagram_steps}
            />
          ) : null}

          {section.notes && section.notes.length > 0 ? (
            <div className='border-primary/20 bg-primary/6 rounded-xl border px-5 py-4'>
              <div className='text-primary mb-3 flex items-center gap-2 text-sm font-semibold'>
                <ShieldAlert className='size-4' />
                说明
              </div>
              <ul className='text-foreground/80 space-y-2 text-sm leading-7'>
                {section.notes.map((note, index) => (
                  <li key={`${section.id}-note-${index}`}>{note}</li>
                ))}
              </ul>
            </div>
          ) : null}
        </div>

        <div className='space-y-5'>
          {section.images.map((image) => (
            <figure key={image.src} className='space-y-3'>
              <div className='overview-soft-card overflow-hidden'>
                <img
                  src={image.src}
                  alt={image.alt}
                  className='h-auto w-full object-cover'
                  loading='lazy'
                />
              </div>
              <figcaption className='text-muted-foreground text-sm leading-6'>
                {image.caption}
              </figcaption>
            </figure>
          ))}
        </div>
      </div>
    </motion.section>
  )
}

export function Guide() {
  return (
    <PublicLayout showMainContainer={false}>
      <SiteSeo
        title={guideSeo.title}
        description={guideSeo.description}
        keywords={guideSeo.keywords}
        canonicalPath={guideSeo.path}
      />
      <main className='bg-background'>
        <section className='border-border/50 relative overflow-hidden border-b px-6 pt-28 pb-10 md:px-10 md:pt-32 md:pb-14'>
          <div className='pointer-events-none absolute inset-0 bg-[radial-gradient(ellipse_80%_50%_at_50%_-10%,color-mix(in_oklch,var(--primary)_10%,transparent),transparent)]' />
          <div className='mx-auto flex max-w-7xl flex-col gap-8 lg:flex-row lg:items-end lg:justify-between'>
            <div className='max-w-3xl space-y-4'>
              <div className='border-primary/20 bg-primary/8 text-primary inline-flex items-center gap-2 rounded-full border px-3 py-1 text-xs font-semibold'>
                <BookOpenText className='size-3.5' />
                {guideSeo.eyebrow}
              </div>
              <div className='space-y-3'>
                <h1 className='text-foreground text-4xl font-semibold tracking-tight md:text-5xl'>
                  {guideSeo.h1}
                </h1>
                <p className='text-muted-foreground max-w-2xl text-base leading-8 md:text-lg'>
                  {guideSeo.intro}
                </p>
              </div>
            </div>

            <div className='grid gap-3 sm:grid-cols-3 lg:min-w-[440px]'>
              <div className='overview-soft-card px-4 py-4'>
                <div className='text-muted-foreground text-xs font-medium tracking-wide uppercase'>
                  章节数量
                </div>
                <div className='mt-2 text-2xl font-semibold'>
                  {guideSections.length}
                </div>
              </div>
              <div className='overview-soft-card px-4 py-4'>
                <div className='text-muted-foreground text-xs font-medium tracking-wide uppercase'>
                  说明范围
                </div>
                <div className='mt-2 text-sm leading-6 font-medium'>
                  从首页到钱包
                </div>
              </div>
              <div className='overview-soft-card px-4 py-4'>
                <div className='text-muted-foreground text-xs font-medium tracking-wide uppercase'>
                  玩法文档
                </div>
                <div className='mt-2 text-sm leading-6 font-medium'>
                  宠物、盲盒、套餐
                </div>
              </div>
            </div>
          </div>
        </section>

        <section className='px-6 py-8 md:px-10'>
          <div className='mx-auto grid max-w-7xl gap-10 xl:grid-cols-[240px_minmax(0,1fr)]'>
            <aside className='xl:sticky xl:top-24 xl:self-start'>
              <div className='overview-glass-card space-y-4 rounded-2xl p-4'>
                <div className='flex items-center gap-2 text-sm font-semibold'>
                  <Compass className='size-4' />
                  导航目录
                </div>

                <nav aria-label='使用说明章节目录'>
                  <ul className='space-y-1'>
                    {guideSections.map((section) => (
                      <li key={section.id}>
                        <a
                          href={`#${section.id}`}
                          className='text-muted-foreground hover:bg-background/50 hover:text-foreground flex items-center gap-3 rounded-xl px-3 py-2 text-sm transition-colors'
                        >
                          <span className='text-muted-foreground/60 text-[10px] font-bold tracking-[0.2em] uppercase'>
                            {section.eyebrow}
                          </span>
                          <span>{section.title}</span>
                        </a>
                      </li>
                    ))}
                  </ul>
                </nav>

                <div className='border-border/60 border-t pt-4 text-sm leading-6'>
                  <div className='mb-2 flex items-center gap-2 font-medium'>
                    <FolderKanban className='size-4' />
                    你会看到什么
                  </div>
                  <p className='text-muted-foreground'>
                    这里把套餐、盲盒、宠物和钱包入口分别说明，方便你直接开始使用。
                  </p>
                </div>

                <div className='flex flex-col gap-2 pt-1'>
                  <Button className='w-full' render={<Link to='/sign-up' />}>
                    立即注册
                  </Button>
                  <Button
                    variant='outline'
                    className='w-full'
                    render={<Link to='/pricing' />}
                  >
                    查看模型广场
                  </Button>
                </div>
              </div>
            </aside>

            <div className='min-w-0 space-y-2'>
              {guideSections.map((section) => (
                <GuideSectionBlock key={section.id} section={section} />
              ))}
            </div>
          </div>
        </section>
      </main>
    </PublicLayout>
  )
}
