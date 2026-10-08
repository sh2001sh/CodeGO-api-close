import { useState } from 'react'
import { useQuery, useSuspenseQuery } from '@tanstack/react-query'
import { useLocation, useNavigate } from '@tanstack/react-router'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { credits, date } from '../lib/format'
import { useTranslation } from '../lib/i18n'
import { DataTable } from '../components/data-table'
import { Button, ErrorMessage, Loading, PageHeader, Stat, StatGrid, Tabs } from '../components/ui'
import { displayInt } from '../features/analytics/format'
import { OrderInvoices } from '../features/commerce/order-invoices'

const balanceOptions = () =>
  resourceOptions('billing-balance', (signal) =>
    api.GET('/api/billing/balance', { signal }).then((result) => unwrap(result)),
  )

/** Positive amounts show a leading "+" so direction never depends on color alone. */
function signedCredits(value: number | string | bigint) {
  const amount = BigInt(value)
  const rendered = credits(amount < 0n ? -amount : amount)
  return amount >= 0n ? `+${rendered}` : `-${rendered}`
}

function LedgerTab() {
  const { t } = useTranslation()
  const [before, setBefore] = useState<string[]>([])
  const list = useQuery(
    resourceOptions(
      'billing-entries',
      (signal) =>
        api
          .GET('/api/billing/entries', {
            signal,
            params: {
              query: { limit: 50, before: before.at(-1) ? Number(before.at(-1)) : undefined },
            },
          })
          .then((result) => unwrap(result)),
      [before.at(-1) ?? ''],
    ),
  )
  return (
    <>
      <ErrorMessage error={list.error} />
      {list.isFetching && <Loading />}
      <DataTable
        rows={list.data?.items ?? []}
        rowKey={(row) => row.id}
        empty="暂无账本记录"
        columns={[
          { label: '时间', render: (row) => date(row.created_at) },
          { label: '类型', render: (row) => row.kind },
          { label: '原因', render: (row) => row.reason },
          { label: '金额', render: (row) => signedCredits(row.amount_micro), numeric: true },
          { label: '账后余额', render: (row) => credits(row.balance_after_micro), numeric: true },
        ]}
      />
      <div className="filters section">
        <Button
          variant="quiet"
          disabled={before.length === 0}
          onClick={() => setBefore(before.slice(0, -1))}
        >
          {t('上一页')}
        </Button>
        <Button
          variant="quiet"
          disabled={list.data?.next_before === undefined}
          onClick={() => setBefore([...before, String(list.data?.next_before ?? '')])}
        >
          {t('下一页')}
        </Button>
      </div>
    </>
  )
}

function HistoryTab() {
  const { t } = useTranslation()
  const [before, setBefore] = useState<string[]>([])
  const list = useQuery(
    resourceOptions(
      'billing-history',
      (signal) =>
        api
          .GET('/api/billing/history', {
            signal,
            params: { query: { limit: 50, before: before.at(-1) } },
          })
          .then((result) => unwrap(result)),
      [before.at(-1) ?? ''],
    ),
  )
  return (
    <>
      <ErrorMessage error={list.error} />
      {list.isFetching && <Loading />}
      <DataTable
        rows={list.data?.items ?? []}
        rowKey={(row) => row.id}
        empty="暂无历史账本记录"
        columns={[
          { label: '时间', render: (row) => date(row.created_at) },
          { label: '来源账户', render: (row) => row.source_account_id, hideOnMobile: true },
          { label: '类型', render: (row) => row.kind },
          { label: '方向', render: (row) => row.direction },
          { label: '原因', render: (row) => row.reason },
          { label: '金额', render: (row) => signedCredits(row.amount_micro), numeric: true },
          {
            label: '账后余额',
            render: (row) =>
              row.balance_after_micro === null ? '—' : credits(row.balance_after_micro),
            numeric: true,
          },
        ]}
      />
      <div className="filters section">
        <Button
          variant="quiet"
          disabled={before.length === 0}
          onClick={() => setBefore(before.slice(0, -1))}
        >
          {t('上一页')}
        </Button>
        <Button
          variant="quiet"
          disabled={!list.data?.next_before}
          onClick={() => setBefore([...before, list.data?.next_before ?? ''])}
        >
          {t('下一页')}
        </Button>
      </div>
    </>
  )
}

export default function BillingHistoryPage() {
  const balance = useSuspenseQuery(balanceOptions()).data
  const hash = useLocation({ select: (location) => location.hash })
  const navigate = useNavigate()
  const tab = hash === 'invoices' || hash === 'history' ? hash : 'ledger'
  return (
    <>
      <PageHeader title="账单明细" />
      <StatGrid>
        <Stat label="当前余额" value={credits(balance.balance_micro_credits)} />
        <Stat label="账户编号" value={displayInt(balance.account_id)} />
        <Stat label="版本号" value={displayInt(balance.version)} />
      </StatGrid>
      <Tabs
        value={tab}
        onValueChange={(value) => void navigate({ hash: value, replace: true })}
        items={[
          { value: 'ledger', label: '当前账本', content: <LedgerTab /> },
          { value: 'history', label: '历史账本', content: <HistoryTab /> },
          { value: 'invoices', label: '发票', content: <OrderInvoices /> },
        ]}
      />
    </>
  )
}
