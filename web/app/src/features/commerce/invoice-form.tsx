import { useTranslation } from '../../lib/i18n'
import { useState } from 'react'
import type { Schema } from '../../lib/types'
import { Button, ErrorMessage, Field } from '../../components/ui'
import { DataTable } from '../../components/data-table'
import { errorFrom, invoicePayment } from './amounts'

export function InvoiceForm(props: {
  orders: Schema['InvoiceEligibleOrder'][]
  pending: boolean
  onSave: (body: Schema['CreateInvoiceRequestInput']) => void
}) {
  const { t } = useTranslation()
  const [selected, setSelected] = useState<string[]>([])
  const [kind, setKind] = useState('personal')
  const [error, setError] = useState<Error | null>(null)
  const available = props.orders.filter((order) => !order.requested)
  return (
    <form
      className="section"
      onSubmit={(event) => {
        event.preventDefault()
        setError(null)
        try {
          const orders = available.filter((order) => selected.includes(order.trade_no))
          if (orders.length === 0 || orders.length > 100)
            throw new Error('请选择 1–100 笔已支付订单')
          if (new Set(orders.map((order) => order.currency)).size > 1)
            throw new Error('不同币种的订单请分别申请')
          const form = new FormData(event.currentTarget)
          const text = (name: string) => String(form.get(name) ?? '').trim()
          const taxNumber = text('invoice-tax-number')
          if (kind === 'enterprise' && [...taxNumber].length < 8)
            throw new Error('企业税号至少八个字符')
          props.onSave({
            orders: orders.map((order) => ({
              source_type: order.source_type,
              trade_no: order.trade_no,
            })),
            source_type: '',
            trade_no: '',
            invoice_type: kind,
            title: text('invoice-title'),
            tax_number: taxNumber,
            email: text('invoice-email'),
            remark: text('invoice-remark'),
          })
        } catch (cause) {
          setError(errorFrom(cause))
        }
      }}
    >
      <h2>{t('申请发票')}</h2>
      <p className="muted">{t('选择同一币种的已支付订单，合并申请发票。')}</p>
      <DataTable
        rows={available}
        rowKey={(row) => row.trade_no}
        empty="暂无可开票订单。完成支付后可在这里申请。"
        columns={[
          {
            label: '选择',
            render: (row) => (
              <input
                type="checkbox"
                aria-label={`选择订单 ${row.trade_no}`}
                checked={selected.includes(row.trade_no)}
                disabled={props.pending}
                onChange={(event) =>
                  setSelected((values) =>
                    event.target.checked
                      ? [...values, row.trade_no]
                      : values.filter((value) => value !== row.trade_no),
                  )
                }
              />
            ),
          },
          { label: '订单号', render: (row) => row.trade_no },
          { label: '订单', render: (row) => row.order_title },
          {
            label: '金额',
            render: (row) => invoicePayment(row.order_amount_minor, row.currency),
            numeric: true,
          },
        ]}
      />
      <div className="form-panel">
        <label className="field" htmlFor="invoice-type">
          <span>{t('发票类型')}</span>
          <select id="invoice-type" value={kind} onChange={(event) => setKind(event.target.value)}>
            <option value="personal">{t('个人')}</option>
            <option value="enterprise">{t('企业')}</option>
          </select>
        </label>
        <Field name="invoice-title" label="发票抬头" required maxLength={255} />
        {kind === 'enterprise' && (
          <Field name="invoice-tax-number" label="企业税号" required maxLength={64} />
        )}
        <Field name="invoice-email" label="接收邮箱" type="email" required maxLength={255} />
        <Field name="invoice-remark" label="备注" maxLength={500} />
        <ErrorMessage error={error} />
        <Button
          disabled={props.pending || available.length === 0 || selected.length === 0}
          type="submit"
        >
          {props.pending ? t('提交中…') : t('提交申请（') + String(selected.length) + t(' 笔）')}
        </Button>
      </div>
    </form>
  )
}

export function InvoiceReview(props: {
  invoice: Schema['InvoiceRequest']
  pending: boolean
  onSave: (body: Schema['UpdateInvoiceRequestInput']) => void
  onCancel: () => void
}) {
  const { t } = useTranslation()
  const [status, setStatus] = useState('issued')
  return (
    <form
      className="form-panel section"
      onSubmit={(event) => {
        event.preventDefault()
        const form = new FormData(event.currentTarget)
        props.onSave({
          status,
          invoice_number:
            status === 'issued' ? String(form.get('invoice-number') ?? '').trim() : '',
          admin_note: String(form.get('invoice-note') ?? '').trim(),
        })
      }}
    >
      <p className="full-width">
        {t('处理')} {props.invoice.title} {t('的发票申请：')}
        {invoicePayment(props.invoice.order_amount_minor, props.invoice.currency)}
      </p>
      <label className="field" htmlFor="invoice-status">
        <span>{t('处理结果')}</span>
        <select
          id="invoice-status"
          value={status}
          onChange={(event) => setStatus(event.target.value)}
        >
          <option value="issued">{t('已开票')}</option>
          <option value="rejected">{t('拒绝申请')}</option>
        </select>
      </label>
      {status === 'issued' && (
        <Field name="invoice-number" label="发票号码" required maxLength={128} />
      )}
      <Field
        name="invoice-note"
        label={status === 'rejected' ? '拒绝原因' : '处理备注'}
        required={status === 'rejected'}
        maxLength={1000}
      />
      <div className="row-actions">
        <Button type="submit" disabled={props.pending}>
          {props.pending ? t('保存中…') : t('保存处理结果')}
        </Button>
        <Button type="button" variant="quiet" disabled={props.pending} onClick={props.onCancel}>
          {t('取消')}
        </Button>
      </div>
    </form>
  )
}
