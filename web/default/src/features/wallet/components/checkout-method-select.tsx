import { useTranslation } from 'react-i18next'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

export function CheckoutMethodSelect(props: {
  value: string
  onChange: (value: string) => void
  methods: { type: string; name?: string }[]
  enableStripe?: boolean
  enableCreem?: boolean
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const methods = props.methods.filter(
    (method) => !method.type.startsWith('stripe') && method.type !== 'creem'
  )
  if (props.enableStripe) methods.push({ type: 'stripe', name: 'Stripe' })
  if (props.enableCreem) methods.push({ type: 'creem', name: 'Creem' })
  const label = (method: { type: string; name?: string }) => {
    if (method.type === 'alipay') return t('支付宝支付')
    if (method.type === 'wxpay' || method.type === 'xunhu') return t('微信支付')
    return method.name || method.type
  }
  const selected = methods.find((method) => method.type === props.value)
  return (
    <Select
      value={props.value}
      onValueChange={(value) => value && props.onChange(value)}
      disabled={props.disabled}
    >
      <SelectTrigger
        aria-label={t('Payment Method')}
        className='w-full sm:w-44'
      >
        <SelectValue>
          {selected ? label(selected) : t('Payment Method')}
        </SelectValue>
      </SelectTrigger>
      <SelectContent alignItemWithTrigger={false}>
        {methods.map((method) => (
          <SelectItem key={method.type} value={method.type}>
            {label(method)}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
