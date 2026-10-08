import type { Schema } from '../../lib/types'
import { useTranslation } from '../../lib/i18n'

export function selectedPaymentType(method: Schema['PaymentMethod'] | undefined, value: string) {
  const choices = method?.payment_types ?? []
  return choices.includes(value) ? value : (choices[0] ?? '')
}

export function PaymentTypeField(props: {
  id: string
  method: Schema['PaymentMethod'] | undefined
  value: string
  onChange: (value: string) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const choices = props.method?.payment_types ?? []
  if (!choices.length) return null
  return (
    <label className="field" htmlFor={props.id}>
      <span>{t('支付方式')}</span>
      <select
        id={props.id}
        value={selectedPaymentType(props.method, props.value)}
        disabled={props.disabled}
        onChange={(event) => props.onChange(event.target.value)}
      >
        {choices.map((type) => (
          <option key={type} value={type}>
            {type === 'alipay' ? t('支付宝') : type === 'wxpay' ? t('微信支付') : type}
          </option>
        ))}
      </select>
    </label>
  )
}
