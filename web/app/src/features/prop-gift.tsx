import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../lib/api'
import type { Schema } from '../lib/types'
import { Button, ErrorMessage, Field } from '../components/ui'

export function GiftProp(props: { items: Schema['MarketplaceProp'][] }) {
  const [operation, setOperation] = useState(() => crypto.randomUUID())
  const client = useQueryClient()
  const gift = useMutation({
    mutationFn: (input: { id: string; recipient: bigint }) =>
      api.POST('/api/blind-box/props/{id}/gift', {
        params: { path: { id: input.id } },
        body: { request_id: operation, recipient_id: input.recipient },
      }),
    onSuccess: () => {
      setOperation(crypto.randomUUID())
      return client.invalidateQueries({ queryKey: ['boxes'] })
    },
  })
  const [error, setError] = useState<Error | null>(null)
  const available = props.items.filter(
    (item) => item.status === 'available' || item.status === 'paused',
  )
  return (
    <section className="section">
      <h2>赠送道具</h2>
      <form
        className="form-panel"
        onSubmit={(event) => {
          event.preventDefault()
          setError(null)
          const values = new FormData(event.currentTarget)
          try {
            gift.mutate({
              id: String(values.get('gift-prop')),
              recipient: BigInt(String(values.get('gift-prop-recipient'))),
            })
          } catch (cause) {
            setError(cause instanceof Error ? cause : new Error('参数无效'))
          }
        }}
      >
        <label className="field" htmlFor="gift-prop">
          <span>道具</span>
          <select id="gift-prop" name="gift-prop" required>
            <option value="">选择道具</option>
            {available.map((item) => (
              <option key={String(item.id)} value={String(item.id)}>
                {item.title}
              </option>
            ))}
          </select>
        </label>
        <Field
          name="gift-prop-recipient"
          label="道具收件人用户 ID"
          required
          placeholder="用户 ID"
        />
        <Button type="submit" disabled={gift.isPending || !available.length}>
          赠送
        </Button>
      </form>
      <ErrorMessage error={gift.error ?? error} />
      {gift.isSuccess && (
        <p role="status" className="notice">
          已赠送
        </p>
      )}
    </section>
  )
}
