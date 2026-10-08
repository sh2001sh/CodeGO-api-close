import { useState } from 'react'
import { useMutation, useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { api, unwrap } from '../../lib/api'
import { resourceOptions, sessionOptions } from '../../lib/queries'
import { credits } from '../../lib/format'
import { useTranslation } from '../../lib/i18n'
import { Button, ErrorMessage, Loading, confirmAction } from '../../components/ui'
import type { BoxBatch, BoxBatchDraw, BoxBatchOverview } from './batch-contract'
import {
  batchRewardTotal,
  batchAverageCredits,
  canDrawBatch,
  entitlementCount,
  persistentBoxOperations,
  boxPlanSnapshot,
} from './batch-presentation'
import { date } from '../../lib/format'
import { weightPercentage } from './presentation'
import { BoxPlanSpecification } from './plan-specification'
import { BoxBatchSimulator } from './batch-simulator'

export const boxBatchesOptions = () =>
  resourceOptions<BoxBatchOverview>('box-batches', (signal) =>
    api.GET('/api/blind-box/batches', { signal }).then(unwrap),
  )

export function BoxBatchOffers({ pending: otherPending }: { pending: boolean }) {
  const { t } = useTranslation()
  const { data: user } = useSuspenseQuery(sessionOptions())
  const query = useQuery(boxBatchesOptions())
  const client = useQueryClient()
  const [result, setResult] = useState<BoxBatchDraw | null>(null)
  const [confirming, setConfirming] = useState(false)
  const [confirmedID, setConfirmedID] = useState('')
  const operations = () => persistentBoxOperations(String(user.id), sessionStorage)
  const draw = useMutation({
    mutationFn: (batch: BoxBatch): Promise<BoxBatchDraw> =>
      api
        .POST('/api/blind-box/batches/{id}/draw', {
          params: { path: { id: String(batch.id) } },
          body: { request_id: operations().forPayload(`batch:${batch.id}:1`), count: 1 },
        })
        .then(unwrap),
    onSuccess: (next, batch) => {
      setResult(next)
      operations().complete(`batch:${batch.id}:1`)
      void client.invalidateQueries({ queryKey: ['box-batches'] })
      void client.invalidateQueries({ queryKey: ['wallet'] })
      void client.invalidateQueries({ queryKey: ['boxes'] })
      void client.invalidateQueries({ queryKey: ['box-history'] })
    },
  })
  const pending = otherPending || draw.isPending || confirming
  const startDraw = async (batch: BoxBatch, trigger: HTMLButtonElement) => {
    if (pending) return
    draw.reset()
    if (batch.purpose === 'consumption' || operations().pending(`batch:${batch.id}:1`)) {
      setConfirmedID(String(batch.id))
      draw.mutate(batch)
      return
    }
    setConfirming(true)
    let accepted = false
    try {
      accepted = await confirmAction({
        title: `${t('确认购买并揭晓')} · ${credits(batch.price_micro)}`,
        description:
          batch.purpose === 'paid_random'
            ? t(
                '每盒随机获得一份奖励，奖励可能低于售价；仅限本人 API 消费，不能再买盲盒、转赠或退款。购买即揭晓。',
              )
            : `${t('确定获得')} ${credits(batch.base_credits_micro)}。${t('额度仅限本人 API 消费，不可转赠、退款或购买商品；附加奖励随机，购买即揭晓。')}`,
        confirmLabel: '购买并揭晓',
      })
      if (accepted) {
        setConfirmedID(String(batch.id))
        draw.mutate(batch)
      }
    } finally {
      setConfirming(false)
      if (!accepted) requestAnimationFrame(() => trigger.focus())
    }
  }
  const batches = query.data?.batches ?? []
  const entitlements = query.data?.entitlements ?? []
  return (
    <section className="section box-batch-offers" aria-labelledby="box-batches-heading">
      <div className="box-section-heading">
        <div>
          <h2 id="box-batches-heading">{t('盲盒奖池')}</h2>
          <p className="muted">
            {t('每批奖品数量固定，不放回抽取；初始概率和当前剩余概率公开展示。')}
          </p>
        </div>
        <Link to="/wallet" className="text-link">
          {t('普通充值')}
        </Link>
      </div>
      <ErrorMessage error={query.error ?? draw.error} />
      {(query.isError || draw.isError) && (
        <p className="muted">
          {t('超时或结果不明时请重试同一批次，刷新页面后仍沿用原请求编号，避免重复扣费。')}
        </p>
      )}
      {query.isError && (
        <Button variant="quiet" onClick={() => void query.refetch()}>
          {t('重试')}
        </Button>
      )}
      {query.isPending && <Loading />}
      {result && (
        <section className="box-open-result notice" role="status" aria-live="polite">
          <h3>{t('本次回馈结果')}</h3>
          {BigInt(result.base_credits_micro) > 0n && (
            <p>
              <strong>{t('确定消费额度')}</strong> · {credits(result.base_credits_micro)}
            </p>
          )}
          {result.records.map((record) => (
            <div key={String(record.id)}>
              <p>
                <strong>{record.reward.title}</strong>
                {record.reward.kind === 'credits' && <> · {credits(record.reward.amount_micro)}</>}
              </p>
              {BigInt(record.guarantee_credits_micro ?? 0) > 0n && (
                <p>
                  {t('保底补足')} · {credits(record.guarantee_credits_micro)}
                </p>
              )}
              {record.reward.kind === 'subscription' && (
                <>
                  <BoxPlanSpecification snapshot={boxPlanSnapshot(record.reward.plan_snapshot)} />
                  <p className="muted">
                    {t('套餐兑换卡已发放，请在下方道具区激活；有效期从激活成功起算。')}
                  </p>
                </>
              )}
            </div>
          ))}
          <p className="muted">
            {t('本次扣款')} · {credits(result.charged_micro)}。
            {(BigInt(result.base_credits_micro) > 0n ||
              result.records.some(
                (record) =>
                  record.reward.kind === 'credits' ||
                  BigInt(record.guarantee_credits_micro ?? 0) > 0n,
              )) &&
              t('消费额度已到账，仅限本人 API 消费。')}
          </p>
        </section>
      )}
      {!query.isPending && !query.isError && batches.length === 0 && (
        <div className="empty-state">
          <p>{t('暂无回馈批次')}</p>
          <p className="muted">{t('旧库存和道具继续在下方按原规则使用。')}</p>
        </div>
      )}
      {(['paid_random', 'consumption', 'credits'] as const).map((purpose) => {
        const matching = batches.filter((batch) => batch.purpose === purpose)
        if (!matching.length) return null
        return (
          <div key={purpose} className="box-batch-purpose">
            <h3>
              {t(
                purpose === 'paid_random'
                  ? '付费盲盒'
                  : purpose === 'consumption'
                    ? '消费回馈'
                    : '额度回馈',
              )}
            </h3>
            <p className="muted">
              {t(
                purpose === 'paid_random'
                  ? '每盒随机获得一份奖励，没有额外基础额度。奖励不可再买盲盒；每批按固定数量、不放回开奖。'
                  : purpose === 'consumption'
                    ? '凭符合规则的真实付费 API 消费资格免费领取；奖励消费不会重复产生资格。'
                    : '使用钱包购买确定消费额度，并获得一份随机附加奖励。',
              )}
            </p>
            {matching.map((batch) => {
              const available = entitlementCount(batch, entitlements)
              const recoverable = !!operations().pending(`batch:${batch.id}:1`)
              const dailyLimit = query.data?.daily_purchase_limit ?? 10
              const dailyPurchased = BigInt(query.data?.daily_purchased ?? 0)
              const quotaAvailable =
                purpose === 'consumption' || dailyPurchased < BigInt(dailyLimit)
              const allowed = (canDrawBatch(batch, available) && quotaAvailable) || recoverable
              const initialTotal = batchRewardTotal(batch.rewards, false)
              const remainingTotal = batchRewardTotal(batch.rewards, true)
              return (
                <article key={String(batch.id)} className="box-batch">
                  <div className="box-pool-layout">
                    <div className="box-pool-main">
                      <h4>{batch.name}</h4>
                      <p className="muted">
                        {t('批次')} <bdi>#{String(batch.id)}</bdi> · {t('剩余')}{' '}
                        {String(batch.remaining_count)} / {String(batch.total_count)}
                      </p>
                      <div className="box-batch-reward-head" aria-hidden="true">
                        <span>{t(purpose === 'paid_random' ? '奖励' : '附加奖励')}</span>
                        <span>{t('初始 / 当前概率')}</span>
                      </div>
                      <ul className="box-reward-list">
                        {batch.rewards.map((reward) => (
                          <li key={reward.id}>
                            <span className="box-reward-name">
                              <strong>{reward.title}</strong>
                              {reward.kind === 'credits' ? (
                                <span className="muted">{credits(reward.amount_micro)}</span>
                              ) : (
                                <BoxPlanSpecification snapshot={reward.plan_snapshot} />
                              )}
                              <span className="muted">
                                {t('剩余份数')} {String(reward.remaining)} /{' '}
                                {String(reward.quantity)}
                              </span>
                            </span>
                            <span className="box-probability" dir="ltr">
                              {weightPercentage(reward.quantity, initialTotal) ?? '—'} /{' '}
                              {remainingTotal === 0n
                                ? '0.00%'
                                : (weightPercentage(reward.remaining, remainingTotal) ?? '—')}
                            </span>
                          </li>
                        ))}
                      </ul>
                    </div>
                    <aside className="box-pool-order" aria-label={t('领取或购买信息')}>
                      <p className="muted">
                        {t(purpose === 'consumption' ? '免费领取' : '钱包扣款')}
                      </p>
                      <p className="box-price">{credits(batch.price_micro)}</p>
                      {purpose === 'paid_random' && (
                        <>
                          <p>
                            {t('基础奖池初始平均额度')} ·{' '}
                            {credits(batchAverageCredits(batch, false))}
                          </p>
                          <p>
                            {t('基础奖池当前平均额度')} ·{' '}
                            {credits(batchAverageCredits(batch, true))}
                          </p>
                          <p className="muted">
                            {t('上述概率与平均额度仅针对基础奖池；保底补足另计，不保证单次盈利。')}
                          </p>
                          {batch.pity_policy && (
                            <div className="box-guarantees">
                              <h4>{t('保底规则与进度')}</h4>
                              <p>
                                {t('小保底')} · {query.data?.pity?.small_progress ?? '—'} /{' '}
                                {batch.pity_policy.small_after}
                              </p>
                              <p>
                                {t('连续 {misses} 次低于 {amount}，下次不足时补足到该额度。', {
                                  misses: batch.pity_policy.small_after - 1,
                                  amount: credits(batch.pity_policy.small_minimum_micro),
                                })}
                              </p>
                              <p>
                                {t('大保底')} · {query.data?.pity?.big_progress ?? '—'} /{' '}
                                {batch.pity_policy.big_after}
                              </p>
                              <p>
                                {t('连续 {misses} 次低于 {amount}，下次不足时补足到该额度。', {
                                  misses: batch.pity_policy.big_after - 1,
                                  amount: credits(batch.pity_policy.big_minimum_micro),
                                })}
                              </p>
                              <p className="muted">
                                {t(
                                  '达到对应额度重置进度；大保底达标同时重置小保底。进度跨天、跨新版批次保留，套餐不按永久消费额度重置。',
                                )}
                              </p>
                            </div>
                          )}
                        </>
                      )}
                      {purpose === 'credits' && (
                        <p>
                          <strong>{t('确定消费额度')}</strong>
                          <br />
                          {credits(batch.base_credits_micro)}
                        </p>
                      )}
                      {purpose !== 'consumption' && (
                        <>
                          <p>
                            {t('今日已购')} · {dailyPurchased.toString()} / {dailyLimit}
                          </p>
                          <p className="muted">
                            {t('北京时间每日限购 10 个，跨付费奖池合并计数，次日零点恢复。')}
                          </p>
                        </>
                      )}
                      {purpose === 'consumption' && (
                        <>
                          <p className="muted">
                            {t(
                              '消费结算满 7 天后计算资格；未知成本、退款、混合来源和奖励消费不计入，活动最多使用剩余正贡献的 10%。',
                            )}
                          </p>
                          <p>
                            {t('可领取次数')} · {String(available)}
                          </p>
                          {entitlements
                            .filter((entry) => String(entry.batch_id) === String(batch.id))
                            .map((entry) => (
                              <p key={String(entry.id)} className="muted">
                                {t('资格来源：已结算的真实付费消费')} · {date(entry.created_at)}
                              </p>
                            ))}
                        </>
                      )}
                      <p className="muted">{t('仅限本人 API 消费；不可转赠、退款或购买商品。')}</p>
                      {batch.state === 'paused' && (
                        <p className="muted">
                          {t(
                            purpose === 'consumption' && available > 0n
                              ? '此批次已停售，已有资格仍可领取。'
                              : '此批次已停售。',
                          )}
                        </p>
                      )}
                      <Button
                        disabled={pending || !allowed}
                        loading={draw.isPending && confirmedID === String(batch.id)}
                        onClick={(event) => void startDraw(batch, event.currentTarget)}
                      >
                        {t(
                          recoverable
                            ? '恢复上次开奖'
                            : remainingTotal === 0n
                              ? '奖品已领完'
                              : purpose === 'consumption'
                                ? '领取并揭晓'
                                : !quotaAvailable
                                  ? '今日限购已满'
                                  : '购买并揭晓',
                        )}
                      </Button>
                      {purpose === 'paid_random' && <BoxBatchSimulator batch={batch} />}
                    </aside>
                  </div>
                </article>
              )
            })}
          </div>
        )
      })}
    </section>
  )
}
