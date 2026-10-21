import { useTranslation } from 'react-i18next'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { isStripeCurrency, type StripeCurrency } from '../lib/stripe'

export function StripeCurrencySelect(props: {
  value: StripeCurrency
  onChange: (value: StripeCurrency) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  return (
    <Select
      value={props.value}
      onValueChange={(value) => {
        if (value && isStripeCurrency(value)) props.onChange(value)
      }}
      disabled={props.disabled}
    >
      <SelectTrigger
        aria-label={t('Payment currency')}
        className='w-full sm:w-40'
      >
        <SelectValue>{props.value}</SelectValue>
      </SelectTrigger>
      <SelectContent alignItemWithTrigger={false}>
        <SelectItem value='HKD'>HKD · HK$</SelectItem>
        <SelectItem value='CNY'>CNY · ¥</SelectItem>
        <SelectItem value='USD'>USD · US$</SelectItem>
      </SelectContent>
    </Select>
  )
}
