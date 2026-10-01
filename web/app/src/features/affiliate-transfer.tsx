import { useState } from 'react'
import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import type { Schema } from '../lib/types'
import { sessionOptions } from '../lib/queries'
import { credits, toMicroCredits } from '../lib/format'
import { Button, ErrorMessage, Field } from '../components/ui'
import { errorFrom } from './commerce/amounts'

export function AffiliateTransfer() {
  const client = useQueryClient()
  const profile = useSuspenseQuery(sessionOptions()).data
  const balance = BigInt(profile.affiliate_micro_credits ?? 0)
  const [error, setError] = useState<Error | null>(null)
  const [draft, setDraft] = useState<Schema['AffiliateTransferInput'] | null>(null)
  const transfer = useMutation({
    mutationFn: (body: Schema['AffiliateTransferInput']) =>
      api.POST('/api/user/aff_transfer', { body }).then(unwrap),
    onSuccess: async () => {
      setDraft(null)
      await Promise.all([
        client.invalidateQueries({ queryKey: ['session'] }),
        client.invalidateQueries({ queryKey: ['wallet'] }),
      ])
    },
  })
  const prepare = (amount: bigint) => {
    if (amount < 1_000_000n) throw new Error('最低转入 1 credit')
    if (amount > balance) throw new Error('转入金额超过可提现余额')
    transfer.reset()
    setDraft({ amount_micro_credits: amount, operation_id: crypto.randomUUID() })
  }
  if (balance <= 0n && !draft && !transfer.isSuccess) return null
  return (
    <section className="section" aria-label="可提现余额转入钱包">
      <h2>可提现余额</h2>
      <p>当前可提现余额：{credits(balance)}</p>
      <p className="muted">转入钱包后可用于当前服务消费，最低转入 1 credit。</p>
      <ErrorMessage error={error ?? transfer.error} />
      {transfer.data && (
        <p role="status">已转入钱包 {credits(transfer.data.amount_micro_credits)}</p>
      )}
      {!draft && balance >= 1_000_000n && (
        <form
          className="form-panel"
          onSubmit={(event) => {
            event.preventDefault()
            setError(null)
            try {
              prepare(
                BigInt(
                  toMicroCredits(String(new FormData(event.currentTarget).get('affiliate-amount'))),
                ),
              )
            } catch (cause) {
              setError(errorFrom(cause))
            }
          }}
        >
          <Field name="affiliate-amount" label="转入金额 credits" required placeholder="1.00" />
          <Button type="submit" disabled={transfer.isPending}>
            核对转入金额
          </Button>
          <Button
            variant="quiet"
            disabled={transfer.isPending}
            onClick={() => {
              setError(null)
              prepare(balance)
            }}
          >
            全部转入钱包
          </Button>
        </form>
      )}
      {!draft && balance > 0n && balance < 1_000_000n && (
        <p className="muted">余额不足 1 credit，暂时无法转入。</p>
      )}
      {draft && (
        <div className="form-panel">
          <p className="full-width">确认将 {credits(draft.amount_micro_credits)} 转入钱包？</p>
          <Button disabled={transfer.isPending} onClick={() => transfer.mutate(draft)}>
            {transfer.isPending ? '转入中…' : transfer.isError ? '重试同一次转入' : '确认转入钱包'}
          </Button>
          {!transfer.isError && (
            <Button variant="quiet" disabled={transfer.isPending} onClick={() => setDraft(null)}>
              返回修改
            </Button>
          )}
          {transfer.isError && (
            <p className="muted full-width">重试会核对同一次转入结果，请勿重复创建转入。</p>
          )}
        </div>
      )}
    </section>
  )
}
