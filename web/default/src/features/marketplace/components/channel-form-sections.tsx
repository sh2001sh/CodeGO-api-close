import type { UseFormReturn } from 'react-hook-form'
import { Bot, Gauge, Link2, Loader2, Plus, RefreshCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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

export function ChannelModelsSection(props: {
  form: ChannelForm
  availableModels: string[]
  selectedModels: string[]
  manualModel: string
  fetching: boolean
  onManualModelChange: (value: string) => void
  onFetch: () => void
  onAddManual: () => void
  onToggle: (model: string, checked: boolean) => void
}) {
  const { t } = useTranslation()
  return (
    <FormSection
      icon={Bot}
      title={t('模型能力')}
      description={t('从上游同步，或补充需要发布的模型。')}
    >
      <ModelToolbar {...props} />
      {props.form.formState.errors.declared_models?.message && (
        <p className='text-destructive text-xs' role='alert'>
          {props.form.formState.errors.declared_models.message}
        </p>
      )}
      <ModelList {...props} />
    </FormSection>
  )
}

function ModelToolbar(props: {
  manualModel: string
  fetching: boolean
  onManualModelChange: (value: string) => void
  onFetch: () => void
  onAddManual: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className='flex flex-col gap-2 sm:flex-row'>
      <Button
        type='button'
        variant='outline'
        onClick={props.onFetch}
        disabled={props.fetching}
      >
        {props.fetching ? <Loader2 className='animate-spin' /> : <RefreshCcw />}
        {t('获取模型列表')}
      </Button>
      <div className='flex min-w-0 flex-1 gap-2'>
        <Input
          value={props.manualModel}
          onChange={(event) => props.onManualModelChange(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault()
              props.onAddManual()
            }
          }}
          placeholder={t('输入模型名称，例如 gpt-4.1')}
        />
        <Button
          type='button'
          variant='outline'
          size='icon'
          title={t('添加模型')}
          onClick={props.onAddManual}
        >
          <Plus />
        </Button>
      </div>
    </div>
  )
}

function ModelList(props: {
  availableModels: string[]
  selectedModels: string[]
  onToggle: (model: string, checked: boolean) => void
}) {
  const { t } = useTranslation()
  if (props.availableModels.length === 0) {
    return (
      <div className='border-border text-muted-foreground rounded-md border border-dashed px-4 py-6 text-center text-sm'>
        {t('模型列表将在这里显示')}
      </div>
    )
  }
  return (
    <div className='border-border max-h-56 overflow-y-auto rounded-md border p-2'>
      <div className='grid gap-1 sm:grid-cols-2 xl:grid-cols-3'>
        {props.availableModels.map((model) => (
          <label
            key={model}
            className='hover:bg-muted flex min-w-0 cursor-pointer items-center gap-2 rounded-sm px-2 py-2 text-sm'
          >
            <Checkbox
              checked={props.selectedModels.includes(model)}
              onCheckedChange={(checked) =>
                props.onToggle(model, checked === true)
              }
            />
            <span className='truncate' title={model}>
              {model}
            </span>
          </label>
        ))}
      </div>
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

function StrategyMetrics(props: { form: ChannelForm }) {
  const { t } = useTranslation()
  const { form } = props
  return (
    <div className='grid gap-4 sm:grid-cols-2 xl:grid-cols-4'>
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
        label={t('最大并发')}
        error={form.formState.errors.max_concurrency?.message}
      >
        <Input
          type='number'
          min='1'
          max='10000'
          {...form.register('max_concurrency', { valueAsNumber: true })}
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
