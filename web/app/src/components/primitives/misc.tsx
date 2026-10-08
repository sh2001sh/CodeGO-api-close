import { Tabs as BaseTabs } from '@base-ui/react/tabs'
import { useState, type ReactNode } from 'react'
import { Check, Copy } from 'lucide-react'
import { useTranslation } from '../../lib/i18n'
import { useToast } from '../../hooks/use-toast'
import { Button, IconButton } from './button'

async function writeClipboard(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    return false
  }
}

/** Copies `value`; shows a check for 1.5s and reports failure through a toast. */
export function CopyButton(props: { value: string; label?: string; withText?: boolean }) {
  const { t } = useTranslation()
  const toast = useToast((state) => state.add)
  const [done, setDone] = useState(false)
  const label = t(props.label ?? '复制')
  const run = async () => {
    if (await writeClipboard(props.value)) {
      setDone(true)
      setTimeout(() => setDone(false), 1500)
    } else toast(t('复制失败，请手动复制'), 'error')
  }
  const icon = done ? <Check size={15} aria-hidden /> : <Copy size={15} aria-hidden />
  if (props.withText)
    return (
      <Button variant="secondary" size="sm" onClick={run}>
        {icon}
        {done ? t('已复制') : label}
      </Button>
    )
  return (
    <IconButton label={done ? t('已复制') : label} onClick={run}>
      {icon}
    </IconButton>
  )
}

export function CopyField(props: { value: string; label?: string }) {
  return (
    <div className="copy-field">
      <code title={props.value}>{props.value}</code>
      <CopyButton value={props.value} label={props.label} />
    </div>
  )
}

export type TabItem = { value: string; label: string; icon?: ReactNode; content: ReactNode }

/** Underlined tabs; pass `value`/`onValueChange` to sync with the URL. */
export function Tabs(props: {
  items: readonly TabItem[]
  value?: string
  defaultValue?: string
  onValueChange?: (value: string) => void
  label?: string
}) {
  const { t } = useTranslation()
  return (
    <BaseTabs.Root
      value={props.value}
      defaultValue={props.defaultValue ?? props.items[0]?.value}
      onValueChange={(value) => props.onValueChange?.(String(value))}
    >
      <BaseTabs.List className="tabs-list" aria-label={props.label ? t(props.label) : undefined}>
        {props.items.map((item) => (
          <BaseTabs.Tab key={item.value} value={item.value} className="tabs-tab">
            {item.icon}
            {t(item.label)}
          </BaseTabs.Tab>
        ))}
      </BaseTabs.List>
      {props.items.map((item) => (
        <BaseTabs.Panel key={item.value} value={item.value}>
          {item.content}
        </BaseTabs.Panel>
      ))}
    </BaseTabs.Root>
  )
}

export function Segmented<T extends string>(props: {
  value: T
  options: readonly { value: T; label: string }[]
  onChange: (value: T) => void
  label: string
}) {
  const { t } = useTranslation()
  return (
    <div className="segmented" role="group" aria-label={t(props.label)}>
      {props.options.map((option) => (
        <button
          key={option.value}
          type="button"
          aria-pressed={props.value === option.value}
          onClick={() => props.onChange(option.value)}
        >
          {t(option.label)}
        </button>
      ))}
    </div>
  )
}

/** Page-based pagination. `total` may be unknown; then only previous/next are shown. */
export function Pagination(props: {
  page: number
  pageSize: number
  total?: number | bigint
  hasMore?: boolean
  pending?: boolean
  onChange: (page: number) => void
  label?: string
}) {
  const { t } = useTranslation()
  const total = props.total === undefined ? undefined : Number(props.total)
  const pages = total === undefined ? undefined : Math.max(1, Math.ceil(total / props.pageSize))
  const hasNext = pages === undefined ? Boolean(props.hasMore) : props.page < pages
  return (
    <nav className="pagination" aria-label={t(props.label ?? '分页')}>
      {total !== undefined && (
        <span className="subtle">
          {t('共')} {total.toLocaleString()} {t('条')}
        </span>
      )}
      <Button
        variant="secondary"
        size="sm"
        disabled={props.page <= 1 || props.pending}
        onClick={() => props.onChange(props.page - 1)}
      >
        {t('上一页')}
      </Button>
      <span className="tabular">
        {props.page}
        {pages !== undefined && ` / ${pages}`}
      </span>
      <Button
        variant="secondary"
        size="sm"
        disabled={!hasNext || props.pending}
        onClick={() => props.onChange(props.page + 1)}
      >
        {t('下一页')}
      </Button>
    </nav>
  )
}
