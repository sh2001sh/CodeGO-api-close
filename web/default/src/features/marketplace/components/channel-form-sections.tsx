import type { UseFormReturn } from 'react-hook-form'
import interfaceI18n from '@/i18n/config'
import { Gauge, Link2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import type { ChannelFormInput } from '../lib/channel-form'
import { FormField, FormSection } from './channel-form-layout'

type ChannelForm = UseFormReturn<ChannelFormInput>

export function ChannelConnectionSection(props: {
  form: ChannelForm
  editing?: boolean
}) {
  const { t } = useTranslation()
  const { form } = props
  return (
    <FormSection icon={Link2} title={t('连接信息')}>
      {props.editing && (
        <div className='border-primary/25 bg-primary/[0.05] text-muted-foreground rounded-md border px-3 py-2 text-xs leading-5'>
          {t(
            '这是编辑模式：已有配置会保留。Base URL 和 API Key 留空即可保持原值，只填写需要修改的字段。'
          )}
        </div>
      )}
      <ConnectionEndpointFields form={form} editing={props.editing} />
      <ConnectionCredentialFields form={form} editing={props.editing} />
    </FormSection>
  )
}

function ConnectionEndpointFields(props: {
  form: ChannelForm
  editing?: boolean
}) {
  const { t } = useTranslation()
  return (
    <div className='grid gap-4 lg:grid-cols-2'>
      <FormField
        label={t('协议类型')}
        error={props.form.formState.errors.provider_type?.message}
      >
        <NativeSelect
          className='w-full'
          {...props.form.register('provider_type')}
        >
          <option value='openai_compatible'>
            {interfaceI18n.t('OpenAI Compatible')}
          </option>
          <option value='codex'>Codex</option>
          <option value='azure_openai'>Azure OpenAI</option>
          <option value='anthropic'>Anthropic / Claude</option>
          <option value='gemini'>Google Gemini</option>
        </NativeSelect>
      </FormField>
      <FormField
        label={t('Base URL')}
        error={props.form.formState.errors.base_url?.message}
      >
        <Input
          placeholder={
            props.editing
              ? t('留空则保持当前 Base URL')
              : 'https://api.example.com'
          }
          autoComplete='url'
          {...props.form.register('base_url')}
        />
      </FormField>
    </div>
  )
}

function ConnectionCredentialFields(props: {
  form: ChannelForm
  editing?: boolean
}) {
  const { t } = useTranslation()
  const sourceField = props.form.register('source_label')
  const sourceLabel = props.form.watch('source_label')
  return (
    <div className='grid gap-4 lg:grid-cols-2'>
      <FormField
        label={t('渠道主声明')}
        error={props.form.formState.errors.source_label?.message}
      >
        <Input
          placeholder={t('例如：Codex Pro 自营号池')}
          {...sourceField}
          onChange={(event) => {
            event.target.value = Array.from(event.target.value)
              .slice(0, 30)
              .join('')
            void sourceField.onChange(event)
          }}
        />
        <div className='text-muted-foreground flex items-start justify-between gap-3 text-xs leading-5'>
          <p>
            {t('由渠道主自行声明，最多 30 个字符，平台未核验来源。')}
            {t('不能填写链接、域名或 IP 地址。')}
          </p>
          <span className='shrink-0 tabular-nums'>
            {Array.from(sourceLabel).length}/30
          </span>
        </div>
      </FormField>
      <FormField
        label={t('API Key')}
        error={props.form.formState.errors.api_key?.message}
      >
        <Input
          type='password'
          autoComplete='new-password'
          placeholder={props.editing ? t('留空则保持当前 API Key') : 'sk-...'}
          {...props.form.register('api_key')}
        />
        <p className='text-muted-foreground text-xs leading-5'>
          {t('密钥提交后不会回显。')}
        </p>
      </FormField>
    </div>
  )
}

export function ChannelStrategySection(props: { form: ChannelForm }) {
  const { t } = useTranslation()
  const { form } = props
  return (
    <FormSection icon={Gauge} title={t('服务策略')}>
      <StrategyMetrics form={form} />
      <FormField
        label={t('维护窗口')}
        error={form.formState.errors.maintenance_window?.message}
      >
        <Input
          placeholder={t('例如：每周日 02:00-03:00 UTC+8')}
          {...form.register('maintenance_window')}
        />
      </FormField>
      <ChannelInterceptionPolicy form={form} />
      <ChannelSecurityAlertEmailPolicy form={form} />
      <ChannelMultiplierCardPolicy form={form} />
      <ChannelCreditPackPolicy form={form} />
      <ChannelAutoProbePolicy form={form} />
    </FormSection>
  )
}

function ChannelSecurityAlertEmailPolicy(props: { form: ChannelForm }) {
  const { t } = useTranslation()
  const enabled = props.form.watch('security_alert_email_enabled')
  return (
    <div className='flex min-h-16 items-center justify-between gap-4 rounded-md border px-3 py-2.5'>
      <label
        htmlFor='channel-security-alert-email-enabled'
        className='text-sm font-medium'
      >
        {t('安全风险邮件')}
      </label>
      <Switch
        id='channel-security-alert-email-enabled'
        checked={enabled}
        onCheckedChange={(checked) =>
          props.form.setValue('security_alert_email_enabled', checked, {
            shouldDirty: true,
          })
        }
      />
    </div>
  )
}

function ChannelMultiplierCardPolicy({ form }: { form: ChannelForm }) {
  const { t } = useTranslation()
  const enabled = form.watch('multiplier_card_user_enabled')
  return (
    <div className='flex items-center justify-between gap-4 rounded-md border p-3'>
      <div className='space-y-0.5'>
        <p className='text-sm font-medium'>{t('允许使用倍率卡')}</p>
        <p className='text-muted-foreground text-xs leading-5'>
          {enabled
            ? t('倍率卡请求可以路由到该渠道。')
            : t('倍率卡请求不会使用该渠道。')}
        </p>
      </div>
      <Switch
        checked={enabled}
        onCheckedChange={(checked) => {
          form.setValue('multiplier_card_user_enabled', checked, {
            shouldDirty: true,
          })
          form.setValue('multiplier_card_supported', checked, {
            shouldDirty: true,
          })
        }}
        aria-label={t('允许用户使用倍率卡')}
      />
    </div>
  )
}

function ChannelCreditPackPolicy(props: { form: ChannelForm }) {
  const { t } = useTranslation()
  const enabled = props.form.watch('credit_pack_supported')
  return (
    <div className='space-y-3 rounded-md border p-3'>
      <div className='flex items-center justify-between gap-4'>
        <label
          htmlFor='channel-credit-pack-supported'
          className='text-sm font-medium'
        >
          {t('接入新套餐')}
        </label>
        <Switch
          id='channel-credit-pack-supported'
          checked={enabled}
          onCheckedChange={(checked) =>
            props.form.setValue('credit_pack_supported', checked, {
              shouldDirty: true,
              shouldValidate: true,
            })
          }
        />
      </div>
      <dl className='grid gap-x-4 gap-y-2 text-sm sm:grid-cols-2'>
        <div className='flex items-baseline justify-between gap-3'>
          <dt className='text-muted-foreground'>{t('普通手续费')}</dt>
          <dd className='font-medium tabular-nums'>5%</dd>
        </div>
        <div className='flex items-baseline justify-between gap-3'>
          <dt className='text-muted-foreground'>{t('套餐最高费率')}</dt>
          <dd className='font-medium tabular-nums'>11%</dd>
        </div>
        <div className='flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 sm:col-span-2'>
          <dt className='text-muted-foreground'>{t('赠额成本分担')}</dt>
          <dd className='font-medium tabular-nums'>{t('套餐增额率 × 60%')}</dd>
        </div>
      </dl>
      <details className='bg-muted/50 rounded-xl p-4 text-sm'>
        <summary className='cursor-pointer font-medium'>
          {t('套餐增额分担怎么算')}
        </summary>
        <dl className='mt-4 grid gap-4 leading-7'>
          <div>
            <dt className='font-medium'>{t('增额成本')}</dt>
            <dd className='text-muted-foreground'>
              {t(
                '套餐增额率 = 套餐赠额 ÷ 套餐实付额度；附加手续费率 = 套餐增额率 × 60%；综合手续费率 = 5% + 附加手续费率。'
              )}
            </dd>
          </div>
          <div>
            <dt className='font-medium'>{t('渠道净收益')}</dt>
            <dd className='text-muted-foreground'>
              {t(
                '渠道净收益 = 结算毛额 − 5% 基础佣金 − 套餐增额分担费 − 邀请返利。增额分担费 = 结算毛额 × 套餐增额率 × 60%；邀请返利按合资格实付消费计算。'
              )}
            </dd>
          </div>
          <div>
            <dt className='font-medium'>{t('增额 3% 示例')}</dt>
            <dd className='text-muted-foreground'>
              {t(
                '套餐增额 3%，附加费率为 3% × 60% = 1.8%，综合手续费率为 6.8%。结算毛额 100 credits：基础佣金 5、增额分担费 1.8，邀请返利为 0 时，渠道净收益 93.2 credits。'
              )}
            </dd>
          </div>
          <div>
            <dt className='font-medium'>{t('各档费率')}</dt>
            <dd className='text-muted-foreground'>
              {t(
                '增额 5% / 6% / 7% / 8% / 10%，综合手续费率分别为 8% / 8.6% / 9.2% / 9.8% / 11%；邀请返利另计。实际金额按每笔最小计费单位四舍五入，历史日志按原结算快照展示。'
              )}
            </dd>
          </div>
          <div>
            <dt className='font-medium'>{t('接入范围')}</dt>
            <dd className='text-muted-foreground'>
              {t(
                '官方渠道默认支持套餐；市场渠道由渠道主开启新套餐接入。旧套餐转余额的集享增额由平台提供，按钱包结算，不收取新套餐增额分担。'
              )}
            </dd>
          </div>
        </dl>
      </details>
    </div>
  )
}

function ChannelInterceptionPolicy({ form }: { form: ChannelForm }) {
  const { t } = useTranslation()
  const enabled = form.watch('sensitive_word_interception_enabled')
  return (
    <div className='flex min-h-16 items-center justify-between gap-4 rounded-md border px-3 py-2.5'>
      <div className='space-y-0.5'>
        <p className='text-sm font-medium'>{t('敏感词拦截')}</p>
        <p className='text-muted-foreground text-xs leading-5'>
          {enabled
            ? t('请求会接入平台敏感词检测，命中规则时按平台策略拦截。')
            : t('请求不接入平台敏感词检测，由渠道主自行承担内容治理责任。')}
        </p>
      </div>
      <Switch
        checked={enabled}
        onCheckedChange={(checked) =>
          form.setValue('sensitive_word_interception_enabled', checked, {
            shouldDirty: true,
          })
        }
        aria-label={t('敏感词拦截')}
      />
    </div>
  )
}

function ChannelAutoProbePolicy({ form }: { form: ChannelForm }) {
  const { t } = useTranslation()
  const enabled = form.watch('auto_probe_enabled')
  const models = form.watch('declared_models')
  return (
    <div className='border-border grid gap-3 rounded-md border p-3 sm:grid-cols-[minmax(0,1fr)_130px_180px_auto] sm:items-end'>
      <div>
        <p className='text-sm font-medium'>{t('自动探针')}</p>
      </div>
      <FormField label={t('间隔（分钟）')}>
        <Input
          type='number'
          min='1'
          max='1440'
          disabled={!enabled}
          {...form.register('auto_probe_interval_minutes', {
            valueAsNumber: true,
          })}
        />
      </FormField>
      <FormField label={t('探针模型')}>
        <NativeSelect
          disabled={!enabled || models.length === 0}
          {...form.register('auto_probe_model')}
        >
          <option value=''>{t('选择模型')}</option>
          {models.map((model) => (
            <option key={model} value={model}>
              {model}
            </option>
          ))}
        </NativeSelect>
      </FormField>
      <Switch
        checked={enabled}
        onCheckedChange={(checked) => {
          form.setValue('auto_probe_enabled', checked, { shouldDirty: true })
          if (checked && !form.getValues('auto_probe_model') && models[0]) {
            form.setValue('auto_probe_model', models[0], { shouldDirty: true })
          }
        }}
        aria-label={t('自动探针')}
      />
    </div>
  )
}

function StrategyMetrics(props: { form: ChannelForm }) {
  const { t } = useTranslation()
  const { form } = props
  return (
    <div className='grid gap-4 sm:grid-cols-2 xl:grid-cols-3'>
      <FormField
        label={t('消费倍率')}
        error={form.formState.errors.multiplier?.message}
      >
        <Input
          type='number'
          step='any'
          max='1000000'
          inputMode='decimal'
          {...form.register('multiplier', { valueAsNumber: true })}
        />
      </FormField>
      <FormField
        label={t('渠道总并发（0 不限制）')}
        error={form.formState.errors.max_concurrency?.message}
      >
        <Input
          type='number'
          min='0'
          max='10000'
          {...form.register('max_concurrency', { valueAsNumber: true })}
        />
      </FormField>
      <FormField
        label={t('单用户并发（0 不限制）')}
        error={form.formState.errors.user_max_concurrency?.message}
      >
        <Input
          type='number'
          min='0'
          max='10000'
          {...form.register('user_max_concurrency', { valueAsNumber: true })}
        />
      </FormField>
      <FormField label='QPS' error={form.formState.errors.qps?.message}>
        <Input
          type='number'
          min='0.1'
          max='10000'
          step='0.1'
          {...form.register('qps', { valueAsNumber: true })}
        />
      </FormField>
      <FormField
        label={t('可见性')}
        error={form.formState.errors.visibility?.message}
      >
        <NativeSelect className='w-full' {...form.register('visibility')}>
          <option value='private'>{t('私有')}</option>
          <option value='unlisted'>{t('不公开列出')}</option>
          <option value='public'>{t('公开参与市场')}</option>
        </NativeSelect>
      </FormField>
    </div>
  )
}
