import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import type { Schema } from '../../lib/types'
import { credits } from '../../lib/format'
import { Button, ErrorMessage, Field } from '../../components/ui'
import { errorFrom, transferAmounts } from './amounts'

export function TransferForm(props: { overview: Schema['WalletOverview'] }) {
  const [error, setError] = useState<Error | null>(null)
  const [draft, setDraft] = useState<Schema['WalletTransferInput'] | null>(null)
  const [recipient, setRecipient] = useState<Schema['WalletRecipient'] | null>(null)
  const [formKey, setFormKey] = useState(0)
  const queryClient = useQueryClient()
  const overview = props.overview
  const locked = Number(overview.security.locked_until) * 1000 > Date.now()
  const lookup = useMutation({
    mutationFn: (externalID: string) =>
      api
        .GET('/api/wallet/transfers/recipients/{external_id}', {
          params: { path: { external_id: externalID } },
        })
        .then((result) => unwrap(result)),
    onSuccess: setRecipient,
  })
  const send = useMutation({
    mutationFn: (body: Schema['WalletTransferInput']) =>
      api.POST('/api/wallet/transfers', { body }).then((result) => unwrap(result)),
    onSuccess: () => {
      setDraft(null)
      setRecipient(null)
      setFormKey((value) => value + 1)
      void queryClient.invalidateQueries({ queryKey: ['transfers'] })
      void queryClient.invalidateQueries({ queryKey: ['wallet'] })
    },
    onError: () => {
      void queryClient.invalidateQueries({ queryKey: ['transfers'] })
    },
  })
  const fee = draft ? (BigInt(draft.amount_micro) * BigInt(overview.fee_bps) + 9999n) / 10000n : 0n
  return (
    <section className="section">
      <h2>转账</h2>
      <p className="muted">
        最低 {credits(overview.min_micro)}，金额按该单位递增；手续费 {overview.fee_bps / 100}%
        ，由付款人承担。
      </p>
      {!overview.security.password_set && <p>先在下方设置支付密码，再向其他用户转账。</p>}
      {locked && <p role="alert">支付密码已临时锁定。</p>}
      {send.isSuccess && <p role="status">转账成功。</p>}
      <ErrorMessage error={error ?? lookup.error ?? send.error} />
      {!recipient && (
        <form
          key={formKey}
          className="form-panel"
          onSubmit={(event) => {
            event.preventDefault()
            setError(null)
            try {
              const form = new FormData(event.currentTarget)
              const externalID = String(form.get('recipient') ?? '')
                .trim()
                .toUpperCase()
              if (!/^[A-Z0-9]{6}$/.test(externalID)) throw new Error('请输入六位收款人 ID')
              const amount = transferAmounts(
                String(form.get('transfer-amount') ?? ''),
                overview.fee_bps,
              )
              if (amount.amount < BigInt(overview.min_micro))
                throw new Error('转账金额低于最低额度')
              if (amount.amount % BigInt(overview.min_micro) !== 0n)
                throw new Error(`转账金额需按 ${credits(overview.min_micro)} 递增`)
              if (amount.total > BigInt(overview.balance))
                throw new Error('余额不足以支付金额和手续费')
              setDraft({
                recipient_external_id: externalID,
                amount_micro: amount.amount,
                payment_password: String(form.get('transfer-password') ?? ''),
                request_id: crypto.randomUUID(),
              })
              send.reset()
              lookup.mutate(externalID)
            } catch (cause) {
              setError(errorFrom(cause))
            }
          }}
        >
          <Field name="recipient" label="收款人 ID" required maxLength={6} placeholder="ABC123" />
          <Field name="transfer-amount" label="转账 credits" required placeholder="10.00" />
          <Field
            name="transfer-password"
            label="支付密码"
            type="password"
            required
            maxLength={72}
          />
          <Button
            type="submit"
            disabled={!overview.security.password_set || lookup.isPending || locked}
          >
            {lookup.isPending ? '核对中…' : '核对收款人'}
          </Button>
        </form>
      )}
      {recipient && draft && (
        <div className="form-panel">
          <p className="full-width">
            确认向 <strong>{recipient.display_name_masked}</strong>（{recipient.external_id}）转账。
          </p>
          <dl>
            <div>
              <dt>转账金额</dt>
              <dd>{credits(draft.amount_micro)}</dd>
            </div>
            <div>
              <dt>手续费</dt>
              <dd>{credits(fee)}</dd>
            </div>
            <div>
              <dt>合计扣款</dt>
              <dd>{credits(BigInt(draft.amount_micro) + fee)}</dd>
            </div>
          </dl>
          {send.isError && (
            <label className="field" htmlFor="retry-payment-password">
              <span>支付密码</span>
              <input
                id="retry-payment-password"
                type="password"
                maxLength={72}
                value={draft.payment_password}
                onChange={(event) => setDraft({ ...draft, payment_password: event.target.value })}
              />
            </label>
          )}
          <div className="row-actions">
            <Button disabled={send.isPending || locked} onClick={() => send.mutate(draft)}>
              {send.isPending ? '转账中…' : send.isError ? '重试同一笔转账' : '确认转账'}
            </Button>
            {!send.isError && (
              <Button
                variant="quiet"
                disabled={send.isPending}
                onClick={() => {
                  setDraft(null)
                  setRecipient(null)
                }}
              >
                返回修改
              </Button>
            )}
          </div>
          {send.isError && (
            <p className="muted full-width">
              可更正支付密码后重试。收款人、金额和请求编号保持一致，请勿重复创建转账。
            </p>
          )}
        </div>
      )}
    </section>
  )
}
