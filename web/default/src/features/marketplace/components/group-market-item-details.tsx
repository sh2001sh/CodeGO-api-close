import { useState } from 'react'
import { FileText } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import type { MarketplaceGroup } from '../types'
import {
  ChannelFeedbackButton,
  ChannelFeedbackSummary,
} from './channel-feedback'
import { GroupDetails } from './group-details'
import {
  GroupModelResults,
  GroupModelVerificationReport,
} from './group-model-verification'
import { TokenBindPanel } from './token-bind-panel'

export function GroupMarketItemDetails(props: { group: MarketplaceGroup }) {
  const { t } = useTranslation()
  const [reportOpen, setReportOpen] = useState(false)
  const reportID = `model-report-${props.group.id}`

  return (
    <div className='border-border bg-muted/15 border-t'>
      <GroupDetails group={props.group} />
      <div className='border-border border-t px-4 py-4 sm:px-5'>
        <div className='flex flex-wrap items-start justify-between gap-3'>
          <h5 className='text-sm font-semibold'>{t('可用模型')}</h5>
          <Button
            variant='outline'
            size='sm'
            onClick={() => setReportOpen((current) => !current)}
            aria-expanded={reportOpen}
            aria-controls={reportID}
          >
            <FileText />
            {reportOpen ? t('收起报告') : t('检测报告')}
          </Button>
        </div>
        <GroupModelResults group={props.group} />
        <div className='mt-4'>
          <TokenBindPanel groupId={props.group.id} compact />
        </div>
        <div className='mt-3 flex flex-wrap items-center justify-between gap-2'>
          <ChannelFeedbackSummary group={props.group} />
          <ChannelFeedbackButton group={props.group} />
        </div>
      </div>
      {reportOpen && (
        <div id={reportID}>
          <GroupModelVerificationReport group={props.group} />
        </div>
      )}
    </div>
  )
}
