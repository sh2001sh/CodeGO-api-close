import { useEffect, useState } from 'react'
import dayjs from 'dayjs'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import i18n from '@/i18n/config'
import {
  AlertCircle,
  Clock3,
  Download,
  History,
  ImagePlus,
  Images,
  PenSquare,
  RefreshCw,
  Sparkles,
  WandSparkles,
} from 'lucide-react'
import { nanoid } from 'nanoid'
import { useTranslation, Trans } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { ImageDialog } from '@/features/usage-logs/components/dialogs/image-dialog'
import {
  fetchImageWorkspaceSourceFile,
  getImageWorkspaceGroups,
  getImageWorkspaceItems,
  getImageWorkspaceModels,
  submitImageEdit,
  submitImageGeneration,
} from './api'
import './image-workspace.css'
import type {
  GroupOption,
  ImageWorkspaceFormState,
  ImageWorkspaceItem,
  ModelOption,
} from './types'

const SESSION_STORAGE_KEY = 'image_workspace_session_id'

const SIZE_OPTIONS = [
  '1024x1024',
  '1024x1536',
  '1536x1024',
  '1792x1024',
  '1024x1792',
]

const GPT_IMAGE_2_SIZE_OPTIONS = ['1024x1024', '1536x1024']

const QUALITY_OPTIONS = [
  { value: 'standard', label: '标准' },
  { value: 'hd', label: '高清' },
]

const COUNT_OPTIONS = ['1', '2', '3', '4']

function createSessionId() {
  return `imgs_${Date.now()}_${nanoid(8)}`
}

function getSizeOptionsForModel(model: string) {
  return model.toLowerCase().includes('gpt-image-2')
    ? GPT_IMAGE_2_SIZE_OPTIONS
    : SIZE_OPTIONS
}

function createBatchId() {
  return `imgb_${Date.now()}_${nanoid(8)}`
}

function loadSessionId() {
  if (typeof window === 'undefined') {
    return createSessionId()
  }
  const stored = window.localStorage.getItem(SESSION_STORAGE_KEY)
  if (stored) {
    return stored
  }
  const next = createSessionId()
  window.localStorage.setItem(SESSION_STORAGE_KEY, next)
  return next
}

function saveSessionId(sessionId: string) {
  if (typeof window !== 'undefined') {
    window.localStorage.setItem(SESSION_STORAGE_KEY, sessionId)
  }
}

function getStatusVariant(status: ImageWorkspaceItem['status']) {
  if (status === 'ready') return 'default'
  if (status === 'expired') return 'outline'
  return 'destructive'
}

function getStatusLabel(status: ImageWorkspaceItem['status']) {
  if (status === 'ready') return i18n.t('已完成')
  if (status === 'expired') return i18n.t('已过期')
  return i18n.t('生成失败')
}

function dedupeItems(items: ImageWorkspaceItem[]) {
  const seen = new Set<number>()
  return items.filter((item) => {
    if (seen.has(item.id)) return false
    seen.add(item.id)
    return true
  })
}

function InfoPill(props: { label: string; value: string }) {
  return (
    <div className='border-border bg-muted/40 rounded-lg border px-4 py-3'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div className='text-foreground mt-1 text-sm font-semibold'>
        {props.value}
      </div>
    </div>
  )
}

function ImageWorkspaceSkeleton() {
  return (
    <div className='image-workspace-layout'>
      <Skeleton className='h-[46rem] rounded-xl' />
      <Skeleton className='h-[46rem] rounded-xl' />
    </div>
  )
}

