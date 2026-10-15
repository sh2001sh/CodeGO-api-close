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
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { CodeGoLoader } from '@/components/codego-loader'

interface LoadingStateProps {
  className?: string
  message?: string
  size?: 'sm' | 'md' | 'lg'
  inline?: boolean
}

const sizeMap = {
  sm: 'h-3.5',
  md: 'h-5',
  lg: 'h-7',
} as const

export function LoadingState(props: LoadingStateProps) {
  const { t } = useTranslation()
  const loaderSize = sizeMap[props.size ?? 'md']

  if (props.inline) {
    return (
      <span className={cn('inline-flex items-center gap-2.5', props.className)}>
        <CodeGoLoader className={loaderSize} />
        {props.message != null && (
          <span className='text-muted-foreground text-sm'>{props.message}</span>
        )}
      </span>
    )
  }

  return (
    <div
      className={cn(
        'flex min-h-[200px] flex-col items-center justify-center gap-4',
        props.className
      )}
    >
      <CodeGoLoader className='h-6' />
      <p className='text-muted-foreground text-sm'>
        {props.message ?? t('Loading...')}
      </p>
    </div>
  )
}
