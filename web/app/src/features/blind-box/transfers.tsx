import { useState } from 'react'
import type { Schema } from '../../lib/types'
import { useTranslation } from '../../lib/i18n'
import { Button, ErrorMessage, Field, confirmAction } from '../../components/ui'
import { recipientID } from './presentation'

export function BoxTransfers(props: {
  items: readonly Schema['MarketplaceProp'][]
  available: boolean
  pending: boolean
  onGiftBox: (recipient: string) => void
  onGiftProp: (input: { id: string; recipient: string }) => void
}) {
  const { t } = useTranslation()
  const [error, setError] = useState<Error | null>(null)
  const [confirming, setConfirming] = useState(false)
  const transferable = props.items.filter(
    (item) => item.status === 'available' || item.status === 'paused',
  )
  const review = async (
    recipient: string,
    action: () => void,
    trigger: HTMLButtonElement | null,
  ) => {
    if (confirming || props.pending) return
    setError(null)
    const id = recipientID(recipient)
    if (!id) {
      setError(new Error(t('请输入有效的收件人用户 ID')))
      return
    }
    setConfirming(true)
    let accepted = false
    try {
      accepted = await confirmAction({
        title: `${t('确认赠送至用户')} ${id}?`,
        description: t('请核对用户 ID。赠送后物品将归收件人所有，无法自行撤回。'),
      })
      if (accepted) action()
    } finally {
      setConfirming(false)
      if (!accepted) requestAnimationFrame(() => trigger?.focus())
    }
  }
  const pending = props.pending || confirming
  return (
    <details className="section box-transfers">
      <summary>{t('赠送盲盒与道具')}</summary>
      <p className="muted">{t('请核对用户 ID。赠送后物品将归收件人所有，无法自行撤回。')}</p>
      <ErrorMessage error={error} />
      <div className="box-transfer-grid">
        <form
          className="form-panel"
          onSubmit={(event) => {
            event.preventDefault()
            const recipient = String(
              new FormData(event.currentTarget).get('box-recipient') ?? '',
            ).trim()
            void review(
              recipient,
              () => props.onGiftBox(recipient),
              event.currentTarget.querySelector('button[type="submit"]'),
            )
          }}
        >
          <h3>{t('赠送盲盒')}</h3>
          <Field name="box-recipient" label="收件人用户 ID" required maxLength={19} />
          <Button type="submit" variant="quiet" disabled={props.pending || !props.available}>
            {t('赠送一个')}
          </Button>
        </form>
        <form
          className="form-panel"
          onSubmit={(event) => {
            event.preventDefault()
            const values = new FormData(event.currentTarget)
            const id = String(values.get('gift-prop') ?? '')
            const recipient = String(values.get('prop-recipient') ?? '').trim()
            if (!transferable.some((item) => String(item.id) === id)) return
            void review(
              recipient,
              () => props.onGiftProp({ id, recipient }),
              event.currentTarget.querySelector('button[type="submit"]'),
            )
          }}
        >
          <h3>{t('赠送道具')}</h3>
          <div className="field">
            <label htmlFor="box-gift-prop">{t('道具')}</label>
            <select
              id="box-gift-prop"
              name="gift-prop"
              required
              disabled={pending || !transferable.length}
            >
              <option value="">{t('选择道具')}</option>
              {transferable.map((item) => (
                <option key={String(item.id)} value={String(item.id)}>
                  {item.title}
                </option>
              ))}
            </select>
          </div>
          <Field name="prop-recipient" label="道具收件人用户 ID" required maxLength={19} />
          <Button type="submit" variant="quiet" disabled={props.pending || !transferable.length}>
            {t('赠送')}
          </Button>
        </form>
      </div>
    </details>
  )
}