export function ImageWorkspace() {
  useTranslation()
  const [sessionId, setSessionId] = useState(loadSessionId)
  const [galleryTab, setGalleryTab] = useState('session')
  const [selectedSourceId, setSelectedSourceId] = useState<number | null>(null)
  const [previewItem, setPreviewItem] = useState<ImageWorkspaceItem | null>(
    null
  )
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [form, setForm] = useState<ImageWorkspaceFormState>({
    mode: 'generate',
    model: '',
    group: '',
    prompt: '',
    size: SIZE_OPTIONS[0],
    quality: QUALITY_OPTIONS[0].value,
    count: COUNT_OPTIONS[0],
  })

  const modelsQuery = useQuery({
    queryKey: ['image-workspace-models', form.group],
    queryFn: () => getImageWorkspaceModels(form.group),
    enabled: Boolean(form.group),
  })

  const groupsQuery = useQuery({
    queryKey: ['image-workspace-groups'],
    queryFn: getImageWorkspaceGroups,
  })

  const sessionItemsQuery = useQuery({
    queryKey: ['image-workspace-items', sessionId],
    queryFn: () =>
      getImageWorkspaceItems({
        sessionId,
        page: 1,
        pageSize: 60,
      }),
  })

  const recentItemsQuery = useQuery({
    queryKey: ['image-workspace-items-recent'],
    queryFn: () =>
      getImageWorkspaceItems({
        page: 1,
        pageSize: 60,
      }),
  })

  useEffect(() => {
    const models = modelsQuery.data ?? []
    const currentModelAvailable = models.some(
      (item) => item.value === form.model
    )
    if (models.length > 0 && !currentModelAvailable) {
      const preferredModel =
        models.find((item) => item.value === 'gpt-image-2')?.value ??
        models[0].value
      queueMicrotask(() =>
        setForm((prev) => ({ ...prev, model: preferredModel }))
      )
    } else if (models.length === 0 && form.model) {
      queueMicrotask(() => setForm((prev) => ({ ...prev, model: '' })))
    }
  }, [form.model, modelsQuery.data])

  useEffect(() => {
    const groups = groupsQuery.data ?? []
    if (!form.group && groups.length > 0) {
      const preferredGroup =
        groups.find((item) => item.value === 'default')?.value ??
        groups[0].value
      queueMicrotask(() =>
        setForm((prev) => ({ ...prev, group: preferredGroup }))
      )
    }
  }, [form.group, groupsQuery.data])

  const sessionItems = sessionItemsQuery.data?.items ?? []
  const recentItems = recentItemsQuery.data?.items ?? []
  const readySourceItems = dedupeItems(
    [...sessionItems, ...recentItems].filter(
      (item) => item.status === 'ready' && item.image_url
    )
  )
  const selectedSource =
    readySourceItems.find((item) => item.id === selectedSourceId) ?? null
  const selectedGroup =
    (groupsQuery.data ?? []).find((item) => item.value === form.group) ?? null
  const sizeOptions = getSizeOptionsForModel(form.model)

  useEffect(() => {
    if (form.mode !== 'edit') return
    if (selectedSource) return
    if (readySourceItems.length > 0) {
      queueMicrotask(() => setSelectedSourceId(readySourceItems[0].id))
    }
  }, [form.mode, readySourceItems, selectedSource])

  useEffect(() => {
    const nextSizeOptions = getSizeOptionsForModel(form.model)
    if (!nextSizeOptions.includes(form.size)) {
      queueMicrotask(() =>
        setForm((prev) => ({ ...prev, size: nextSizeOptions[0] }))
      )
    }
  }, [form.model, form.size])

  const updateForm = <K extends keyof ImageWorkspaceFormState>(
    key: K,
    value: ImageWorkspaceFormState[K]
  ) => {
    setForm((prev) => ({ ...prev, [key]: value }))
  }

  const handleNewSession = () => {
    const nextSessionId = createSessionId()
    setSessionId(nextSessionId)
    saveSessionId(nextSessionId)
    setGalleryTab('session')
    setSelectedSourceId(null)
  }

  const handleSubmit = async () => {
    if (!form.model) {
      toast.error(i18n.t('请先选择模型'))
      return
    }
    if (!form.group) {
      toast.error(i18n.t('请先选择分组'))
      return
    }
    if (!form.prompt.trim()) {
      toast.error(i18n.t('请先输入提示词'))
      return
    }
    if (form.mode === 'edit' && !selectedSource) {
      toast.error(i18n.t('改图模式需要先选择一张来源图片'))
      return
    }

    setIsSubmitting(true)
    try {
      const payload = {
        model: form.model,
        prompt: form.prompt.trim(),
        size: form.size,
        quality: form.quality,
        n: Number(form.count),
      }
      const batchId = createBatchId()

      if (form.mode === 'generate') {
        await submitImageGeneration(payload, {
          group: form.group,
          sessionId,
          batchId,
        })
      } else if (selectedSource) {
        const imageFile = await fetchImageWorkspaceSourceFile(selectedSource)
        await submitImageEdit(
          payload,
          {
            group: form.group,
            sessionId,
            batchId,
            sourceItemId: selectedSource.id,
          },
          imageFile
        )
      }

      await Promise.all([
        sessionItemsQuery.refetch(),
        recentItemsQuery.refetch(),
      ])
      setGalleryTab('session')
      toast.success(
        form.mode === 'generate' ? i18n.t('图片生成完成') : i18n.t('改图完成')
      )
    } catch (error) {
      const err = error as {
        response?: { data?: { error?: { message?: string }; message?: string } }
        message?: string
      }
      const message =
        err?.response?.data?.error?.message ||
        err?.response?.data?.message ||
        err?.message ||
        i18n.t('图片请求失败')
      toast.error(message)
    } finally {
      setIsSubmitting(false)
    }
  }

  const applyItemPrompt = (item: ImageWorkspaceItem) => {
    setForm((prev) => ({
      ...prev,
      prompt: item.revised_prompt || item.prompt,
    }))
  }

  const startEditFromItem = (item: ImageWorkspaceItem) => {
    setSelectedSourceId(item.id)
    setForm((prev) => ({
      ...prev,
      mode: 'edit',
      prompt: item.revised_prompt || item.prompt,
      model: item.model || prev.model,
    }))
  }

  const refreshGallery = async () => {
    await Promise.all([sessionItemsQuery.refetch(), recentItemsQuery.refetch()])
  }

  const isLoading =
    modelsQuery.isLoading ||
    groupsQuery.isLoading ||
    sessionItemsQuery.isLoading ||
    recentItemsQuery.isLoading

  if (isLoading && !modelsQuery.data && !groupsQuery.data) {
    return <ImageWorkspaceSkeleton />
  }

  const models = modelsQuery.data ?? []
  const groups = groupsQuery.data ?? []

  return (
    <>
      <div className='market-image-workspace min-h-full'>
        <div className='mx-auto max-w-[1440px] space-y-8'>
          {form.group &&
            !modelsQuery.isLoading &&
            !modelsQuery.isError &&
            models.length === 0 && (
              <Alert className='border-amber-500/40 bg-amber-500/5'>
                <AlertCircle className='text-amber-600' />
                <AlertTitle>
                  <Trans i18nKey={'当前分组没有可用的生图模型'} />
                </AlertTitle>
                <AlertDescription>
                  {form.group === 'auto'
                    ? i18n.t(
                        '请检查后台 AutoGroups 是否包含生图模型所在分组、用户分组权限和模型计费配置。'
                      )
                    : i18n.t(
                        '请检查该分组的渠道模型、用户权限和模型计费配置。'
                      )}
                </AlertDescription>
              </Alert>
            )}
          {[groupsQuery, modelsQuery, sessionItemsQuery, recentItemsQuery].some(
            (query) => query.isError
          ) && (
            <Alert variant='destructive'>
              <AlertTitle>
                <Trans i18nKey={'加载失败'} />
              </AlertTitle>
              <Button
                variant='outline'
                onClick={() => {
                  if (groupsQuery.isError) void groupsQuery.refetch()
                  if (modelsQuery.isError) void modelsQuery.refetch()
                  if (sessionItemsQuery.isError)
                    void sessionItemsQuery.refetch()
                  if (recentItemsQuery.isError) void recentItemsQuery.refetch()
                }}
              >
                <Trans i18nKey={'Retry'} />
              </Button>
            </Alert>
          )}
          <header className='image-workspace-heading'>
            <div>
              <h1>
                <Trans i18nKey={'生图工作台'} />
              </h1>
            </div>
            <div className='flex flex-wrap gap-2'>
              <Button variant='outline' onClick={handleNewSession}>
                <RefreshCw data-icon='inline-start' />
                <Trans i18nKey={'新建会话'} />
              </Button>
              <Button
                render={
                  <Link
                    to='/usage-logs/$section'
                    params={{ section: 'common' }}
                  />
                }
              >
                <Clock3 data-icon='inline-start' />
                <Trans i18nKey={'查看日志'} />
              </Button>
            </div>
          </header>

          <div className='image-workspace-layout'>
            <section className='image-workspace-panel'>
              <h2 className='image-workspace-section-title'>
                <Sparkles className='size-4' />
                <Trans i18nKey={'创作对话'} />
              </h2>

              <div className='mt-5 space-y-5'>
                <Tabs
                  value={form.mode}
                  onValueChange={(value) =>
                    updateForm('mode', value as ImageWorkspaceFormState['mode'])
                  }
                >
                  <TabsList className='grid w-full grid-cols-2'>
                    <TabsTrigger value='generate'>
                      <WandSparkles className='size-4' />
                      <Trans i18nKey={'生图模式'} />
                    </TabsTrigger>
                    <TabsTrigger value='edit'>
                      <PenSquare className='size-4' />
                      <Trans i18nKey={'改图模式'} />
                    </TabsTrigger>
                  </TabsList>
                </Tabs>

                <div className='grid gap-2'>
                  <Label htmlFor='image-workspace-prompt'>
                    <Trans i18nKey={'提示词'} />
                  </Label>
                  <Textarea
                    id='image-workspace-prompt'
                    value={form.prompt}
                    onChange={(event) =>
                      updateForm('prompt', event.target.value)
                    }
                    className='min-h-40 rounded-xl'
                  />
                </div>

                <div className='grid gap-4 md:grid-cols-2'>
                  <div className='grid gap-2'>
                    <Label htmlFor='image-workspace-model'>
                      <Trans i18nKey={'模型'} />
                    </Label>
                    <Select
                      items={models.map((item: ModelOption) => ({
                        value: item.value,
                        label: item.label,
                      }))}
                      value={form.model}
                      onValueChange={(value) =>
                        updateForm('model', value ?? '')
                      }
                    >
                      <SelectTrigger
                        id='image-workspace-model'
                        className='w-full rounded-lg'
                      >
                        <SelectValue placeholder={i18n.t('选择模型')} />
                      </SelectTrigger>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {models.map((item) => (
                            <SelectItem key={item.value} value={item.value}>
                              {item.label}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </div>

                  <div className='grid gap-2'>
                    <Label htmlFor='image-workspace-group'>
                      <Trans i18nKey={'分组'} />
                    </Label>
                    <Select
                      items={groups.map((item: GroupOption) => ({
                        value: item.value,
                        label: item.label,
                      }))}
                      value={form.group}
                      onValueChange={(value) =>
                        updateForm('group', value ?? '')
                      }
                    >
                      <SelectTrigger
                        id='image-workspace-group'
                        className='w-full rounded-lg'
                      >
                        <SelectValue placeholder={i18n.t('选择分组')} />
                      </SelectTrigger>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {groups.map((item) => (
                            <SelectItem key={item.value} value={item.value}>
                              <div className='flex items-center gap-2'>
                                <span>{item.label}</span>
                                <span className='text-muted-foreground text-xs'>
                                  <Trans i18nKey={'倍率 x'} />
                                  {item.ratio}
                                </span>
                              </div>
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    {selectedGroup?.desc ? (
                      <div className='text-muted-foreground text-xs leading-6'>
                        {selectedGroup.desc}
                      </div>
                    ) : null}
                  </div>
                </div>

                <div className='grid gap-4 md:grid-cols-3'>
                  <div className='grid gap-2'>
                    <Label htmlFor='image-workspace-size'>
                      <Trans i18nKey={'尺寸'} />
                    </Label>
                    <Select
                      items={sizeOptions.map((value) => ({
                        value,
                        label: value,
                      }))}
                      value={form.size}
                      onValueChange={(value) =>
                        updateForm('size', value ?? sizeOptions[0])
                      }
                    >
                      <SelectTrigger
                        id='image-workspace-size'
                        className='w-full rounded-lg'
                      >
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {sizeOptions.map((value) => (
                            <SelectItem key={value} value={value}>
                              {value}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </div>

                  <div className='grid gap-2'>
                    <Label htmlFor='image-workspace-quality'>
                      <Trans i18nKey={'清晰度'} />
                    </Label>
                    <Select
                      items={QUALITY_OPTIONS.map((item) => ({
                        ...item,
                        label: i18n.t(item.label),
                      }))}
                      value={form.quality}
                      onValueChange={(value) =>
                        updateForm('quality', value ?? QUALITY_OPTIONS[0].value)
                      }
                    >
                      <SelectTrigger
                        id='image-workspace-quality'
                        className='w-full rounded-lg'
                      >
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {QUALITY_OPTIONS.map((item) => (
                            <SelectItem key={item.value} value={item.value}>
                              {i18n.t(item.label)}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </div>

                  <div className='grid gap-2'>
                    <Label htmlFor='image-workspace-count'>
                      <Trans i18nKey={'生成张数'} />
                    </Label>
                    <Select
                      items={COUNT_OPTIONS.map((value) => ({
                        value,
                        label: value,
                      }))}
                      value={form.count}
                      onValueChange={(value) =>
                        updateForm('count', value ?? COUNT_OPTIONS[0])
                      }
                    >
                      <SelectTrigger
                        id='image-workspace-count'
                        className='w-full rounded-2xl'
                      >
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          {COUNT_OPTIONS.map((value) => (
                            <SelectItem key={value} value={value}>
                              {value}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </div>
                </div>

                {form.mode === 'edit' && (
                  <div className='border-border bg-muted/40 rounded-lg border p-4'>
                    <div className='flex items-center justify-between gap-3'>
                      <div>
                        <div className='text-foreground text-sm font-semibold'>
                          <Trans i18nKey={'来源图片'} />
                        </div>
                      </div>
                      <Badge variant='outline'>
                        {readySourceItems.length} <Trans i18nKey={'张可选'} />
                      </Badge>
                    </div>

                    <div className='mt-4 space-y-3'>
                      {selectedSource ? (
                        <div className='grid gap-3 md:grid-cols-[7.5rem_minmax(0,1fr)]'>
                          <img
                            src={selectedSource.image_url}
                            alt={selectedSource.prompt}
                            className='max-h-64 w-full rounded-lg border object-contain'
                          />
                          <div className='space-y-2'>
                            <div className='text-foreground text-sm leading-6 break-words'>
                              {selectedSource.revised_prompt ||
                                selectedSource.prompt}
                            </div>
                            <div className='text-muted-foreground text-xs'>
                              {selectedSource.model} ·{' '}
                              {dayjs
                                .unix(selectedSource.created_at)
                                .format('MM-DD HH:mm')}
                            </div>
                            <div className='flex flex-wrap gap-2'>
                              <Button
                                variant='outline'
                                size='sm'
                                onClick={() => setPreviewItem(selectedSource)}
                              >
                                <Trans i18nKey={'预览'} />
                              </Button>
                              <Button
                                variant='outline'
                                size='sm'
                                onClick={() => applyItemPrompt(selectedSource)}
                              >
                                <Trans i18nKey={'复用提示词'} />
                              </Button>
                            </div>
                          </div>
                        </div>
                      ) : (
                        <div className='text-muted-foreground text-sm'>
                          <Trans
                            i18nKey={
                              '暂时没有可用来源图片。你可以先生成一张，再回来做改图。'
                            }
                          />
                        </div>
                      )}

                      {readySourceItems.length > 0 ? (
                        <div className='grid grid-cols-3 gap-2 md:grid-cols-4'>
                          {readySourceItems.slice(0, 8).map((item) => (
                            <button
                              key={item.id}
                              type='button'
                              onClick={() => setSelectedSourceId(item.id)}
                              className={`overflow-hidden rounded-2xl border text-left transition ${
                                selectedSourceId === item.id
                                  ? 'border-primary ring-primary/20 ring-2'
                                  : 'hover:border-primary'
                              }`}
                            >
                              <img
                                src={item.image_url}
                                alt={item.prompt}
                                className='aspect-square w-full object-contain'
                              />
                            </button>
                          ))}
                        </div>
                      ) : null}
                    </div>
                  </div>
                )}

                <Separator />

                <div className='flex flex-col gap-3 sm:flex-row'>
                  <Button
                    size='lg'
                    className='flex-1'
                    onClick={handleSubmit}
                    disabled={isSubmitting || !form.model || !form.group}
                  >
                    {isSubmitting ? (
                      <>
                        <RefreshCw className='size-4 animate-spin' />
                        <Trans i18nKey={'正在处理'} />
                      </>
                    ) : (
                      <>
                        {form.mode === 'generate' ? (
                          <WandSparkles className='size-4' />
                        ) : (
                          <ImagePlus className='size-4' />
                        )}
                        {form.mode === 'generate'
                          ? i18n.t('开始生图')
                          : i18n.t('开始改图')}
                      </>
                    )}
                  </Button>
                  <Button
                    variant='outline'
                    size='lg'
                    onClick={() => updateForm('prompt', '')}
                    disabled={isSubmitting}
                  >
                    <Trans i18nKey={'清空提示词'} />
                  </Button>
                </div>
              </div>
            </section>

            <section className='image-workspace-gallery'>
              <div className='flex items-end justify-between gap-3'>
                <div>
                  <h2 className='image-workspace-section-title'>
                    <Images className='size-4' />
                    <Trans i18nKey={'作品记录'} />
                  </h2>
                </div>
                <Button variant='outline' size='sm' onClick={refreshGallery}>
                  <RefreshCw className='size-4' />
                  <Trans i18nKey={'刷新'} />
                </Button>
              </div>

              <Tabs value={galleryTab} onValueChange={setGalleryTab}>
                <TabsList className='mt-5 grid w-full grid-cols-2'>
                  <TabsTrigger value='session'>
                    <Sparkles className='size-4' />
                    <Trans i18nKey={'当前会话'} />
                  </TabsTrigger>
                  <TabsTrigger value='recent'>
                    <History className='size-4' />
                    <Trans i18nKey={'最近历史'} />
                  </TabsTrigger>
                </TabsList>

                <TabsContent value='session' className='mt-4'>
                  <ImageGrid
                    items={sessionItems}
                    emptyTitle={i18n.t('当前会话还没有图片')}
                    onPreview={setPreviewItem}
                    onReusePrompt={applyItemPrompt}
                    onEditFromItem={startEditFromItem}
                  />
                </TabsContent>

                <TabsContent value='recent' className='mt-4'>
                  <ImageGrid
                    items={recentItems}
                    emptyTitle={i18n.t('最近还没有图片历史')}
                    onPreview={setPreviewItem}
                    onReusePrompt={applyItemPrompt}
                    onEditFromItem={startEditFromItem}
                  />
                </TabsContent>
              </Tabs>

              <div className='image-workspace-telemetry'>
                <InfoPill
                  label={i18n.t('当前会话图片数')}
                  value={String(sessionItems.length)}
                />
                <InfoPill
                  label={i18n.t('最近历史图片数')}
                  value={String(recentItems.length)}
                />
                <InfoPill
                  label={i18n.t('当前浏览标签')}
                  value={
                    galleryTab === 'session'
                      ? i18n.t('当前会话')
                      : i18n.t('最近历史')
                  }
                />
                <InfoPill
                  label={i18n.t('当前选中来源图')}
                  value={
                    selectedSource ? `#${selectedSource.id}` : i18n.t('未选择')
                  }
                />
              </div>
            </section>
          </div>
        </div>
      </div>

      <ImageDialog
        imageUrl={previewItem?.image_url ?? ''}
        taskId={previewItem ? String(previewItem.id) : undefined}
        open={Boolean(previewItem)}
        onOpenChange={(open) => {
          if (!open) {
            setPreviewItem(null)
          }
        }}
      />
    </>
  )
}

function ImageGrid(props: {
  items: ImageWorkspaceItem[]
  emptyTitle: string
  onPreview: (item: ImageWorkspaceItem) => void
  onReusePrompt: (item: ImageWorkspaceItem) => void
  onEditFromItem: (item: ImageWorkspaceItem) => void
}) {
  useTranslation()
  if (props.items.length === 0) {
    return (
      <Empty className='min-h-72 rounded-xl border'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <Images className='size-4' />
          </EmptyMedia>
          <EmptyTitle>{props.emptyTitle}</EmptyTitle>
        </EmptyHeader>
      </Empty>
    )
  }

  return (
    <div className='image-workspace-results'>
      {props.items.map((item) => {
        const isReady = item.status === 'ready' && !!item.image_url
        const isExpired = item.status === 'expired'

        return (
          <div
            key={item.id}
            className='image-workspace-result'
            data-status={item.status}
          >
            <div className='flex min-w-0 flex-col gap-4'>
              {isReady ? (
                <button
                  type='button'
                  onClick={() => props.onPreview(item)}
                  className='image-workspace-result-image'
                >
                  <img
                    src={item.image_url}
                    alt={item.prompt}
                    className='max-h-80 w-full object-contain'
                  />
                </button>
              ) : (
                <div className='bg-muted flex size-28 items-center justify-center rounded-2xl border'>
                  <div className='text-muted-foreground flex items-center gap-2 text-sm'>
                    <AlertCircle className='size-4' />
                    {isExpired ? i18n.t('已过期') : i18n.t('不可用')}
                  </div>
                </div>
              )}

              <div className='min-w-0 flex-1 space-y-3'>
                <div className='flex items-start justify-between gap-3'>
                  <div className='min-w-0'>
                    <div className='text-foreground text-sm leading-6 break-words'>
                      {item.revised_prompt || item.prompt}
                    </div>
                    <div className='text-muted-foreground mt-2 flex flex-wrap items-center gap-2 text-xs'>
                      <span className='image-workspace-model-id'>
                        {item.model}
                      </span>
                      <span>·</span>
                      <span>
                        {dayjs.unix(item.created_at).format('MM-DD HH:mm')}
                      </span>
                      {item.expires_at > 0 && !isExpired ? (
                        <>
                          <span>·</span>
                          <span>
                            <Trans i18nKey={'到期'} />{' '}
                            {dayjs.unix(item.expires_at).format('MM-DD HH:mm')}
                          </span>
                        </>
                      ) : null}
                    </div>
                  </div>
                  <Badge
                    className='image-workspace-result-status'
                    variant={getStatusVariant(item.status)}
                  >
                    {getStatusLabel(item.status)}
                  </Badge>
                </div>

                {item.error_message && item.status !== 'ready' ? (
                  <div className='rounded-2xl border border-dashed border-red-200 px-3 py-2 text-xs leading-5 text-red-600 dark:border-red-500/20 dark:text-red-300'>
                    {item.error_message}
                  </div>
                ) : null}

                <div className='flex flex-wrap gap-2'>
                  {isReady ? (
                    <Button
                      variant='outline'
                      size='sm'
                      onClick={() => props.onPreview(item)}
                    >
                      <Trans i18nKey={'预览'} />
                    </Button>
                  ) : null}
                  {isReady && item.download_url ? (
                    <Button
                      variant='outline'
                      size='sm'
                      render={
                        <a href={item.download_url}>
                          <Download className='size-4' />
                          <Trans i18nKey={'下载'} />
                        </a>
                      }
                    />
                  ) : null}
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={() => props.onReusePrompt(item)}
                  >
                    <Trans i18nKey={'复用提示词'} />
                  </Button>
                  {isReady ? (
                    <Button
                      variant='outline'
                      size='sm'
                      onClick={() => props.onEditFromItem(item)}
                    >
                      <Trans i18nKey={'基于这张继续改'} />
                    </Button>
                  ) : null}
                </div>
              </div>
            </div>
          </div>
        )
      })}
    </div>
  )
}
