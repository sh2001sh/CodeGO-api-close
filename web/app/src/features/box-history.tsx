import { useState } from 'react'
import { useSuspenseQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { credits, date } from '../lib/format'
import { DataTable } from '../components/data-table'
import { Button } from '../components/ui'

export const boxHistoryOptions = (before = '') =>
  resourceOptions(
    'box-history',
    (signal) =>
      api
        .GET('/api/blind-box/history', {
          signal,
          params: { query: { before: before || undefined } },
        })
        .then(unwrap),
    [before],
  )

export function BoxHistory() {
  const [before, setBefore] = useState('')
  const records = useSuspenseQuery(boxHistoryOptions(before)).data
  return (
    <section className="section">
      <h2>开启记录</h2>
      <DataTable
        rows={records ?? []}
        rowKey={(row) => String(row.id)}
        columns={[
          { label: '奖励', render: (row) => row.reward.title },
          {
            label: '额度',
            render: (row) =>
              row.reward.kind === 'credits' ? credits(row.reward.amount_micro) : '—',
          },
          { label: '开启时间', render: (row) => date(row.created_at) },
        ]}
      />
      <div className="filters section">
        <Button variant="quiet" disabled={!before} onClick={() => setBefore('')}>
          返回最新
        </Button>
        <Button
          variant="quiet"
          disabled={(records?.length ?? 0) < 30}
          onClick={() => setBefore(String(records?.at(-1)?.id ?? ''))}
        >
          下一页
        </Button>
      </div>
    </section>
  )
}
