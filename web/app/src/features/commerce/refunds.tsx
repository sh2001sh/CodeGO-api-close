import { useTranslation } from '../../lib/i18n'
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import type { Schema } from '../../lib/types'
import { resourceOptions } from '../../lib/queries'
import { credits, date } from '../../lib/format'
import { paymentAmount } from '../../lib/commerce'
import { DataTable } from '../../components/data-table'
import { Button, ErrorMessage, Field, Loading, Status } from '../../components/ui'

export const refundsOptions = () =>
  resourceOptions('refunds', (signal) =>
    api.GET('/api/wallet/refunds/eligible', { signal }).then(unwrap),
  )

function refundState(state: string) {
  return state === 'success'
    ? '退款完成'
    : state === 'processing'
      ? '退款处理中'
      : state === 'failed'
        ? '退款失败'
        : state || '尚未退款'
}

export function Refunds() {
  const { t } = useTranslation()
  const list = useQuery(refundsOptions())
  const client = useQueryClient()
  const [selected, setSelected] = useState<Schema['RefundableOrder'] | null>(null)
  const [result, setResult] = useState<Schema['UserRefundResult'] | null>(null)
  const refresh = () => {
    void client.invalidateQueries({ queryKey: ['refunds'] })
    void client.invalidateQueries({ queryKey: ['wallet'] })
    void client.invalidateQueries({ queryKey: ['orders'] })
    void client.invalidateQueries({ queryKey: ['subscriptions'] })
  }
  const create = useMutation({
    mutationFn: (body: Schema['UserRefundRequest']) =>
      api.POST('/api/wallet/refunds', { body }).then(unwrap),
    onSuccess: (value) => {
      setResult(value)
      setSelected(null)
      refresh()
    },
    onError: refresh,
  })
  const sync = useMutation({
    mutationFn: (refundNo: string) =>
      api
        .POST('/api/wallet/refunds/{refund_no}/sync', { params: { path: { refund_no: refundNo } } })
        .then(unwrap),
    onSuccess: (value) => {
      setResult(value)
      refresh()
    },
  })
  return (
    <section className="section">
      <div className="page-header">
        <h2>{t('未使用额度退款')}</h2>
        <Button variant="quiet" disabled={list.isFetching} onClick={() => void list.refetch()}>
          {t('刷新')}
        </Button>
      </div>
      <p className="muted">
        {t(
          '目前支持人民币易支付订单，按未使用额度计算退款并扣除 2% 手续费。显示金额为扣除手续费后的预计到账金额，实际结果以支付平台确认为准。',
        )}
      </p>
      <ErrorMessage error={list.error ?? create.error ?? sync.error} />
      {list.isPending && <Loading />}
      <DataTable
        rows={list.data?.items ?? []}
        rowKey={(row) => row.trade_no}
        empty="暂无支持退款的订单。已支付的易支付订单会显示在这里。"
        columns={[
          { label: '订单号', render: (row) => row.trade_no },
          {
            label: '类型',
            render: (row) => (row.order_type === 'subscription' ? t('订阅') : t('充值')),
          },
          { label: '未使用额度', render: (row) => credits(row.remaining_quota), numeric: true },
          {
            label: '预计到账',
            render: (row) =>
              row.refundable || row.refund_status
                ? paymentAmount(row.refund_amount_minor, 'cny')
                : '—',
            numeric: true,
          },
          { label: '状态', render: (row) => <Status value={refundState(row.refund_status)} /> },
          { label: '支付时间', render: (row) => date(Number(row.created_at) * 1000) },
          {
            label: '操作',
            render: (row) =>
              row.refundable ? (
                <Button
                  variant="quiet"
                  disabled={create.isPending || !!selected}
                  onClick={() => {
                    create.reset()
                    setSelected(row)
                  }}
                >
                  {t('申请退款')}
                </Button>
              ) : (
                row.unavailable_reason || t('无法退款')
              ),
          },
        ]}
      />
      {selected && (
        <div className="form-panel">
          <p className="full-width">
            {t('确认退回订单')} {selected.trade_no} {t('的未使用额度？预计到账')}{' '}
            {paymentAmount(selected.refund_amount_minor, 'cny')}
            {t('。处理中会暂扣相关额度。')}
          </p>
          <Button
            variant="danger"
            disabled={create.isPending}
            onClick={() =>
              create.mutate({ order_type: selected.order_type, trade_no: selected.trade_no })
            }
          >
            {create.isPending
              ? t('提交中…')
              : create.isError
                ? t('重试该订单退款')
                : t('确认申请退款')}
          </Button>
          <Button variant="quiet" disabled={create.isPending} onClick={() => setSelected(null)}>
            {t('取消')}
          </Button>
        </div>
      )}
      {result && (
        <div className="section" role="status">
          <p>
            {t(refundState(result.status))} · {paymentAmount(result.refund_amount_minor, 'cny')}
          </p>
          <p>
            {t('退款编号：')}
            <code>{result.refund_no}</code>
          </p>
          {result.message && <p>{result.message}</p>}
          {result.status === 'processing' && (
            <Button
              variant="quiet"
              disabled={sync.isPending}
              onClick={() => sync.mutate(result.refund_no)}
            >
              {sync.isPending ? t('查询中…') : t('查询支付平台结果')}
            </Button>
          )}
        </div>
      )}
      <form
        className="form-panel"
        onSubmit={(event) => {
          event.preventDefault()
          sync.mutate(String(new FormData(event.currentTarget).get('refund-number') ?? '').trim())
        }}
      >
        <Field name="refund-number" label="查询已提交退款编号" required maxLength={128} />
        <Button type="submit" variant="quiet" disabled={sync.isPending}>
          {sync.isPending ? t('查询中…') : t('查询退款')}
        </Button>
      </form>
    </section>
  )
}
