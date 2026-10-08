import { useTranslation } from '../lib/i18n'
import type { Schema } from '../lib/types'
import { credits } from '../lib/format'
import { weightPercentage } from './blind-box/presentation'

export function BoxGuarantees(props: {
  pool: Schema['MarketplacePool']
  state?: Schema['MarketplacePityState']
}) {
  const { t } = useTranslation()
  const rules = props.pool.guarantees
  const standard = props.pool.standard_policy
  if (standard?.enabled)
    return (
      <div className="box-guarantees">
        <h4>{t('保底规则与进度')}</h4>
        {standard.pity_after > 0 && (
          <p>
            {t('保底进度')} {props.state?.small_progress ?? 0} / {standard.pity_after}
            {t('，基础额度分支保底')} {credits(standard.pity_minimum_micro)}
          </p>
        )}
        <details>
          <summary>{t('查看条件与奖励分支')}</summary>
          <p className="muted">
            {t('套餐奖励分支先于基础额度分支判断；首购和保底金额不是所有奖励的统一最低价值。')}
          </p>
          {BigInt(standard.first_purchase_minimum_micro) > 0n && (
            <p>
              {t('本人首次付费购买并开启时，基础额度奖励至少')}{' '}
              {credits(standard.first_purchase_minimum_micro)}
              {t('；旧 Claude 额度奖励的首购门槛为上述金额的四分之一。')}
            </p>
          )}
          {standard.pity_after > 0 && (
            <p>
              {t('基础额度达到高价值门槛或获得套餐奖励时重置进度。高价值门槛为')}{' '}
              {credits(standard.low_reward_threshold_micro)}
              {t('；旧 Claude 额度按原门槛计算。')}
            </p>
          )}
          {BigInt(standard.subscription_probability_ppb) > 0n && (
            <p>
              {t('套餐奖励分支概率')}{' '}
              {weightPercentage(standard.subscription_probability_ppb, 1_000_000_000, 5)}
              {t('，命中后重置保底进度。')}
            </p>
          )}
          <p className="muted">
            {t('赠送和免费发放的盲盒不触发本人付费首购权益；零时卡按原独立规则判断。')}
          </p>
          <p className="muted">{t('零时卡命中也会重置保底进度。')}</p>
        </details>
      </div>
    )
  if (!rules || (!rules.first?.length && !rules.small_after && !rules.big_after))
    return <p className="box-guarantees muted">{t('此奖池未配置额外保底。')}</p>
  return (
    <div className="box-guarantees">
      <h4>{t('保底规则与进度')}</h4>
      {rules.small_after ? (
        <p>
          {t('小保底')} {props.state?.small_progress ?? 0} / {rules.small_after}
        </p>
      ) : null}
      {rules.big_after ? (
        <p>
          {t('大保底')} {props.state?.big_progress ?? 0} / {rules.big_after}
        </p>
      ) : null}
      <details>
        <summary>{t('查看条件与奖励分支')}</summary>
        {rules.first?.length ? (
          <p>
            {t('首抽保底：')}
            {rules.first.map((reward) => reward.title).join('、')}
          </p>
        ) : null}
        {rules.small_after ? (
          <p>
            {t('小保底奖池：')}
            {rules.small?.map((reward) => reward.title).join('、')}
            {t('；额度达到')} {credits(rules.small_reset_micro)} {t('时重置。')}
          </p>
        ) : null}
        {rules.big_after ? (
          <p>
            {t('大保底奖池：')}
            {rules.big?.map((reward) => reward.title).join('、')}
            {t('；额度达到')} {credits(rules.big_reset_micro)} {t('时重置。')}
          </p>
        ) : null}
        <p className="muted">{t('保底会切换至对应奖池；重置门槛不代表保底奖池的统一最低奖励。')}</p>
      </details>
    </div>
  )
}
