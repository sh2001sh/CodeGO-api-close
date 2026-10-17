import { useTranslation } from 'react-i18next'

export function CreditPackRules() {
  const { t } = useTranslation()

  return (
    <dl className='grid gap-x-10 gap-y-5 text-sm leading-6 md:grid-cols-2'>
      <div>
        <dt className='text-foreground font-semibold'>{t('永久额度包')}</dt>
        <dd className='text-muted-foreground mt-1'>
          {t(
            '新额度包永久有效、无周期重置，按钱包同价扣费；仅适用于已接入的渠道。'
          )}
        </dd>
      </div>
      <div>
        <dt className='text-foreground font-semibold'>{t('实付额度与赠额')}</dt>
        <dd className='text-muted-foreground mt-1'>
          {t(
            '到账额度已包含赠额，具体金额以当前套餐和支付确认页为准。每次购买生成独立额度包，不延长或重置已有权益。'
          )}
        </dd>
      </div>
      <div>
        <dt className='text-foreground font-semibold'>{t('适用渠道')}</dt>
        <dd className='text-muted-foreground mt-1'>
          {t(
            '官方渠道默认接入；市场渠道由渠道主选择是否接入。额度包与通用钱包独立记账，按钱包中的扣费顺序设置使用。'
          )}
        </dd>
      </div>
      <div>
        <dt className='text-foreground font-semibold'>{t('历史权益')}</dt>
        <dd className='text-muted-foreground mt-1'>
          {t(
            '旧套餐已停止新售，已购权益保留原有效期、倍率与扣费规则。可用刷新次数可用于符合条件的旧套餐刷新（每月最多 1 次），或每 1 次兑换 1 份当前最低档新套餐；两种操作共用次数，成功后各扣除 1 次。'
          )}
        </dd>
      </div>
    </dl>
  )
}
