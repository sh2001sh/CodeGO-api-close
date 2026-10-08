import { useTranslation } from '../../lib/i18n'
import { Button } from '../../components/ui'
import type { APIKey } from '../../lib/types'

export function KeyFilterBar(props: {
  search: string
  onSearchChange: (value: string) => void
  statusFilter: '' | APIKey['status']
  onStatusChange: (value: '' | APIKey['status']) => void
}) {
  const { t } = useTranslation()
  return (
    <form className="filters" onSubmit={(event) => event.preventDefault()}>
      <label className="field" htmlFor="key-search">
        <span>{t('搜索名称')}</span>
        <input
          id="key-search"
          value={props.search}
          onChange={(event) => props.onSearchChange(event.target.value)}
          placeholder={t('输入关键字')}
        />
      </label>
      <label className="field" htmlFor="key-status-filter">
        <span>{t('状态')}</span>
        <select
          id="key-status-filter"
          value={props.statusFilter}
          onChange={(event) => props.onStatusChange(event.target.value as '' | APIKey['status'])}
        >
          <option value="">{t('全部')}</option>
          <option value="active">{t('active')}</option>
          <option value="disabled">{t('disabled')}</option>
        </select>
      </label>
    </form>
  )
}

export function KeyBatchBar(props: {
  total: number
  selectedCount: number
  allSelected: boolean
  pending: boolean
  onToggleAll: (checked: boolean) => void
  onEnable: () => void
  onDisable: () => void
  onDelete: () => void
}) {
  const { t } = useTranslation()
  if (props.total === 0) return null
  return (
    <div className="row-actions">
      <label className="checkbox-field">
        <input
          type="checkbox"
          checked={props.allSelected}
          onChange={(event) => props.onToggleAll(event.target.checked)}
        />
        {t('全选当前页')}
      </label>
      {props.selectedCount > 0 && (
        <div className="row-actions" role="toolbar" aria-label={t('批量操作')}>
          <span className="subtle">
            {t('已选择')} {props.selectedCount}
          </span>
          <Button variant="quiet" disabled={props.pending} onClick={props.onEnable}>
            {t('批量启用')}
          </Button>
          <Button variant="quiet" disabled={props.pending} onClick={props.onDisable}>
            {t('批量停用')}
          </Button>
          <Button variant="danger" disabled={props.pending} onClick={props.onDelete}>
            {t('批量删除')}
          </Button>
        </div>
      )}
    </div>
  )
}
