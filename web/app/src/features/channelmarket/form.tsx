import { useTranslation } from '../../lib/i18n'
import { useState, type ReactNode } from 'react'
import { Button, ErrorMessage } from '../../components/ui'

export function MarketForm(props: {
  children: ReactNode
  pending?: boolean
  submit?: string
  onSubmit: (fields: FormData) => void
}) {
  const { t } = useTranslation()
  const [error, setError] = useState<Error | null>(null)
  return (
    <form
      className="form-panel"
      onSubmit={(event) => {
        event.preventDefault()
        if (props.pending) return
        setError(null)
        try {
          props.onSubmit(new FormData(event.currentTarget))
        } catch (failure) {
          setError(failure instanceof Error ? failure : new Error('参数无效'))
        }
      }}
    >
      {props.children}
      <Button type="submit" disabled={props.pending}>
        {props.pending ? t('正在提交…') : t(props.submit ?? '保存')}
      </Button>
      <ErrorMessage error={error} />
    </form>
  )
}

export const text = (fields: FormData, key: string) => String(fields.get(key) ?? '').trim()
export function integer(fields: FormData, key: string): bigint {
  const value = text(fields, key)
  if (!/^[1-9]\d*$/.test(value)) throw new Error('请输入有效的正整数 ID')
  const number = BigInt(value)
  if (number > 9_223_372_036_854_775_807n) throw new Error('ID 超出允许范围')
  return number
}
export function factor(fields: FormData, key = 'multiplier'): number {
  const value = text(fields, key)
  if (!/^\d+(\.\d{1,6})?$/.test(value)) throw new Error('倍率最多保留六位小数')
  const number = Number(value)
  if (number <= 0 || number > 1000) throw new Error('倍率应大于 0 且不超过 1000')
  return number
}
