import { Fragment, useMemo, useState, type ReactNode } from 'react'
import { Link } from '@tanstack/react-router'
import { ChevronDown, ShieldCheck } from 'lucide-react'
import { useTranslation } from '../../lib/i18n'
import { Button, CopyButton, EmptyState, ErrorMessage, Segmented } from '../../components/ui'
import {
  filterRows,
  multiplierLabel,
  sortRows,
  type BoardHealth,
  type BoardRow,
  type BoardSort,
} from './board-data'

const sortOptions: readonly { value: BoardSort; label: string }[] = [
  { value: 'multiplier', label: '倍率' },
  { value: 'latency', label: '探测延迟' },
  { value: 'models', label: '模型数' },
]
const healthLabel: Record<BoardHealth, string> = { up: '可用', down: '异常', idle: '暂无数据' }
const COLUMNS = 7

function ago(value: string | null, locale: string): string {
  if (!value) return '—'
  const minutes = Math.max(0, Math.round((Date.now() - new Date(value).getTime()) / 60_000))
  if (!Number.isFinite(minutes)) return '—'
  const relative = new Intl.RelativeTimeFormat(locale, { numeric: 'auto', style: 'short' })
  if (minutes < 60) return relative.format(-minutes, 'minute')
  const hours = Math.round(minutes / 60)
  return hours < 48
    ? relative.format(-hours, 'hour')
    : relative.format(-Math.round(hours / 24), 'day')
}

function Verified(props: { verified: boolean }) {
  const { t } = useTranslation()
  return props.verified ? (
    <span className="board-verified">
      <ShieldCheck size={14} aria-hidden />
      {t('已验证')}
    </span>
  ) : (
    <span className="subtle">{t('待验证')}</span>
  )
}

function RowDetail(props: { row: BoardRow }) {
  const { t, locale } = useTranslation()
  const row = props.row
  return (
    <div className="board-detail">
      {/* Verification and last probe are hidden as columns on narrow screens; repeat them here. */}
      <dl className="board-detail-facts">
        <div>
          <dt>{t('分组 ID')}</dt>
          <dd>
            <code dir="ltr">{row.id}</code>
            <CopyButton value={row.id} label="复制分组 ID" />
          </dd>
        </div>
        <div>
          <dt>{t('验证')}</dt>
          <dd>
            <Verified verified={row.verified} />
          </dd>
        </div>
        <div>
          <dt>{t('最近探测')}</dt>
          <dd className="board-fig">{ago(row.probedAt, locale)}</dd>
        </div>
      </dl>
      <ul className="board-models" aria-label={t('分组模型')}>
        {row.models.map((model) => (
          <li key={model}>
            <code>{model}</code>
            <CopyButton value={model} label="复制模型名" />
          </li>
        ))}
      </ul>
      <div className="board-detail-actions">
        <Link
          to="/models"
          search={{ q: row.models[0] }}
          className="button button-secondary"
          data-size="sm"
        >
          {t('查看模型')}
        </Link>
        <Link
          to="/channel-market"
          search={{ group: row.id }}
          className="button button-primary"
          data-size="sm"
        >
          {t('绑定此分组')}
        </Link>
      </div>
    </div>
  )
}

function BoardLine(props: { row: BoardRow; open: boolean; onToggle: () => void }) {
  const { t, locale } = useTranslation()
  const row = props.row
  const detailId = `board-detail-${row.id}`
  return (
    <>
      <tr data-health={row.health} data-open={props.open || undefined}>
        <th scope="row">
          <button
            type="button"
            className="board-toggle"
            aria-expanded={props.open}
            aria-controls={detailId}
            onClick={props.onToggle}
          >
            <span className="board-dot" data-health={row.health} aria-hidden />
            <span className="board-name">{row.name}</span>
            <ChevronDown size={14} aria-hidden className="board-chevron" />
          </button>
          <span className="board-sub">
            {row.health !== 'up' && (
              <span className="board-health" data-health={row.health}>
                {t(healthLabel[row.health])}
              </span>
            )}
            {row.provider}
          </span>
        </th>
        <td className="numeric board-mult">{multiplierLabel(row.multiplierPpm)}</td>
        <td className="numeric board-fig hide-mobile">{row.models.length}</td>
        <td className="hide-mobile board-vendors">{row.vendors.slice(0, 3).join(' · ')}</td>
        <td className="numeric board-fig">
          {row.latencyMs === null ? '—' : `${row.latencyMs} ms`}
        </td>
        <td className="hide-mobile">
          <Verified verified={row.verified} />
        </td>
        <td className="numeric board-fig hide-mobile subtle">{ago(row.probedAt, locale)}</td>
      </tr>
      {props.open && (
        <tr className="board-detail-row" id={detailId}>
          <td colSpan={COLUMNS}>
            <RowDetail row={row} />
          </td>
        </tr>
      )}
    </>
  )
}

