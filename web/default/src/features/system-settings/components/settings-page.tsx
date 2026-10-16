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
import { useParams } from '@tanstack/react-router'
import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { useSystemOptions, getOptionValue } from '../hooks/use-system-options'
import '../settings.css'

type SettingsPageProps<
  TSettings extends Record<string, string | number | boolean | unknown[]>,
  TSectionId extends string,
> = {
  routePath: string
  defaultSettings: TSettings
  defaultSection: TSectionId
  getSectionContent: (
    sectionId: TSectionId,
    settings: TSettings,
    ...extraArgs: unknown[]
  ) => React.ReactNode
  extraArgs?: unknown[]
}

/**
 * Generic settings page component
 * Handles loading state, data fetching, and section rendering
 */
export function SettingsPage<
  TSettings extends Record<string, string | number | boolean | unknown[]>,
  TSectionId extends string,
>({
  routePath,
  defaultSettings,
  defaultSection,
  getSectionContent,
  extraArgs = [],
}: SettingsPageProps<TSettings, TSectionId>) {
  const { t } = useTranslation()
  const { data, isLoading, isError, refetch, isFetching } = useSystemOptions()
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const params = useParams({ from: routePath as any })

  if (isLoading) {
    return (
      <div
        className='settings-workspace settings-loading'
        role='status'
        aria-busy='true'
        aria-label={t('Loading settings...')}
      >
        <div className='settings-skeleton settings-skeleton-title' />
        {Array.from({ length: 4 }, (_, index) => (
          <div key={index} className='settings-skeleton-row'>
            <div className='settings-skeleton settings-skeleton-label' />
            <div className='settings-skeleton settings-skeleton-input' />
          </div>
        ))}
      </div>
    )
  }

  if ((isError && !data) || data?.success === false) {
    return (
      <div className='settings-workspace settings-error' role='alert'>
        <h1>{t('Failed to load settings')}</h1>
        <Button
          variant='outline'
          onClick={() => void refetch()}
          disabled={isFetching}
        >
          <RefreshCw aria-hidden='true' />
          {t('Retry')}
        </Button>
      </div>
    )
  }

  const settings = getOptionValue(data?.data, defaultSettings) as TSettings
  const activeSection = (params?.section ?? defaultSection) as TSectionId
  const sectionContent = getSectionContent(
    activeSection,
    settings,
    ...extraArgs
  )

  return (
    <div
      className='settings-workspace'
      data-settings-section={activeSection}
      key={activeSection}
    >
      {isError && (
        <div className='settings-error' role='alert'>
          <span>{t('Failed to load settings')}</span>
          <Button
            variant='outline'
            onClick={() => void refetch()}
            disabled={isFetching}
          >
            <RefreshCw aria-hidden='true' />
            {t('Retry')}
          </Button>
        </div>
      )}
      <div className='settings-workspace-content'>{sectionContent}</div>
    </div>
  )
}
