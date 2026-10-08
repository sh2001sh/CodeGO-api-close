import { useState } from 'react'
import { Button, Dialog, ErrorMessage, Field } from '../../components/ui'
import { useTranslation } from '../../lib/i18n'
import type { InvoiceBuyer } from './download-invoice'

/** Purchaser details stay in this page's memory; issued documents are frozen by the server. */
export function InvoiceBuyerDialog(props: {
  trade: string | null
  draft?: InvoiceBuyer
  pending: boolean
  error: Error | null
  onClose: () => void
  onIssue: (buyer: InvoiceBuyer) => void
}) {
  const { t } = useTranslation()
  const [error, setError] = useState<Error | null>(null)
  return (
    <Dialog
      open={props.trade !== null}
      title="开具商业发票"
      description="填写个人姓名或公司全称及真实地址。开具后，抬头和地址不可更改。"
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
          props.onIssue({ buyer_name: name, buyer_address: address })
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
            {t(props.pending ? '开具中…' : '开具并下载')}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
