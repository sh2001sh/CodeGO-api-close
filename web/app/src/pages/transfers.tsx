import { useState } from 'react'
import { useQuery, useSuspenseQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { credits, date } from '../lib/format'
import { DataTable } from '../components/data-table'
import { Button, ErrorMessage, Loading, PageHeader, Status } from '../components/ui'
import { TransferForm } from '../features/commerce/transfer-form'
import { PaymentPassword } from '../features/commerce/payment-password'

export const transfersOptions = (page = 1) =>
  resourceOptions(
    'transfers',
    (signal) =>
      api
        .GET('/api/wallet/transfers', { signal, params: { query: { p: page, page_size: 10 } } })
        .then((result) => unwrap(result)),
    [page],
  )

export default function TransfersPage() {
  const first = useSuspenseQuery(transfersOptions()).data
  const [page, setPage] = useState(1)
  const history = useQuery(transfersOptions(page))
  const overview = history.data ?? first
  return (
    <>
      <PageHeader
        title="钱包转账"
        action={
          <Button
            variant="quiet"
            disabled={history.isFetching}
            onClick={() => void history.refetch()}
          >
            刷新
          </Button>
        }
      />
      <dl className="balance-ledger">
        <div>
          <dt>钱包余额</dt>
          <dd>{credits(overview.balance)}</dd>
        </div>
      </dl>
      <TransferForm overview={overview} />
      <PaymentPassword security={overview.security} />
      <section className="section">
        <h2>转账记录</h2>
        <ErrorMessage error={history.error} />
        {history.isFetching && <Loading />}
        <DataTable
          rows={history.data?.history.items ?? []}
          rowKey={(row) => row.id}
          empty="暂无转账记录。完成一笔转账后会显示在这里。"
          columns={[
            { label: '方向', render: (row) => (row.direction === 'outgoing' ? '转出' : '转入') },
            {
              label: '对方',
              render: (row) =>
                `${row.counterparty_display_name_masked} (${row.counterparty_external_id})`,
            },
            { label: '金额', render: (row) => credits(row.amount_micro), numeric: true },
            { label: '手续费', render: (row) => credits(row.fee_micro), numeric: true },
            { label: '余额', render: (row) => credits(row.balance_after), numeric: true },
            { label: '状态', render: (row) => <Status value={row.status} /> },
            { label: '时间', render: (row) => date(Number(row.created_at) * 1000) },
          ]}
        />
        <nav className="pagination" aria-label="转账记录分页">
          <Button
            variant="quiet"
            disabled={page <= 1 || history.isFetching}
            onClick={() => setPage(page - 1)}
          >
            上一页
          </Button>
          <span>第 {page} 页</span>
          <Button
            variant="quiet"
            disabled={
              !history.data ||
              BigInt(page * 10) >= BigInt(history.data.history.total) ||
              history.isFetching
            }
            onClick={() => setPage(page + 1)}
          >
            下一页
          </Button>
        </nav>
      </section>
    </>
  )
}
