import { Link } from '@tanstack/react-router'
import { getIntlLocale } from '@/i18n/format'
import { ArrowUpRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  formatDuration,
  formatMultiplier,
} from '@/features/marketplace/lib/format'
import {
  localizedGroupName,
  localizedSourceLabel,
} from '@/features/marketplace/lib/localized-group-name'
import type { MarketplaceGroup } from '@/features/marketplace/types'

export function RankingsTable(props: { groups: MarketplaceGroup[] }) {
  const { t } = useTranslation()

  return (
    <div className='rankings-table-scroll'>
      <table className='rankings-table'>
        <caption className='sr-only'>{t('rankings.tableCaption')}</caption>
        <thead>
          <tr>
            <th scope='col'>{t('rankings.rank')}</th>
            <th scope='col'>{t('rankings.channel')}</th>
            <th scope='col'>{t('rankings.score')}</th>
            <th scope='col'>{t('rankings.successRate')}</th>
            <th scope='col'>{t('rankings.avgFirstToken')}</th>
            <th scope='col'>{t('rankings.requests')}</th>
            <th scope='col'>{t('rankings.multiplier')}</th>
          </tr>
        </thead>
        <tbody>
          {props.groups.map((group) => (
            <RankingRow key={group.id} group={group} />
          ))}
        </tbody>
      </table>
    </div>
  )
}

function RankingRow(props: { group: MarketplaceGroup }) {
  const { t, i18n } = useTranslation()
  const group = props.group
  const hasTraffic = group.request_count > 0
  const isRanked = hasTraffic && !group.observing && group.rank > 0
  const name = group.custom_name
    ? `${group.channel_id}-${group.custom_name}`
    : localizedGroupName(
        group.system_display_name,
        group.source_label,
        i18n.language
      )
  const source = group.source_label
    ? localizedSourceLabel(group.source_label, i18n.language)
    : t('rankings.marketplaceSource')
  const number = new Intl.NumberFormat(getIntlLocale(), {
    maximumFractionDigits: 2,
  })
  let state = t('rankings.noRequests')
  if (hasTraffic) {
    state = group.observing ? t('rankings.observing') : source
  }

  return (
    <tr data-leading={isRanked && group.rank === 1}>
      <td className='rankings-position'>
        <span aria-label={isRanked ? undefined : state}>
          {isRanked ? String(group.rank).padStart(2, '0') : '—'}
        </span>
      </td>
      <th scope='row' className='rankings-identity'>
        <Link
          to='/market'
          search={() => ({
            search:
              Number(group.channel_id) ||
              group.channel_id ||
              group.system_display_name,
            window_hours: 24,
          })}
        >
          <span>{name}</span>
          <ArrowUpRight size={15} aria-hidden='true' />
        </Link>
        <span className='rankings-channel-meta'>
          {state}
          <span aria-hidden='true'> / </span>
          {t('rankings.modelCount', {
            count: group.models.length,
            formattedCount: number.format(group.models.length),
          })}
        </span>
      </th>
      <td className='rankings-score'>
        {isRanked ? number.format(group.score) : '—'}
      </td>
      <td>{hasTraffic ? `${number.format(group.success_rate)}%` : '—'}</td>
      <td>{hasTraffic ? formatDuration(group.avg_ttft_ms) : '—'}</td>
      <td>{number.format(group.request_count)}</td>
      <td>{formatMultiplier(group.multiplier)}×</td>
    </tr>
  )
}
