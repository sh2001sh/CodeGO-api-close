import { useMutation } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { KeyRound } from 'lucide-react'
import { api, unwrap } from '../../lib/api'
import { useTranslation } from '../../lib/i18n'
import { Button, ErrorMessage } from '../../components/ui'
import type { APIKey } from '../../lib/types'

/**
 * Lets the user pick one of their keys and reveal its secret on demand.
 * The secret lives only in the parent's React state (never localStorage).
 */
export function KeyPicker(props: {
  keys: APIKey[]
  selectedID: string
  onSelect: (id: string) => void
  hasSecret: boolean
  onReveal: (secret: string) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const reveal = useMutation({
    mutationFn: (id: string) =>
      api
        .POST('/api/token/{id}/key', { params: { path: { id } } })
        .then((result) => unwrap(result)),
    onSuccess: (value) => props.onReveal(value.key),
  })
  const activeKeys = props.keys.filter((key) => key.status === 'active')
  return (
    <div className="field">
      <label className="field-label" htmlFor="playground-key">
        {t('API Key')}
      </label>
      <select
        id="playground-key"
        value={props.selectedID}
        disabled={props.disabled || reveal.isPending}
        onChange={(event) => props.onSelect(event.target.value)}
      >
        <option value="">{t('选择 Key')}</option>
        {activeKeys.map((key) => (
          <option key={String(key.id)} value={String(key.id)}>
            {key.name}
          </option>
        ))}
      </select>
      <Button
        type="button"
        variant="secondary"
        size="sm"
        disabled={!props.selectedID || reveal.isPending || props.disabled}
        loading={reveal.isPending}
        onClick={() => reveal.mutate(props.selectedID)}
      >
        <KeyRound size={14} aria-hidden />
        {props.hasSecret ? t('已启用') : t('使用此 Key')}
      </Button>
      <ErrorMessage error={reveal.error} />
      {!activeKeys.length && (
        <Link className="btn btn-secondary" to="/keys">
          {t('创建 API Key')}
        </Link>
      )}
    </div>
  )
}
