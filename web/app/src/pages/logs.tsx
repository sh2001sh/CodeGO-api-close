import { useState } from 'react'
import { useSuspenseQuery } from '@tanstack/react-query'
import { resourceOptions } from '../lib/queries'
import { api, unwrap } from '../lib/api'
import { credits, date } from '../lib/format'
import { useTranslation } from '../lib/i18n'
import { DataTable } from '../components/data-table'
import { Button, PageHeader, Status } from '../components/ui'

export default function LogsPage() {
  const { t } = useTranslation()
  const [model, setModel] = useState('')
  const [cursor, setCursor] = useState('')
  const { data } = useSuspenseQuery(
    resourceOptions(
      'logs',
      (signal) =>
        api
          .GET('/api/log/self', {
            signal,
            params: {
              query: { page_size: 50, model: model || undefined, cursor: cursor || undefined },
            },
          })
          .then((result) => unwrap(result)),
      [model, cursor],
    ),
  )
  return (
    <>
      <PageHeader
        title="使用日志"
        action={
          <a
            className="button button-quiet"
            href={`/api/log/self/export?${new URLSearchParams({ ...(model ? { model } : {}), ...(cursor ? { cursor } : {}) })}`}
          >
            {t('导出本页')}
          </a>
        }
      />
      <form
        className="filters"
        onSubmit={(event) => {
          event.preventDefault()
          setModel(String(new FormData(event.currentTarget).get('model')))
          setCursor('')
        }}
      >
        <label className="field" htmlFor="model-filter">
          <span>{t('模型')}</span>
          <input id="model-filter" name="model" placeholder="gpt-4o" />
        </label>
        <Button type="submit">{t('筛选')}</Button>
      </form>
      <DataTable
        rows={data.items ?? []}
        rowKey={(row) => `${row.request_id}-${row.created_at}`}
        columns={[
          { label: '时间', render: (row) => date(row.created_at) },
          { label: '模型', render: (row) => row.model },
          {
            label: '输入 token',
            render: (row) => row.prompt_tokens.toLocaleString(),
            numeric: true,
          },
          {
            label: '输出 token',
            render: (row) => row.completion_tokens.toLocaleString(),
            numeric: true,
          },
          { label: '扣费', render: (row) => credits(row.amount), numeric: true },
          { label: '终态', render: (row) => <Status value={row.terminal} /> },
        ]}
      />
      <div className="filters section">
        <Button variant="quiet" disabled={!cursor} onClick={() => setCursor('')}>
          {t('返回最新')}
        </Button>
        <Button
          variant="quiet"
          disabled={!data.next_cursor}
          onClick={() => setCursor(data.next_cursor ?? '')}
        >
          {t('下一页')}
        </Button>
      </div>
    </>
  )
}
