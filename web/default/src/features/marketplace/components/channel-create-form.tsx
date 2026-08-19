import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { useMarketplaceChannelUpdate, useMarketplaceMutations } from '../hooks'
import {
  channelEditFormSchema,
  channelFormDefaults,
  channelFormDefaultsForEdit,
  channelFormSchema,
  type ChannelFormInput,
} from '../lib/channel-form'
import {
  buildChannelModelSyncDiff,
  mergeChannelModels,
  modelKey,
  normalizeChannelModels,
  reconcileAutoProbeModel,
  reconcileChannelModelPrices,
  type ChannelModelSyncDiff,
} from '../lib/channel-model-sync'
import type { MarketplaceChannel } from '../types'
import { ChannelConsistencySection } from './channel-consistency-section'
import {
  ChannelConnectionSection,
  ChannelStrategySection,
} from './channel-form-sections'
import { ChannelModelsSection } from './channel-models-section'

export function ChannelCreateForm(props: { onCreated: () => void }) {
  return <ChannelEditorForm onSaved={props.onCreated} />
}

export function ChannelEditorForm(props: {
  channel?: MarketplaceChannel
  admin?: boolean
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const mutations = useMarketplaceMutations()
  const update = useMarketplaceChannelUpdate(props.admin === true)
  const editing = props.channel != null
  const initialValues = editing
    ? channelFormDefaultsForEdit(props.channel!)
    : channelFormDefaults
  const [availableModels, setAvailableModels] = useState<string[]>(
    initialValues.declared_models
  )
  const [manualModel, setManualModel] = useState('')
  const [modelSyncDiff, setModelSyncDiff] =
    useState<ChannelModelSyncDiff | null>(null)
  const form = useForm<ChannelFormInput>({
    resolver: zodResolver(editing ? channelEditFormSchema : channelFormSchema),
    defaultValues: initialValues,
  })
  const selectedModels = form.watch('declared_models')

  useEffect(() => {
    const values = props.channel
      ? channelFormDefaultsForEdit(props.channel)
      : channelFormDefaults
    form.reset(values)
    setAvailableModels(values.declared_models)
    setModelSyncDiff(null)
  }, [form, props.channel])

  const applySelectedModels = (models: string[]) => {
    const next = normalizeChannelModels(models)
    form.setValue('declared_models', next, {
      shouldDirty: true,
      shouldValidate: true,
    })
    form.setValue(
      'model_prices',
      reconcileChannelModelPrices(form.getValues('model_prices'), next),
      { shouldDirty: true, shouldValidate: true }
    )
    form.setValue(
      'auto_probe_model',
      reconcileAutoProbeModel(
        form.getValues('auto_probe_model'),
        next,
        form.getValues('auto_probe_enabled')
      ),
      { shouldDirty: true, shouldValidate: true }
    )
  }

  const fetchModels = async () => {
    const valid = await form.trigger(['provider_type', 'base_url', 'api_key'])
    if (!valid) return
    try {
      const models = await mutations.fetchModels.mutateAsync(form.getValues())
      const diff = buildChannelModelSyncDiff(
        form.getValues('declared_models'),
        models
      )
      setModelSyncDiff(diff)
      setAvailableModels(
        mergeChannelModels(diff.upstreamModels, diff.removedModels)
      )
      applySelectedModels(diff.upstreamModels)
      toast.success(
        t('已同步 {{count}} 个模型，并按上游列表完成覆盖', {
          count: diff.upstreamModels.length,
        })
      )
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('获取模型失败'))
    }
  }

  const toggleModel = (model: string, checked: boolean) => {
    const targetKey = modelKey(model)
    const next = checked
      ? normalizeChannelModels([...selectedModels, model])
      : selectedModels.filter((item) => modelKey(item) !== targetKey)
    applySelectedModels(next)
  }

  const addManualModel = () => {
    const model = manualModel.trim()
    if (!model) return
    setAvailableModels((current) => normalizeChannelModels([...current, model]))
    applySelectedModels([...selectedModels, model])
    setManualModel('')
  }

  const applySyncMode = (mode: 'replace' | 'merge') => {
    if (!modelSyncDiff) return
    applySelectedModels(
      mode === 'replace'
        ? modelSyncDiff.upstreamModels
        : mergeChannelModels(
            modelSyncDiff.upstreamModels,
            modelSyncDiff.removedModels
          )
    )
  }

  const submit = form.handleSubmit(async (values) => {
    try {
      if (props.channel) {
        await update.mutateAsync({
          id: props.channel.id,
          values: {
            provider_type: values.provider_type,
            source_label: values.source_label,
            declared_models: values.declared_models,
            model_prices: values.model_prices,
            multiplier: values.multiplier,
            visibility: values.visibility,
            max_concurrency: values.max_concurrency,
            user_max_concurrency: values.user_max_concurrency,
            qps: values.qps,
            maintenance_window: values.maintenance_window,
            sensitive_word_interception_enabled:
              values.sensitive_word_interception_enabled,
            auto_probe_enabled: values.auto_probe_enabled,
            auto_probe_interval_minutes: values.auto_probe_interval_minutes,
            auto_probe_model: values.auto_probe_model,
            ...(values.base_url ? { base_url: values.base_url } : {}),
            ...(values.api_key ? { api_key: values.api_key } : {}),
            ...(props.admin
              ? {
                  model_consistency_status:
                    values.model_consistency_status === 'none'
                      ? ''
                      : values.model_consistency_status,
                }
              : {}),
          },
        })
        toast.success(t('渠道已更新；渠道信息变化时需重新完成对应检测或测试'))
      } else {
        await mutations.create.mutateAsync(values)
        toast.success(t('渠道已提交，必做检测或测试通过后将自动上架'))
        form.reset(channelFormDefaults)
        setAvailableModels([])
        setModelSyncDiff(null)
      }
      props.onSaved()
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('保存失败'))
    }
  })

  const pending = editing ? update.isPending : mutations.create.isPending
  return (
    <form onSubmit={submit}>
      <ChannelConnectionSection form={form} editing={editing} />
      <ChannelModelsSection
        form={form}
        availableModels={availableModels}
        selectedModels={selectedModels}
        manualModel={manualModel}
        syncDiff={modelSyncDiff}
        fetching={mutations.fetchModels.isPending}
        onManualModelChange={setManualModel}
        onFetch={() => void fetchModels()}
        onAddManual={addManualModel}
        onToggle={toggleModel}
        onApplySync={applySyncMode}
      />
      <ChannelStrategySection form={form} />
      {props.admin && <ChannelConsistencySection form={form} />}
      <div className='border-border bg-muted/15 flex justify-end border-t px-4 py-4 sm:px-5'>
        <Button type='submit' disabled={pending}>
          {pending && <Loader2 className='animate-spin' />}
          {editing ? t('保存修改') : t('提交并执行必做校验')}
        </Button>
      </div>
    </form>
  )
}