/**
 * The home page's market: every public group as one sortable row. Rows are ranked by health
 * first, then by the chosen key; each health tier gets a labelled divider so the order reads true.
 */
export function MarketBoard(props: {
  rows: readonly BoardRow[] | undefined
  query: string
  onClearQuery: () => void
  pending: boolean
  error: Error | null
  onRetry: () => void
  updatedAt: number
  footer: ReactNode
}) {
  const { t, locale } = useTranslation()
  const [sort, setSort] = useState<BoardSort>('multiplier')
  const [open, setOpen] = useState<string | null>(null)
  const rows = useMemo(
    () => sortRows(filterRows(props.rows ?? [], props.query), sort),
    [props.rows, props.query, sort],
  )
  const tiered = new Set(rows.map((row) => row.health)).size > 1
  return (
    <section className="board" aria-labelledby="board-title">
      <div className="board-bar">
        <h2 id="board-title">{t('公开分组')}</h2>
        <Segmented label="排序" value={sort} options={sortOptions} onChange={setSort} />
      </div>
      {props.error && (
        <div className="board-state">
          <ErrorMessage error={props.error} />
          <Button variant="secondary" onClick={props.onRetry}>
            {t('重试')}
          </Button>
        </div>
      )}
      {!props.error && (
        <div className="table-scroll board-table" aria-busy={props.pending || undefined}>
          <table>
            <caption className="sr-only">{t('公开分组的倍率、模型、探测延迟与验证状态')}</caption>
            <thead>
              <tr>
                <th scope="col">{t('分组')}</th>
                <th scope="col" className="numeric">
                  {t('倍率')}
                </th>
                <th scope="col" className="numeric hide-mobile">
                  {t('模型')}
                </th>
                <th scope="col" className="hide-mobile">
                  {t('厂商')}
                </th>
                <th scope="col" className="numeric">
                  {t('探测延迟')}
                </th>
                <th scope="col" className="hide-mobile">
                  {t('验证')}
                </th>
                <th scope="col" className="numeric hide-mobile">
                  {t('最近探测')}
                </th>
              </tr>
            </thead>
            <tbody>
              {props.pending &&
                Array.from({ length: 6 }, (_, index) => (
                  <tr key={index} className="board-skeleton" aria-hidden>
                    <td colSpan={COLUMNS}>
                      <span className="skeleton" />
                    </td>
                  </tr>
                ))}
              {!props.pending &&
                rows.map((row, index) => (
                  <Fragment key={row.id}>
                    {tiered && rows[index - 1]?.health !== row.health && (
                      <tr className="board-tier">
                        <th scope="rowgroup" colSpan={COLUMNS}>
                          {t(healthLabel[row.health])}
                        </th>
                      </tr>
                    )}
                    <BoardLine
                      row={row}
                      open={open === row.id}
                      onToggle={() => setOpen(open === row.id ? null : row.id)}
                    />
                  </Fragment>
                ))}
            </tbody>
          </table>
          {!props.pending && rows.length === 0 && (
            <EmptyState
              title={props.query ? '没有匹配的分组' : '暂无公开分组'}
              action={
                props.query ? (
                  <Button variant="secondary" size="sm" onClick={props.onClearQuery}>
                    {t('清除搜索')}
                  </Button>
                ) : undefined
              }
            />
          )}
        </div>
      )}
      <div className="board-footer">
        <p className="subtle board-fig" aria-live="polite">
          {props.updatedAt
            ? String(t('数据更新于')) +
              ' ' +
              String(new Date(props.updatedAt).toLocaleTimeString(locale))
            : t('加载中…')}
        </p>
        <div className="board-footer-actions">{props.footer}</div>
      </div>
    </section>
  )
}
