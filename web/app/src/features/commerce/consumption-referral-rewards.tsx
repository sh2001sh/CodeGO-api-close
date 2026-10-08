import { useTranslation } from '../../lib/i18n'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { api, unwrap } from '../../lib/api'
import { credits, date } from '../../lib/format'
import { Button, ErrorMessage, Loading } from '../../components/ui'
import { DataTable } from '../../components/data-table'
import { ReferralTerms } from './referral-order-terms'

export function ConsumptionReferralRewards() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const rewards = useQuery({
    queryKey: ['referral-consumption-rewards'],
    enabled: open,
    queryFn: ({ signal }) => api.GET('/api/user/aff/consumption-rewards', { signal }).then(unwrap),
  })
  return (
    <section className="section">
      <div className="page-header">
        <h2>{t('邀请消费奖励')}</h2>
        <Button
          variant="quiet"
          disabled={rewards.isFetching}
          onClick={() => {
            if (open) void rewards.refetch()
            else setOpen(true)
          }}
        >
          {open ? t('刷新消费奖励') : t('查看消费奖励')}
        </Button>
      </div>
      <p className="muted">
        {t(
          '以首笔合格订单确认的规则核算实际付费消费，成本未知时等待核对。奖励仅本人消费，不能转赠、提现或退款；套餐转换、刷新换卡、赠送及奖励消费不重复计奖。',
        )}
      </p>
      <ErrorMessage error={rewards.error} />
      {open && rewards.isPending && <Loading />}
      {rewards.data && (
        <>
          {BigInt(rewards.data.refund_offset_credits) > 0n && (
            <p>
              {t('历史退款待抵扣奖励')} {credits(rewards.data.refund_offset_credits)}
              {t('，从未来奖励冲抵，不扣充值本金。')}
            </p>
          )}
          <DataTable
            rows={rewards.data.records}
            rowKey={(row) => row.id}
            empty="暂无新邀请消费奖励资格。"
            columns={[
              { label: '受邀用户 / 订单', render: (row) => `${row.invitee_id} / ${row.order_id}` },
              { label: '活动条款', render: (row) => <ReferralTerms terms={row.terms} /> },
              { label: '消费窗口截止', render: (row) => date(row.window_until) },
              { label: '奖励上限', render: (row) => credits(row.max_reward_credits) },
              { label: '已发奖励', render: (row) => credits(row.paid_credits) },
              { label: '退款冲减', render: (row) => credits(row.refunded_reward_credits) },
              {
                label: '状态',
                render: (row) =>
                  t(
                    {
                      reserved: '等待付款',
                      active: '消费计奖中',
                      completed: '窗口结束，继续核对迟到消费',
                      needs_review: '待核对',
                      released: '订单资格已释放',
                      refunded: '订单已退款',
                    }[row.state] ?? row.state,
                  ),
              },
            ]}
          />
          <p className="muted">
            {t('最近100条资格记录；奖励上限为最高可得金额，不代表已赚取或保证发放。')}
          </p>
        </>
      )}
    </section>
  )
}
