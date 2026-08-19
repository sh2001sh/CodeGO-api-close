import { useMemo, useState } from 'react'
import type { UseFormReturn } from 'react-hook-form'
import {
  Bot,
  GitCompareArrows,
  Loader2,
  Plus,
  RefreshCcw,
  Search,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import type { ChannelFormInput } from '../lib/channel-form'
import { modelKey, type ChannelModelSyncDiff } from '../lib/channel-model-sync'
import { FormSection } from './channel-form-layout'
import { ChannelModelPrices } from './channel-model-prices'

type ChannelForm = UseFormReturn<ChannelFormInput>

export function ChannelModelsSection(props: {
  form: ChannelForm
  availableModels: string[]
  selectedModels: string[]
  manualModel: string
  syncDiff: ChannelModelSyncDiff | null
  fetching: boolean
  onManualModelChange: (value: string) => void
  onFetch: () => void
  onAddManual: () => void
  onToggle: (model: string, checked: boolean) => void
  onApplySync: (mode: 'replace' | 'merge') => void
}) {
  const { t } = useTranslation()
  return (
    <FormSection
      icon={Bot}
      title={t('模型能力')}
      description={t('从上游同步，或补充需要发布的模型。')}
    >
      <ModelToolbar {...props} />
      {props.syncDiff && <ModelSyncSummary {...props} diff={props.syncDiff} />}
      {props.form.formState.errors.declared_models?.message && (
        <p className='text-destructive text-xs' role='alert'>
          {props.form.formState.errors.declared_models.message}
        </p>
      )}
      <ModelList {...props} />
      <ChannelModelPrices
        form={props.form}
        selectedModels={props.selectedModels}
      />
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
        {t('同步上游模型')}
      </Button>
      <div className='flex min-w-0 flex-1 gap-2'>
        <Input
          value={props.manualModel}
          onChange={(event) => props.onManualModelChange(event.target.value)}
          onKeyDown={(event) => {
            if (event.key !== 'Enter') return
            event.preventDefault()
            props.onAddManual()
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

function ModelSyncSummary(props: {
  diff: ChannelModelSyncDiff
  selectedModels: string[]
  onApplySync: (mode: 'replace' | 'merge') => void
}) {
  const { t } = useTranslation()
  const selectedKeys = new Set(props.selectedModels.map(modelKey))
  const merging = props.diff.removedModels.some((model) =>
    selectedKeys.has(modelKey(model))
  )
  return (
    <div className='border-border bg-muted/35 flex flex-col gap-3 rounded-md border px-3 py-3 lg:flex-row lg:items-center lg:justify-between'>
      <div className='min-w-0'>
        <p className='flex items-center gap-2 text-sm font-medium'>
          <GitCompareArrows className='size-4' />
          {t('上游模型差异')}
        </p>
        <p className='text-muted-foreground mt-1 text-xs leading-5'>
          {t('新增 {{added}} · 保留 {{retained}} · 上游未返回 {{removed}}', {
            added: props.diff.addedModels.length,
            retained: props.diff.retainedModels.length,
            removed: props.diff.removedModels.length,
          })}
        </p>
      </div>
      <div className='flex flex-wrap gap-2'>
        <Button
          type='button'
          size='sm'
          variant={merging ? 'outline' : 'default'}
          onClick={() => props.onApplySync('replace')}
        >
          {t('按上游覆盖')}
        </Button>
        <Button
          type='button'
          size='sm'
          variant={merging ? 'default' : 'outline'}
          onClick={() => props.onApplySync('merge')}
          disabled={props.diff.removedModels.length === 0}
        >
          {t('合并保留原模型')}
        </Button>
      </div>
    </div>
  )
}

function ModelList(props: {
  availableModels: string[]
  selectedModels: string[]
  syncDiff: ChannelModelSyncDiff | null
  onToggle: (model: string, checked: boolean) => void
}) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const groups = useMemo(
    () => buildModelGroups(props.availableModels, props.syncDiff, search),
    [props.availableModels, props.syncDiff, search]
  )
  if (props.availableModels.length === 0) return <EmptyModelList />
  return (
    <div className='border-border overflow-hidden rounded-md border'>
      <div className='border-border relative border-b p-2'>
        <Search className='text-muted-foreground absolute top-1/2 left-5 size-4 -translate-y-1/2' />
        <Input
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          className='pl-9'
          placeholder={t('搜索模型')}
        />
      </div>
      <div className='max-h-72 overflow-y-auto p-2'>
        {groups.map(({ key, ...group }) => (
          <ModelGroup
            key={key}
            {...group}
            selectedModels={props.selectedModels}
            onToggle={props.onToggle}
          />
        ))}
        {groups.every((group) => group.models.length === 0) && (
          <p className='text-muted-foreground px-3 py-6 text-center text-sm'>
            {t('没有匹配的模型')}
          </p>
        )}
      </div>
    </div>
  )
}

function ModelGroup(props: {
  label: string
  tone: 'normal' | 'added' | 'removed'
  models: string[]
  selectedModels: string[]
  onToggle: (model: string, checked: boolean) => void
}) {
  const { t } = useTranslation()
  if (props.models.length === 0) return null
  const selectedKeys = new Set(props.selectedModels.map(modelKey))
  return (
    <section className='mb-2 last:mb-0'>
      <div className='text-muted-foreground flex items-center justify-between px-2 py-1.5 text-xs'>
        <span>{t(props.label)}</span>
        <span className='tabular-nums'>{props.models.length}</span>
      </div>
      <div className='grid gap-1 sm:grid-cols-2 xl:grid-cols-3'>
        {props.models.map((model) => (
          <ModelOption
            key={model}
            model={model}
            tone={props.tone}
            checked={selectedKeys.has(modelKey(model))}
            onToggle={props.onToggle}
          />
        ))}
      </div>
    </section>
  )
}

function ModelOption(props: {
  model: string
  tone: 'normal' | 'added' | 'removed'
  checked: boolean
  onToggle: (model: string, checked: boolean) => void
}) {
  const { t } = useTranslation()
  const status =
    props.tone === 'added'
      ? t('新增')
      : props.tone === 'removed'
        ? t('未返回')
        : ''
  return (
    <label className='hover:bg-muted flex min-w-0 cursor-pointer items-center gap-2 rounded-sm px-2 py-2 text-sm'>
      <Checkbox
        checked={props.checked}
        onCheckedChange={(checked) =>
          props.onToggle(props.model, checked === true)
        }
      />
      <span className='min-w-0 flex-1 truncate' title={props.model}>
        {props.model}
      </span>
      {status && (
        <span
          className={
            props.tone === 'removed'
              ? 'text-amber-700 dark:text-amber-400'
              : 'text-emerald-700 dark:text-emerald-400'
          }
        >
          {status}
        </span>
      )}
    </label>
  )
}

function EmptyModelList() {
  const { t } = useTranslation()
  return (
    <div className='border-border text-muted-foreground rounded-md border border-dashed px-4 py-6 text-center text-sm'>
      {t('模型列表将在这里显示')}
    </div>
  )
}

function buildModelGroups(
  models: string[],
  diff: ChannelModelSyncDiff | null,
  search: string
) {
  const query = search.trim().toLowerCase()
  const visible = (values: string[]) =>
    values.filter((model) => !query || model.toLowerCase().includes(query))
  if (!diff)
    return [
      {
        key: 'models',
        label: '已选择与可用模型',
        tone: 'normal' as const,
        models: visible(models),
      },
    ]
  const addedKeys = new Set(diff.addedModels.map(modelKey))
  const removedKeys = new Set(diff.removedModels.map(modelKey))
  return [
    {
      key: 'added',
      label: '上游新增',
      tone: 'added' as const,
      models: visible(models.filter((model) => addedKeys.has(modelKey(model)))),
    },
    {
      key: 'available',
      label: '上游可用',
      tone: 'normal' as const,
      models: visible(
        models.filter(
          (model) =>
            !addedKeys.has(modelKey(model)) && !removedKeys.has(modelKey(model))
        )
      ),
    },
    {
      key: 'removed',
      label: '上游未返回（勾选可保留）',
      tone: 'removed' as const,
      models: visible(
        models.filter((model) => removedKeys.has(modelKey(model)))
      ),
    },
  ]
}
