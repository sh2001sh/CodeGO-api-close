import type { ReactNode } from 'react'
import { SectionPageLayout } from '@/components/layout'
import { SiteSeo } from '@/components/seo'
import '../commerce.css'

interface WalletWorkspaceShellProps {
  title: string
  description?: string
  canonicalPath?: string
  main: ReactNode
  sidebar?: ReactNode
  framedMain?: boolean
  kicker?: string
}

export function WalletWorkspaceShell(props: WalletWorkspaceShellProps) {
  return (
    <SectionPageLayout kicker=''>
      <SiteSeo
        title={props.title}
        description={props.description || props.title}
        canonicalPath={props.canonicalPath}
        robots='noindex,follow'
      />
      <SectionPageLayout.Title>{props.title}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div
          className={
            props.sidebar
              ? 'commerce-workspace commerce-workspace-split'
              : 'commerce-workspace'
          }
        >
          {props.framedMain === false ? (
            <div className='min-w-0'>{props.main}</div>
          ) : (
            <div className='commerce-surface min-w-0'>{props.main}</div>
          )}
          {props.sidebar ? (
            <aside className='commerce-sidebar'>{props.sidebar}</aside>
          ) : null}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
