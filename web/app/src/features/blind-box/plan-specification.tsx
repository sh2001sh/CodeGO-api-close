import type { PlanSnapshot } from './batch-contract'
import { credits } from '../../lib/format'
import { useTranslation } from '../../lib/i18n'

export function BoxPlanSpecification({ snapshot }: { snapshot?: PlanSnapshot | null }) {
  const { t } = useTranslation()
  if (!snapshot) return <span className="muted">{t('套餐规格在发布时冻结，领取后查看。')}</span>
  const seconds = BigInt(snapshot.period_seconds ?? 0)
  const days = seconds / 86400n
  return (
    <div className="box-plan-specification">
      <span>
        {credits(snapshot.credits)} ·{' '}
        {days > 0n && seconds % 86400n === 0n ? `${days} ${t('天')}` : `${seconds} ${t('秒')}`}
      </span>
      <span className="muted">{t('激活后开始计时，固定额度不能刷新。')}</span>
      {snapshot.model_limits && Object.keys(snapshot.model_limits).length > 0 ? (
        <details>
          <summary>{t('查看适用模型')}</summary>
          <ul>
            {Object.entries(snapshot.model_limits).map(([model, limit]) => (
              <li key={model}>
                <bdi>{model}</bdi> · {credits(limit)}
              </li>
            ))}
          </ul>
        </details>
      ) : (
        <span className="muted">{t('模型范围按冻结套餐规则执行。')}</span>
      )}
    </div>
  )
}
