import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../lib/api'
import type { Schema } from '../lib/types'
import { toMicroCredits } from '../lib/format'
import { useTranslation } from '../lib/i18n'
import { Button, ErrorMessage, Field } from './ui'

export function BalanceAdjustment(props: { onClose: () => void }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [operationID, setOperationID] = useState(() => crypto.randomUUID())
  const [error, setError] = useState<Error | null>(null)
  const [complete, setComplete] = useState(false)
  const mutation = useMutation({
    mutationFn: (body: Schema['AdjustmentInput']) => api.POST('/api/billing/adjustments', { body }),
    onSuccess: () => {
      setOperationID(crypto.randomUUID())
      setComplete(true)
      void queryClient.invalidateQueries({ queryKey: ['wallet'] })
    },
  })
  return (
    <section className="section">
      <h2>{t('余额调整')}</h2>
      <form
        className="form-panel"
        onSubmit={(event) => {
          event.preventDefault()
          setError(null)
          setComplete(false)
          const fields = new FormData(event.currentTarget)
          try {
            const value = String(fields.get('amount')).trim()
            const negative = value.startsWith('-')
            const amount =
              BigInt(toMicroCredits(negative ? value.slice(1) : value)) * (negative ? -1n : 1n)
            const accountID = Number(fields.get('account_id'))
            if (!Number.isSafeInteger(accountID) || accountID <= 0) throw new Error('账户编号无效')
            mutation.mutate({
              account_id: accountID,
              amount_micro: amount,
              reason: String(fields.get('reason')),
              operation_id: operationID,
            })
          } catch (failure) {
            setError(failure as Error)
          }
        }}
      >
        <Field name="account_id" label="钱包账户 ID" type="number" min={1} required />
        <Field name="amount" label="调整 credits" required placeholder="10.000001 / -10" />
        <Field name="reason" label="调整原因" required maxLength={1000} />
        <Button disabled={mutation.isPending} type="submit">
          {t('入账')}
        </Button>
        <Button variant="quiet" type="button" onClick={props.onClose}>
          {t('取消')}
        </Button>
      </form>
      <ErrorMessage error={mutation.error ?? error} />
      {complete && (
        <p className="notice" role="status">
          {t('已入账')}
        </p>
      )}
    </section>
  )
}
