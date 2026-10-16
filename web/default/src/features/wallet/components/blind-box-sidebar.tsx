import { ArrowRight, History, Ticket } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import type { BlindBoxProp, BlindBoxRecord, BlindBoxStatistics } from '../types'
import { formatBlindBoxTimestamp } from './blind-box-dialog-data'
import { BlindBoxStatsPanel } from './blind-box-notices'

export function BlindBoxSidebar(props: {
  quota: number
  availableBoxes: number
  pendingBoxes: number
  records: BlindBoxRecord[]
  props: BlindBoxProp[]
  statistics?: BlindBoxStatistics
  onOpenHistory: () => void
  onOpenProps: () => void
}) {
  const { t } = useTranslation()
  return (
    <aside className='commerce-blind-box-sidebar'>
      <PropsPreview props={props.props} onOpenProps={props.onOpenProps} />
      <section className='blind-box-sidebar-section'>
        <div className='blind-box-section-heading'>
          <h2>
            <History className='size-4' aria-hidden />
            {t('开奖历史')}
          </h2>
          <Button
            variant='ghost'
            size='icon'
            aria-label={t('查看最近 30 天记录')}
            onClick={props.onOpenHistory}
          >
            <ArrowRight className='size-4' aria-hidden />
          </Button>
        </div>
        {props.records.length > 0 ? (
          <ul className='blind-box-recent-records'>
            {props.records.slice(0, 3).map((record) => (
              <li key={record.id}>
                <span>{record.reward_title}</span>
                <time>{formatBlindBoxTimestamp(record.create_time)}</time>
              </li>
            ))}
          </ul>
        ) : (
          <p className='blind-box-empty'>{t('暂无记录')}</p>
        )}
      </section>
      {props.statistics ? (
        <BlindBoxStatsPanel statistics={props.statistics} />
      ) : null}
    </aside>
  )
}

const PROP_STATUS_LABEL: Record<string, string> = {
  available: '可使用',
  active: '生效中',
  reserved: '已锁定',
  used: '已使用',
  expired: '已过期',
  paused: '已暂停',
}

function PropsPreview(props: {
  props: BlindBoxProp[]
  onOpenProps: () => void
}) {
  const { t } = useTranslation()
  const usable = props.props.filter((prop) =>
    ['available', 'active', 'paused'].includes(prop.status)
  )
  const preview = (usable.length > 0 ? usable : props.props).slice(0, 3)
  return (
    <section className='blind-box-sidebar-section blind-box-props-preview'>
      <div className='blind-box-section-heading'>
        <h2>
          <Ticket className='size-4' aria-hidden />
          {t('我的道具')}
        </h2>
        <span className='blind-box-prop-count tabular-nums'>
          {usable.length}
        </span>
      </div>
      {preview.length > 0 ? (
        <ul className='blind-box-prop-list'>
          {preview.map((prop) => (
            <li key={prop.id}>
              <span>{prop.title}</span>
              <span
                className={
                  prop.status === 'active'
                    ? 'text-success'
                    : 'text-muted-foreground'
                }
              >
                {t(PROP_STATUS_LABEL[prop.status] || prop.status)}
              </span>
            </li>
          ))}
        </ul>
      ) : (
        <p className='blind-box-empty'>{t('暂无道具')}</p>
      )}
      <Button
        variant='ghost'
        className='blind-box-manage-props'
        onClick={props.onOpenProps}
      >
        {t('管理全部道具')}
        <ArrowRight className='size-4' aria-hidden />
      </Button>
    </section>
  )
}
