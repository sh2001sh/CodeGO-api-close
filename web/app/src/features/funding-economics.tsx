import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { credits, shanghaiDay } from '../lib/format'
import { DataTable } from '../components/data-table'
import { Button, ErrorMessage, Loading } from '../components/ui'

export function FundingEconomics() {
  const [open, setOpen] = useState(false)
  const [selectedDay, setSelectedDay] = useState(shanghaiDay)
  const [day, setDay] = useState(selectedDay)
  const report = useQuery({
    ...resourceOptions(
      'funding-economics',
      (signal) =>
        api
          .GET('/api/billing/funding-economics', { signal, params: { query: { day } } })
          .then(unwrap),
      [day],
    ),
    enabled: open,
    retry: false,
  })
  return (
    <section className="section funding-report" aria-label="资金经营日报">
      <details onToggle={(event) => setOpen(event.currentTarget.open)}>
        <summary>资金经营日报</summary>
        {open && (
          <>
            <p>按上海时间统计已结算请求。未归属成本单列，不计入已确认收入、成本和利润。</p>
            <form
              className="filters"
              onSubmit={(event) => {
                event.preventDefault()
                if (day === selectedDay) void report.refetch()
                else setDay(selectedDay)
              }}
            >
              <label className="field" htmlFor="funding-day">
                <span>统计日期（上海）</span>
                <input
                  id="funding-day"
                  type="date"
                  required
                  value={selectedDay}
                  onChange={(event) => setSelectedDay(event.target.value)}
                />
              </label>
              <Button type="submit" disabled={report.isFetching}>
                查询日报
              </Button>
            </form>
            <ErrorMessage error={report.error} />
            {report.isFetching && <Loading />}
            {report.data && (
              <>
                <p>报告日期：{report.data.date}</p>
                <dl className="balance-ledger">
                  <div>
                    <dt>已确认收入</dt>
                    <dd>{credits(report.data.recognized_revenue_micro)}</dd>
                  </div>
                  <div>
                    <dt>已确认成本</dt>
                    <dd>{credits(report.data.recognized_cost_micro)}</dd>
                  </div>
                  <div>
                    <dt>已确认利润</dt>
                    <dd>{credits(report.data.recognized_profit_micro)}</dd>
                  </div>
                  <div>
                    <dt>未归属成本</dt>
                    <dd>{credits(report.data.unattributed_cost_micro)}</dd>
                  </div>
                </dl>
                <h3>资金来源</h3>
                <DataTable
                  rows={report.data.sources ?? []}
                  rowKey={(row) => row.source}
                  empty="该日期暂无已结算请求"
                  columns={[
                    { label: '来源', render: (row) => row.source },
                    {
                      label: '扣款金额',
                      render: (row) => credits(row.amount_micro),
                      numeric: true,
                    },
                    { label: '收入', render: (row) => credits(row.revenue_micro), numeric: true },
                    { label: '成本', render: (row) => credits(row.cost_micro), numeric: true },
                    { label: '利润', render: (row) => credits(row.profit_micro), numeric: true },
                  ]}
                />
              </>
            )}
          </>
        )}
      </details>
    </section>
  )
}
