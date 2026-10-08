import { useTranslation } from '../../lib/i18n'
// Filter bar for the channels page. `GET /api/catalog/channels` only accepts
// `keyword` server-side today (see v3/internal/catalogcontrol/channels.go
// listChannels) even though the openapi schema also declares `group` and
// `model` query parameters that the handler never reads. Status, group, tag
// and provider are therefore filtered client-side against the fetched page.
import { useState } from 'react'
import { Button, Field, SelectField } from '../../components/ui'
import type { Channel } from './types'

export type ChannelFilterState = {
  keyword: string
  status: string
  group: string
  tag: string
  provider: string
}

export const emptyChannelFilters: ChannelFilterState = {
  keyword: '',
  status: '',
  group: '',
  tag: '',
  provider: '',
}

export function matchesChannelFilters(channel: Channel, filters: ChannelFilterState): boolean {
  if (filters.status && channel.status !== filters.status) return false
  if (filters.group && !(channel.groups ?? []).includes(filters.group)) return false
  if (filters.tag && (channel.tag ?? '') !== filters.tag) return false
  if (filters.provider && channel.provider !== filters.provider) return false
  return true
}

export function ChannelFilterBar(props: {
  initialKeyword: string
  filters: ChannelFilterState
  onSearch: (keyword: string) => void
  onFiltersChange: (filters: ChannelFilterState) => void
}) {
  const { t } = useTranslation()
  const [local, setLocal] = useState(props.filters)
  return (
    <form
      className="filters"
      onSubmit={(event) => {
        event.preventDefault()
        const form = new FormData(event.currentTarget)
        const keyword = String(form.get('keyword') ?? '')
        const next: ChannelFilterState = {
          keyword,
          status: String(form.get('status') ?? ''),
          group: String(form.get('group') ?? '').trim(),
          tag: String(form.get('tag') ?? '').trim(),
          provider: String(form.get('provider') ?? '').trim(),
        }
        setLocal(next)
        props.onFiltersChange(next)
        props.onSearch(keyword)
      }}
    >
      <Field name="keyword" label="关键字" defaultValue={props.initialKeyword} placeholder="名称" />
      <Field name="group" label="分组" defaultValue={local.group} />
      <Field name="tag" label="标签" defaultValue={local.tag} />
      <Field name="provider" label="Provider" defaultValue={local.provider} />
      <SelectField
        name="status"
        label="状态"
        defaultValue={local.status}
        options={[
          { value: '', label: '全部状态' },
          { value: 'enabled', label: 'enabled' },
          { value: 'disabled', label: 'disabled' },
          { value: 'auto_disabled', label: 'auto_disabled' },
        ]}
      />
      <Button type="submit">{t('搜索')}</Button>
      <Button
        type="button"
        variant="quiet"
        onClick={() => {
          setLocal(emptyChannelFilters)
          props.onFiltersChange(emptyChannelFilters)
          props.onSearch('')
        }}
      >
        {t('重置')}
      </Button>
    </form>
  )
}
