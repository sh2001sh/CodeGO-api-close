import { useState } from 'react'
import { Button, Dialog, ErrorMessage, Field, TextAreaField } from '../../components/ui'
import { useTranslation } from '../../lib/i18n'
import type { InvoiceBuyer } from './download-invoice'

/** Purchaser details stay in this page's memory; issued documents are frozen by the server. */
export function InvoiceBuyerDialog(props: {
  trade: string | null
  draft?: InvoiceBuyer
  pending: boolean
  error: Error | null
  onClose: () => void
  correction?: boolean
  onIssue: (buyer: InvoiceBuyer, reason?: string) => void
}) {
  const { t } = useTranslation()
  const [error, setError] = useState<Error | null>(null)
  return (
    <Dialog
      open={props.trade !== null}
      title={props.correction ? '更正购买方资料' : '开具商业发票'}
      description="填写真实购买方资料。更正将生成新版本，原发票保留。"
      onOpenChange={(open) => {
        if (!open && !props.pending) {
          setError(null)
          props.onClose()
        }
      }}
    >
      <form
        key={props.trade}
        className="form-panel"
        onSubmit={(event) => {
          event.preventDefault()
          setError(null)
          const form = new FormData(event.currentTarget)
          const name = String(form.get('invoice-buyer-name') ?? '').trim()
          const address = String(form.get('invoice-buyer-address') ?? '').trim()
          if (!name || !address || [...name].length > 200 || [...address].length > 600) {
            setError(new Error('请填写完整抬头和购买方地址'))
            return
          }
          const country = String(form.get('invoice-buyer-country') ?? '').trim()
          const taxID = String(form.get('invoice-buyer-tax-id') ?? '').trim()
          const reason = String(form.get('invoice-correction-reason') ?? '').trim()
          if (
            [...country].length > 100 ||
            [...taxID].length > 64 ||
            (props.correction && (!reason || [...reason].length > 300))
          ) {
            setError(new Error('请检查购买方资料及更正原因'))
            return
          }
          props.onIssue(
            {
              buyer_name: name,
              buyer_address: address,
              ...(country ? { buyer_country: country } : {}),
              ...(taxID ? { buyer_tax_id: taxID } : {}),
            },
            reason,
          )
        }}
      >
        <Field
          name="invoice-buyer-name"
          label="发票抬头"
          hint="个人姓名或公司全称，无需中国内地税号"
          required
          maxLength={200}
          disabled={props.pending}
          defaultValue={props.draft?.buyer_name}
          autoComplete="organization"
        />
        <div className="field">
          <label className="field-label" htmlFor="invoice-buyer-address">
            {t('购买方地址')}
          </label>
          <textarea
            id="invoice-buyer-address"
            name="invoice-buyer-address"
            required
            maxLength={600}
            rows={4}
            disabled={props.pending}
            defaultValue={props.draft?.buyer_address}
            autoComplete="street-address"
            aria-describedby="invoice-buyer-address-hint"
          />
          <span className="field-hint" id="invoice-buyer-address-hint">
            {t('填写街道、楼层或室号、城市、地区及国家')}
          </span>
        </div>
        <Field
          name="invoice-buyer-country"
          label="国家或地区（可选）"
          maxLength={100}
          disabled={props.pending}
          defaultValue={props.draft?.buyer_country}
          autoComplete="country-name"
        />
        <Field
          name="invoice-buyer-tax-id"
          label="购买方税号（可选）"
          hint="按报销机构要求填写，并非香港增值税号码"
          maxLength={64}
          disabled={props.pending}
          defaultValue={props.draft?.buyer_tax_id}
        />
        {props.correction && (
          <TextAreaField
            name="invoice-correction-reason"
            label="更正原因"
            required
            maxLength={300}
            disabled={props.pending}
          />
        )}
        <ErrorMessage error={error ?? props.error} />
        <div className="row-actions full-width">
          <Button
            type="button"
            variant="secondary"
            disabled={props.pending}
            onClick={() => {
              setError(null)
              props.onClose()
            }}
          >
            {t('取消')}
          </Button>
          <Button type="submit" disabled={props.pending}>
            {t(props.pending ? '开具中…' : props.correction ? '更正并下载' : '开具并下载')}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
