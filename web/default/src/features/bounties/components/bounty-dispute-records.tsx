import { useTranslation } from 'react-i18next'
import { formatBountyDate } from '../lib/bounty-format'
import type { BountyDetail } from '../types'

export function BountyDisputeRecords(props: {
  disputes: BountyDetail['disputes']
}) {
  const { t } = useTranslation()
  return (
    <section className='border-border/70 bg-card/50 space-y-4 rounded-xl border p-4 sm:p-5'>
      <h2 className='text-base font-semibold'>{t('Dispute records')}</h2>
      {props.disputes.map((dispute) => (
        <article
          key={dispute.dispute_id}
          className='border-border/60 space-y-3 rounded-lg border p-3'
        >
          <div className='flex items-center justify-between gap-3'>
            <span
              className={`bounty-status ${dispute.status === 'resolved' ? 'bounty-status-success' : 'bounty-status-danger'}`}
            >
              {dispute.status === 'resolved'
                ? t('Resolved')
                : t('Open dispute status')}
            </span>
            <span className='text-muted-foreground text-xs'>
              {formatBountyDate(dispute.created_at)}
            </span>
          </div>
          <p className='text-sm leading-6 whitespace-pre-wrap'>
            {dispute.reason}
          </p>
          {dispute.ai_analysis ? (
            <div className='bg-muted/35 space-y-2 rounded-lg p-3 text-sm'>
              <div className='font-medium'>
                {t('Evidence analysis suggestion')}
              </div>
              <p className='text-muted-foreground leading-6'>
                {dispute.ai_analysis.final_requirement_summary}
              </p>
              {dispute.ai_analysis.missing_evidence?.length ? (
                <div className='text-warning'>
                  {t('Missing evidence')}:{' '}
                  {dispute.ai_analysis.missing_evidence.join('、')}
                </div>
              ) : null}
              <div className='text-muted-foreground text-xs'>
                {t(
                  'AI is advisory only; an administrator makes the final quota decision.'
                )}
              </div>
            </div>
          ) : null}
        </article>
      ))}
    </section>
  )
}
