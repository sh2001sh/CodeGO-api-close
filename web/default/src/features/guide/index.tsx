import { Link } from '@tanstack/react-router'
import i18n from '@/i18n/config'
import { ArrowRight, Compass } from 'lucide-react'
import { useTranslation, Trans } from 'react-i18next'
import { getPublicPageSeoEntry } from '@/lib/public-page-seo'
import { Button } from '@/components/ui/button'
import { PublicLayout } from '@/components/layout'
import { SiteSeo } from '@/components/seo'
import '@/features/docs/reading.css'
import { guideSections } from './content'

const guideSeo = getPublicPageSeoEntry('/guide')

function GuideDiagram(props: { title: string; steps: string[] }) {
  return (
    <div className='guide-diagram p-4'>
      <div className='text-foreground text-sm font-medium'>{props.title}</div>
      <div className='mt-3 grid gap-3 md:grid-cols-2 xl:grid-cols-4'>
        {props.steps.map((step, index) => (
          <div key={step} className='guide-diagram-step relative p-3'>
            <div className='text-muted-foreground text-xs font-medium'>
              <Trans i18nKey={'第'} /> {index + 1} <Trans i18nKey={'步'} />
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

  return (
    <section
      id={section.id}
      className='border-border/50 scroll-mt-24 border-t py-10 first:border-t-0 first:pt-0'
    >
      <div className='guide-section-content'>
        <div className='space-y-5'>
          <div className='space-y-3'>
            <h2 className='text-foreground text-2xl font-semibold tracking-tight'>
              {section.title}
            </h2>
            <p className='text-muted-foreground'>{section.summary}</p>
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

          {section.images.map((image) => (
            <figure className='guide-image' key={image.src}>
              <img src={image.src} alt={image.alt} loading='lazy' />
              {image.caption ? <figcaption>{image.caption}</figcaption> : null}
            </figure>
          ))}

          {section.diagram_title && section.diagram_steps?.length ? (
            <GuideDiagram
              title={section.diagram_title}
              steps={section.diagram_steps}
            />
          ) : null}
          {section.notes?.length ? (
            <aside className='guide-notes'>
              {section.notes.map((note) => (
                <p key={note}>{note}</p>
              ))}
            </aside>
          ) : null}
        </div>
      </div>
    </section>
  )
}

export function Guide() {
  const { t } = useTranslation()
  const localizedSections = guideSections.map((section) => ({
    ...section,
    eyebrow: t(section.eyebrow),
    title: t(section.title),
    summary: t(section.summary),
    steps: section.steps?.map((step) => t(step)),
    notes: section.notes?.map((note) => t(note)),
    diagram_title: section.diagram_title ? t(section.diagram_title) : undefined,
    diagram_steps: section.diagram_steps?.map((step) => t(step)),
    images: section.images.map((image) => ({
      ...image,
      alt: t(image.alt),
      caption: t(image.caption),
    })),
  }))
  return (
    <PublicLayout showMainContainer={false}>
      <SiteSeo
        title={guideSeo.title}
        description={guideSeo.description}
        keywords={guideSeo.keywords}
        canonicalPath={guideSeo.path}
      />
      <main id='content' tabIndex={-1} className='market-reading market-guide'>
        <section className='border-border/50 relative overflow-hidden border-b px-5 pt-8 pb-10 md:px-8 md:pt-16 md:pb-14'>
          <div className='mx-auto flex max-w-7xl flex-col gap-8 lg:flex-row lg:items-end lg:justify-between'>
            <div className='max-w-3xl space-y-4'>
              <div className='space-y-3'>
                <h1 className='text-foreground text-4xl font-semibold tracking-tight md:text-5xl'>
                  {t(guideSeo.h1)}
                </h1>
              </div>
            </div>

            <p className='text-muted-foreground text-sm'>
              <Trans i18nKey={'章节数量'} /> · {localizedSections.length}
            </p>
          </div>
        </section>

        <section className='px-6 py-8 md:px-10'>
          <div className='mx-auto grid max-w-7xl gap-10 xl:grid-cols-[240px_minmax(0,1fr)]'>
            <aside className='xl:sticky xl:top-24 xl:self-start'>
              <div className='guide-navigation space-y-4 p-5'>
                <div className='flex items-center gap-2 text-sm font-semibold'>
                  <Compass className='size-4' />
                  <Trans i18nKey={'导航目录'} />
                </div>

                <nav aria-label={i18n.t('使用说明章节目录')}>
                  <ul className='space-y-1'>
                    {localizedSections.map((section) => (
                      <li key={section.id}>
                        <a
                          href={`#${section.id}`}
                          className='text-muted-foreground hover:bg-background/50 hover:text-foreground flex items-center gap-3 rounded-xl px-3 py-2 text-sm transition-colors'
                        >
                          <span>{section.title}</span>
                        </a>
                      </li>
                    ))}
                  </ul>
                </nav>

                <div className='flex flex-col gap-2 pt-1'>
                  <Button className='w-full' render={<Link to='/sign-up' />}>
                    <Trans i18nKey={'立即注册'} />
                  </Button>
                  <Button
                    variant='outline'
                    className='w-full'
                    render={<Link to='/pricing' />}
                  >
                    <Trans i18nKey={'查看模型广场'} />
                  </Button>
                </div>
              </div>
            </aside>

            <div className='guide-sections min-w-0 space-y-2'>
              {localizedSections.map((section) => (
                <GuideSectionBlock key={section.id} section={section} />
              ))}
            </div>
          </div>
        </section>
      </main>
    </PublicLayout>
  )
}
