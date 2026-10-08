import type { ReactNode } from 'react'
import { useTranslation } from '../../lib/i18n'

/** Page title row. `description` explains the page purpose in one sentence. */
export function PageHeader(props: {
  title: string
  description?: ReactNode
  action?: ReactNode
  eyebrow?: ReactNode
}) {
  const { t } = useTranslation()
  return (
    <header className="page-header">
      <div className="page-header-text">
        {props.eyebrow && <div className="breadcrumbs">{props.eyebrow}</div>}
        <h1>{t(props.title)}</h1>
        {props.description && (
          <p>{typeof props.description === 'string' ? t(props.description) : props.description}</p>
        )}
      </div>
      <div className="page-header-actions">{props.action}</div>
    </header>
  )
}

export function SectionHeader(props: { title: string; description?: string; action?: ReactNode }) {
  const { t } = useTranslation()
  return (
    <div className="section-header">
      <div>
        <h2>{t(props.title)}</h2>
        {props.description && <p>{t(props.description)}</p>}
      </div>
      {props.action}
    </div>
  )
}

/** Bordered surface for a group of related content. */
export function Panel(props: {
  title?: string
  description?: string
  action?: ReactNode
  children: ReactNode
  className?: string
  as?: 'section' | 'div' | 'article'
}) {
  const { t } = useTranslation()
  const Tag = props.as ?? 'section'
  return (
    <Tag className={`panel ${props.className ?? ''}`.trim()}>
      {(props.title || props.action) && (
        <div className="panel-header">
          <div>
            {props.title && <h2>{t(props.title)}</h2>}
            {props.description && <p>{t(props.description)}</p>}
          </div>
          {props.action}
        </div>
      )}
      {props.children}
    </Tag>
  )
}

export function Stat(props: { label: string; value: ReactNode; hint?: ReactNode }) {
  const { t } = useTranslation()
  return (
    <div className="stat">
      <dt className="stat-label">{t(props.label)}</dt>
      <dd className="stat-value">{props.value}</dd>
      {props.hint && <dd className="stat-hint">{props.hint}</dd>}
    </div>
  )
}

export function StatGrid(props: { children: ReactNode }) {
  return <dl className="metrics">{props.children}</dl>
}
