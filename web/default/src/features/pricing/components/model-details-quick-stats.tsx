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
import {
  CalendarClock,
  FileText,
  Layers,
  Maximize2,
  Sparkles,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import {
  formatTokenCount,
  formatYearMonth,
  type ModelMetadata,
} from '../lib/model-metadata'
import type { Modality } from '../types'
import { ModalityIcons } from './model-details-modalities'

type QuickStatsProps = {
  metadata: ModelMetadata
}

type Stat = {
  key: string
  icon: React.ComponentType<{ className?: string }>
  label: string
  value: React.ReactNode
  hint?: string
}

function buildStats(
  metadata: ModelMetadata,
  t: (key: string) => string
): Stat[] {
  const stats: Stat[] = [
    {
      key: 'context',
      icon: Layers,
      label: t('Context'),
      value:
        metadata.context_length > 0
          ? formatTokenCount(metadata.context_length)
          : t('待核验'),
    },
  ]

  stats.push({
    key: 'max-output',
    icon: Maximize2,
    label: t('Max output'),
    value:
      metadata.max_output_tokens > 0
        ? formatTokenCount(metadata.max_output_tokens)
        : t('待核验'),
  })

  stats.push({
    key: 'modalities',
    icon: FileText,
    label: t('Modalities'),
    value: (
      <ModalityFlow
        input={metadata.input_modalities}
        output={metadata.output_modalities}
      />
    ),
  })

  stats.push({
    key: 'knowledge',
    icon: Sparkles,
    label: t('Knowledge cutoff'),
    value: metadata.knowledge_cutoff
      ? formatYearMonth(metadata.knowledge_cutoff)
      : t('待核验'),
  })

  stats.push({
    key: 'release',
    icon: CalendarClock,
    label: t('Released'),
    value: metadata.release_date
      ? formatYearMonth(metadata.release_date)
      : t('待核验'),
  })

  return stats
}

function ModalityFlow(props: { input: Modality[]; output: Modality[] }) {
  return (
    <span className='inline-flex items-center gap-1 align-middle'>
      <ModalityIcons modalities={props.input} className='size-3.5' />
      <span className='text-muted-foreground/40'>→</span>
      <ModalityIcons modalities={props.output} className='size-3.5' />
    </span>
  )
}

export function ModelDetailsQuickStats(props: QuickStatsProps) {
  const { t } = useTranslation()
  const stats = buildStats(props.metadata, t)

  return (
    <div className='bg-muted/20 grid grid-cols-2 gap-px overflow-hidden rounded-lg border @md/details:grid-cols-3 @2xl/details:grid-cols-5'>
      {stats.map((stat) => {
        const Icon = stat.icon
        return (
          <div
            key={stat.key}
            className={cn(
              'bg-background flex min-w-0 flex-col gap-0.5 px-3 py-2.5'
            )}
          >
            <span className='text-muted-foreground inline-flex min-w-0 items-center gap-1 text-[10px] font-medium tracking-wider uppercase'>
              <Icon className='size-3 shrink-0' />
              <span className='truncate'>{stat.label}</span>
            </span>
            <span className='text-foreground truncate text-sm font-semibold tabular-nums'>
              {stat.value}
            </span>
          </div>
        )
      })}
    </div>
  )
}
