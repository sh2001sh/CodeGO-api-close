import type { UseFormReturn } from 'react-hook-form'
import { Gauge, Link2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import {
  MARKETPLACE_SOURCE_OPTIONS,
  type ChannelFormInput,
} from '../lib/channel-form'
import { FormField, FormSection } from './channel-form-layout'

type ChannelForm = UseFormReturn<ChannelFormInput>

export function ChannelConnectionSection(props: {
  form: ChannelForm
  editing?: boolean
}) {
  const { t } = useTranslation()
  const { form } = props
  return (
    <FormSection
      icon={Link2}
      title={t('连接信息')}
      description={t('用于安全检测和读取上游模型。')}
    >
      {props.editing && (
        <div className='border-primary/25 bg-primary/[0.05] text-muted-foreground rounded-md border px-3 py-2 text-xs leading-5'>
          {t('这是编辑模式：已有配置会保留。Base URL 和 API Key 留空即可保持原值，只填写需要修改的字段。')}
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
          <option value='openai_compatible'>OpenAI Compatible</option>
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
  return (
    <div className='grid gap-4 lg:grid-cols-2'>
      <FormField
        label={t('上游来源')}
        error={props.form.formState.errors.source_label?.message}
      >
        <NativeSelect
          className='w-full'
          {...props.form.register('source_label')}
        >
          {MARKETPLACE_SOURCE_OPTIONS.map((source) => (
            <option key={source} value={source}>
              {source}
            </option>
          ))}
        </NativeSelect>
        <p className='text-muted-foreground text-xs leading-5'>
          {t('固定来源无需人工审核，检测通过后直接展示。')}
        </p>
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
    <FormSection
      icon={Gauge}
      title={t('服务策略')}
      description={t('设置价格、容量和市场可见范围。')}
    >
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
      <ChannelAutoProbePolicy form={form} />
    </FormSection>
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
        <p className='text-muted-foreground mt-1 text-xs leading-5'>
          {t('按间隔测试指定模型，并将最新结果同步到市场健康状态。')}
        </p>
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
