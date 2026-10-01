import type { ReactNode } from 'react'
import { useTranslation } from '../lib/i18n'

export interface Column<T> {
  label: string
  render: (row: T) => ReactNode
  numeric?: boolean
}

export function DataTable<T>(props: {
  rows: T[]
  columns: Column<T>[]
  rowKey: (row: T) => string | number | bigint
  empty?: string
}) {
  const { t } = useTranslation()
  return (
    <div className="table-scroll">
      <table>
        <thead>
          <tr>
            {props.columns.map((column) => (
              <th key={column.label} className={column.numeric ? 'numeric' : ''} scope="col">
                {t(column.label)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {props.rows.map((row) => (
            <tr key={props.rowKey(row)}>
              {props.columns.map((column) => (
                <td key={column.label} className={column.numeric ? 'numeric' : ''}>
                  {column.render(row)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      {props.rows.length === 0 && <div className="empty-state">{t(props.empty ?? '暂无记录')}</div>}
    </div>
  )
}
