import { useTranslation } from '../lib/i18n'
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { date } from '../lib/format'
import { Button, ErrorMessage, Loading, PageHeader } from '../components/ui'
import { DataTable } from '../components/data-table'
import { LegacyResetOpportunity } from '../features/commerce/legacy-reset-opportunity'
import { ResetCards } from '../features/commerce/reset-cards'
import { ConsumptionReferralRewards } from '../features/commerce/consumption-referral-rewards'

export default function ReferralRewardsPage() {
  const { t } = useTranslation()
  const [copyError, setCopyError] = useState<Error | null>(null)
  const [copied, setCopied] = useState(false)
  const overview = useQuery(
    resourceOptions('referral-rewards', (signal) =>
      api.GET('/api/user/aff/rewards', { signal }).then(unwrap),
    ),
  )
  const link = overview.data?.affiliate_code
    ? `${window.location.origin}/sign-up?ref=${encodeURIComponent(overview.data.affiliate_code)}`
    : ''
  return (
    <>
      <PageHeader title="邀请奖励" />
      <p className="muted">
        {t(
          '邀请奖励以订单确认的条款为准。新活动以实际付费消费奖励替代刷新，已有次数和原订单承诺保留。',
        )}
      </p>
      <ErrorMessage error={copyError ?? overview.error} />
      {overview.isPending && <Loading />}
      {overview.data && (
        <>
          <dl className="metrics">
            <div>
              <dt>{t('已邀请人数')}</dt>
              <dd>{String(overview.data.invited_count)}</dd>
            </div>
            <div>
              <dt>{t('历史首购人数')}</dt>
              <dd>{String(overview.data.successful_purchase_invites)}</dd>
            </div>
            <div>
              <dt>{t('邀请码')}</dt>
              <dd>{overview.data.affiliate_code || t('尚未生成')}</dd>
            </div>
          </dl>
          {link && (
            <div className="form-stack">
              <label className="field" htmlFor="referral-link">
                <span>{t('邀请链接')}</span>
                <input id="referral-link" readOnly value={link} />
              </label>
              <Button
                variant="quiet"
                onClick={async () => {
                  setCopyError(null)
                  try {
                    await navigator.clipboard.writeText(link)
                    setCopied(true)
                  } catch {
                    setCopyError(new Error('无法复制，请从邀请链接文本框手动复制'))
                  }
                }}
              >
                {copied ? t('已复制') : t('复制邀请链接')}
              </Button>
            </div>
          )}
          <section className="section">
            <h2>{t('历史邀请记录')}</h2>
            <DataTable
              rows={overview.data.invitees}
              rowKey={(item) => item.invitee_id}
              empty="暂无邀请记录。"
              columns={[
                {
                  label: '受邀用户',
                  render: (item) =>
                    item.invitee_display_name ||
                    item.invitee_username ||
                    t('用户 ') + String(item.invitee_id),
                },
                { label: '注册时间', render: (item) => date(Number(item.created_at) * 1000) },
                {
                  label: '历史月卡首购',
                  render: (item) => (item.month_card_purchased ? t('已购买') : t('未购买')),
                },
                {
                  label: '刷新奖励',
                  render: (item) => (item.reset_opportunity_earned ? t('已发放') : t('未发放')),
                },
              ]}
            />
          </section>
        </>
      )}
      <LegacyResetOpportunity />
      <ConsumptionReferralRewards />
      <ResetCards />
    </>
  )
}
