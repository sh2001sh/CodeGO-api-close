import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { credits } from '../../lib/format'
import { useTranslation } from '../../lib/i18n'
import { Button, ErrorMessage } from '../../components/ui'
import type { BoxBatch, BoxBatchDraw } from './batch-contract'

export function BoxBatchSimulator({ batch }: { batch: BoxBatch }) {
  const { t } = useTranslation()
  const [count, setCount] = useState(10)
  const simulation = useMutation({
    mutationFn: (): Promise<BoxBatchDraw> =>
      api
        .POST('/api/blind-box/batches/{id}/simulate', {
          params: { path: { id: String(batch.id) } },
          body: { count },
        })
        .then(unwrap),
  })
  const result = simulation.data
  const rewardTotal = result?.records.reduce(
    (total, record) =>
      total +
      (record.reward.kind === 'credits' ? BigInt(record.reward.amount_micro) : 0n) +
      BigInt(record.guarantee_credits_micro ?? 0),
    0n,
  )
  return (
    <details className="box-guarantees box-batch-simulator">
      <summary>{t('模拟抽盒')}</summary>
      <p className="muted">
        {t('复制当前剩余奖池和本人保底进度进行模拟；不扣款、不占库存或限购次数，不改变正式保底。')}
      </p>
      <label className="field" htmlFor={`simulate-count-${batch.id}`}>
        <span>{t('模拟次数')}</span>
        <select
          id={`simulate-count-${batch.id}`}
          value={count}
          disabled={simulation.isPending}
          onChange={(event) => {
            setCount(Number(event.target.value))
            simulation.reset()
          }}
        >
          {[1, 10, 50, 100].map((value) => (
            <option key={value} value={value}>
              {value}
            </option>
          ))}
        </select>
      </label>
      <Button
        variant="quiet"
        loading={simulation.isPending}
        disabled={simulation.isPending || BigInt(batch.remaining_count) <= 0n}
        onClick={() => simulation.mutate()}
      >
        {t('开始模拟')}
      </Button>
      <ErrorMessage error={simulation.error} />
      {result && (
        <section role="status" aria-live="polite" className="box-simulation-result">
          <h4>{t('模拟结果（未到账）')}</h4>
          <p>
            {t('模拟购买金额')} ·{' '}
            {credits(BigInt(batch.price_micro) * BigInt(result.records.length))}
          </p>
          <p>
            {t('额度奖励合计')} · {credits(rewardTotal)}
          </p>
          <p>
            {t('实际扣款')} · {credits(result.charged_micro)}
          </p>
          <p className="muted">
            {t('每次模拟重新复制当前状态；模拟结果不预测或保证正式开奖。套餐奖励另按规格展示。')}
          </p>
          <details>
            <summary>{t('逐次模拟结果')}</summary>
            <ol>
              {result.records.map((record, index) => (
                <li key={index}>
                  {record.reward.title}
                  {BigInt(record.guarantee_credits_micro ?? 0) > 0n && (
                    <>
                      {' '}
                      · {t('保底补足')} {credits(record.guarantee_credits_micro)}
                    </>
                  )}
                </li>
              ))}
            </ol>
          </details>
        </section>
      )}
    </details>
  )
}
