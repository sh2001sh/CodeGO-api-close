import type { ReactNode } from 'react'
import { SectionPageLayout } from '@/components/layout'
import { SiteSeo } from '@/components/seo'

interface WalletWorkspaceShellProps {
  title: string
  description?: string
  canonicalPath?: string
  main: ReactNode
  sidebar?: ReactNode
  framedMain?: boolean
}

export function WalletWorkspaceShell(props: WalletWorkspaceShellProps) {
  return (
    <SectionPageLayout>
      <SiteSeo
        title={props.title}
        description={props.description || props.title}
        canonicalPath={props.canonicalPath}
        robots='noindex,follow'
      />
      <SectionPageLayout.Title>{props.title}</SectionPageLayout.Title>
      {props.description ? (
        <SectionPageLayout.Description>
          {props.description}
        </SectionPageLayout.Description>
      ) : null}
      <SectionPageLayout.Content>
        <div
          className={
            props.sidebar
              ? 'mx-auto grid w-full max-w-[1600px] items-start gap-5 min-[1200px]:grid-cols-[minmax(0,1fr)_288px] 2xl:grid-cols-[minmax(0,1fr)_320px]'
              : 'mx-auto w-full max-w-[1360px]'
          }
        >
          {props.framedMain === false ? (
            <div className='min-w-0'>{props.main}</div>
          ) : (
            <div className='app-page-shell min-w-0 p-4 sm:p-5'>
              {props.main}
            </div>
          )}
          {props.sidebar ? (
            <aside className='min-[1200px]:sticky min-[1200px]:top-5'>
              {props.sidebar}
            </aside>
          ) : null}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
