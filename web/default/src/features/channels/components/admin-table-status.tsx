import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'

/** Administrative lists share live totals and a retry action without clearing filters. */
export function AdminTableStatus(props: {
  title: string
  total?: number
  selected?: number
  loading?: boolean
  failed?: boolean
  fetching?: boolean
  onRetry: () => void
}) {
  const { t } = useTranslation()
  return (
    <>
      <header className='admin-record-meta'>
        <h2>{props.title}</h2>
        <div className='flex flex-wrap items-center gap-6'>
          <dl>
            <div>
              <dt>{t('Total')}</dt>
              <dd aria-live='polite'>
                {props.loading ? '—' : (props.total ?? '—')}
              </dd>
            </div>
            {Boolean(props.selected) && (
              <div>
                <dt>{t('Selected')}</dt>
                <dd>{props.selected}</dd>
              </div>
            )}
          </dl>
          <Button
            variant='outline'
            disabled={props.fetching}
            onClick={props.onRetry}
          >
            <RefreshCw aria-hidden='true' />
            {t('Refresh')}
          </Button>
        </div>
      </header>
      {props.failed && (
        <div role='alert' className='admin-query-error'>
          <span>{t('加载失败')}</span>
          <Button
            variant='outline'
            disabled={props.fetching}
            onClick={props.onRetry}
          >
            {t('重试')}
          </Button>
        </div>
      )}
    </>
  )
}
