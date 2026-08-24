/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Skeleton } from '@/components/ui/skeleton'
import type { MarketplaceGroup } from '../types'

/**
 * 质量排行榜：紧凑榜单视图。
 * 与「市场分组」的卡片目录不同，这里用一屏可对比的数据行呈现
 * 统一口径的质量评分、成功率、首字延迟、请求量与倍率。
 */
export function RankingLeaderboard(props: {
  groups: MarketplaceGroup[]
  loading: boolean
  error: boolean
  onRetry: () => void
}) {
  const { t } = useTranslation()

  if (props.loading) return <LeaderboardSkeleton />
  if (props.error) {
    return (
      <div className='border-border text-muted-foreground flex min-h-48 items-center justify-center rounded-lg border border-dashed text-sm'>
        {t('无法加载榜单，')}{' '}
        <button className='text-primary underline' onClick={props.onRetry}>
          {t('重试')}
        </button>
      </div>
    )
  }
  if (props.groups.length === 0) {
    return (
      <div className='border-border text-muted-foreground flex min-h-48 items-center justify-center rounded-lg border border-dashed text-sm'>
        {t('暂无达到排名门槛的分组。')}
      </div>
    )
  }

  const ranked = props.groups
  const maxScore = Math.max(...ranked.map((g) => g.score ?? 0), 1)

  return (
    <section className='border-border bg-card overflow-hidden rounded-lg border'>
      <div className='border-border bg-primary/[0.04] flex items-center gap-2 border-b px-4 py-2.5 text-xs'>
        <ShieldCheck className='text-primary size-3.5 shrink-0' />
        <span className='text-foreground leading-5'>
          {t(
            'Wilson 可靠性修正 + 首字延迟 / 总延迟 / 吞吐 / 倍率加权；观测中的分组不计入名次。操作入口在「市场分组」。'
          )}
        </span>
      </div>
      <div className='overflow-x-auto'>
        <table className='w-full min-w-[880px] text-sm'>
          <thead className='text-muted-foreground border-border border-b text-xs'>
            <tr className='bg-muted/25'>
              <th className='w-14 px-3 py-2.5 text-left font-medium'>
                {t('名次')}
              </th>
              <th className='px-3 py-2.5 text-left font-medium'>
                {t('分组')}
              </th>
              <th className='w-40 px-3 py-2.5 text-left font-medium'>
                {t('质量评分')}
              </th>
              <th className='w-20 px-3 py-2.5 text-right font-medium'>
                {t('成功率')}
              </th>
              <th className='w-24 px-3 py-2.5 text-right font-medium'>
                {t('首字 P50')}
              </th>
              <th className='w-24 px-3 py-2.5 text-right font-medium'>
                {t('请求量')}
              </th>
              <th className='w-16 px-3 py-2.5 text-right font-medium'>
                {t('倍率')}
              </th>
            </tr>
          </thead>
          <tbody>
            {ranked.map((group, index) => (
              <LeaderboardRow
                key={group.id}
                rank={group.observing ? null : index + 1}
                group={group}
                maxScore={maxScore}
              />
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}

function LeaderboardRow(props: {
  rank: number | null
  group: MarketplaceGroup
  maxScore: number
}) {
  const g = props.group
  const score = g.score ?? 0
  const scoreWidth = Math.max(3, (score / props.maxScore) * 100)
  return (
    <tr className='border-border/60 hover:bg-muted/20 border-b transition-colors last:border-b-0'>
      <td className='px-3 py-2.5'>
        <span
          className={
            props.rank && props.rank <= 3
              ? 'text-primary app-numeric inline-flex size-6 items-center justify-center rounded-[4px] border border-primary/30 bg-primary/10 text-xs font-semibold'
              : 'text-muted-foreground app-numeric text-xs tabular-nums'
          }
        >
          {props.rank ? `#${props.rank}` : '–'}
        </span>
      </td>
      <td className='max-w-72 px-3 py-2.5'>
        <div className='truncate font-medium' title={g.system_display_name}>
          {g.system_display_name}
        </div>
        <div className='text-muted-foreground mt-0.5 truncate text-xs'>
          {g.source_label}
          {g.models.length > 0 && ` · ${g.models.slice(0, 3).join(' / ')}`}
        </div>
      </td>
      <td className='px-3 py-2.5'>
        <div className='flex items-center gap-2'>
          <span className='bg-border/40 relative block h-1.5 w-24 overflow-hidden rounded-sm'>
            <span
              className='absolute inset-y-0 left-0 rounded-sm'
              style={{
                width: `${scoreWidth}%`,
                background:
                  props.rank && props.rank <= 3
                    ? 'var(--primary)'
                    : 'color-mix(in oklab, var(--primary) 45%, var(--border))',
              }}
            />
          </span>
          <span className='app-numeric text-xs font-semibold tabular-nums'>
            {score.toFixed(1)}
          </span>
        </div>
      </td>
      <td className='app-numeric px-3 py-2.5 text-right text-xs tabular-nums'>
        {g.wilson_success_rate != null
          ? `${g.wilson_success_rate.toFixed(2)}%`
          : '--'}
      </td>
      <td className='text-muted-foreground app-numeric px-3 py-2.5 text-right text-xs tabular-nums'>
        {g.attempt_ttft_p50_ms ? `${Math.round(g.attempt_ttft_p50_ms)}ms` : '--'}
      </td>
      <td className='text-muted-foreground app-numeric px-3 py-2.5 text-right text-xs tabular-nums'>
        {g.request_count != null ? g.request_count.toLocaleString() : '--'}
      </td>
      <td className='app-numeric px-3 py-2.5 text-right text-xs font-medium tabular-nums'>
        {g.multiplier.toFixed(2)}x
      </td>
    </tr>
  )
}

function LeaderboardSkeleton() {
  return (
    <div className='space-y-1.5 p-4'>
      {Array.from({ length: 7 }).map((_, index) => (
        <Skeleton key={index} className='h-11 w-full rounded-md' />
      ))}
    </div>
  )
}
