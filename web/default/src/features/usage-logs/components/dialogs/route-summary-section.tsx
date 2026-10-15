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
import { Route } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Label } from '@/components/ui/label'
import { StatusBadge } from '@/components/status-badge'
import type { LogOtherData } from '../../types'

type RouteSummary = NonNullable<LogOtherData['route_summary']>

const skipReasonLabels: Record<string, string> = {
  unavailable: '路由当前不可用',
  capacity_limit: '路由容量已满',
  credential_cooling: '渠道凭据冷却中',
  failed_route: '已失败路由被跳过',
  selection_error: '路由选择异常',
  setup_error: '路由初始化失败',
  stream_circuit: '流式请求保护熔断',
}

export function RouteSummarySection(props: {
  summary: RouteSummary
  groupName?: string
  failed: boolean
}) {
  const { t } = useTranslation()
  const summary = props.summary
  const selectedOrder = summary.selected_order ?? 0
  const outcome = props.failed
    ? t('请求最终失败')
    : selectedOrder > 0
      ? t('路由已生效')
      : t('未命中可用路由')

  return (
    <section className='min-w-0 space-y-1.5'>
      <Label className='flex items-center gap-1.5 text-xs font-semibold'>
        <Route className='size-3.5' aria-hidden='true' />
        {t('Auto 路由情况')}
      </Label>
      <div className='bg-muted/30 min-w-0 space-y-2 rounded-md border p-2.5 max-sm:p-2'>
        <div className='flex flex-wrap items-center gap-2'>
          <StatusBadge
            label={outcome}
            variant={
              props.failed ? 'red' : selectedOrder > 0 ? 'green' : 'yellow'
            }
            size='sm'
            copyable={false}
          />
          {selectedOrder > 0 && (
            <span className='text-muted-foreground text-xs tabular-nums'>
              {t('第 {{order}} / {{total}} 路', {
                order: selectedOrder,
                total: Math.max(summary.candidate_count, selectedOrder),
              })}
            </span>
          )}
        </div>
        <div className='grid gap-1.5 text-xs sm:grid-cols-2'>
          <RouteDetail
            label={t('最终分组')}
            value={props.groupName || t('未命中')}
          />
          <RouteDetail
            label={t('候选路由')}
            value={String(summary.candidate_count)}
          />
          <RouteDetail
            label={t('跳过路由')}
            value={String(summary.skipped_count ?? 0)}
          />
          <RouteDetail
            label={t('回退 / 重试')}
            value={`${summary.fallback ? t('是') : t('否')} / ${summary.retry_count ?? 0}`}
          />
        </div>
        {summary.skip_reasons && summary.skip_reasons.length > 0 && (
          <div className='border-border flex flex-wrap gap-1.5 border-t pt-2'>
            {summary.skip_reasons.map((reason) => (
              <span
                key={reason}
                className='bg-background text-muted-foreground rounded border px-1.5 py-0.5 text-[11px]'
              >
                {t(skipReasonLabels[reason] || reason)}
              </span>
            ))}
          </div>
        )}
      </div>
    </section>
  )
}

function RouteDetail(props: { label: string; value: string }) {
  return (
    <div className='grid grid-cols-[5rem_minmax(0,1fr)] gap-2'>
      <span className='text-muted-foreground'>{props.label}</span>
      <span className='min-w-0 font-medium break-words'>{props.value}</span>
    </div>
  )
}
