import type { ReactNode } from 'react'
import { useTranslation } from '../lib/i18n'
import { EmptyState } from './primitives/feedback'

export interface Column<T> {
  label: string
  render: (row: T) => ReactNode
  numeric?: boolean
  /** Hide on narrow screens to keep the key columns readable. */
  hideOnMobile?: boolean
}

export function DataTable<T>(props: {
  rows: T[]
  columns: Column<T>[]
  rowKey: (row: T) => string | number | bigint
  empty?: string
  emptyDescription?: string
  emptyAction?: ReactNode
  caption?: string
  onRowClick?: (row: T) => void
}) {
  const { t } = useTranslation()
  const cellClass = (column: Column<T>) =>
    [column.numeric ? 'numeric' : '', column.hideOnMobile ? 'hide-mobile' : '']
      .filter(Boolean)
      .join(' ') || undefined
  return (
    <div
      className="table-scroll"
      tabIndex={0}
      role="region"
      aria-label={t(props.caption ?? '数据表格')}
    >
      <table>
        {props.caption && <caption className="sr-only">{t(props.caption)}</caption>}
        <thead>
          <tr>
            {props.columns.map((column) => (
              <th key={column.label} className={cellClass(column)} scope="col">
                {t(column.label)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {props.rows.map((row) => (
            <tr
              key={String(props.rowKey(row))}
              data-clickable={props.onRowClick ? true : undefined}
              tabIndex={props.onRowClick ? 0 : undefined}
              onKeyDown={
                props.onRowClick
                  ? (event) => {
                      if (
                        event.target === event.currentTarget &&
                        (event.key === 'Enter' || event.key === ' ')
                      ) {
                        event.preventDefault()
                        props.onRowClick?.(row)
                      }
                    }
                  : undefined
              }
              onClick={props.onRowClick ? () => props.onRowClick?.(row) : undefined}
            >
              {props.columns.map((column) => (
                <td key={column.label} className={cellClass(column)}>
                  {column.render(row)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      {props.rows.length === 0 && (
        <EmptyState
          title={props.empty ?? '暂无记录'}
          description={props.emptyDescription}
          action={props.emptyAction}
        />
      )}
    </div>
  )
}
