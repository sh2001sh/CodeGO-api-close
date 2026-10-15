import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Boxes, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getUserModelsForGroup } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import type { ApiKeyGroupOption } from './api-key-group-combobox'

export function ApiKeyAvailableModelsDialog(props: {
  option?: ApiKeyGroupOption
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const needsOfficialLookup = props.option?.category === 'official'
  const query = useQuery({
    queryKey: ['user-models', props.option?.value],
    queryFn: () => getUserModelsForGroup(props.option?.value ?? ''),
    enabled: open && Boolean(props.option) && needsOfficialLookup,
    staleTime: 5 * 60 * 1000,
  })
  const models = useMemo(
    () =>
      needsOfficialLookup
        ? (query.data?.data ?? [])
        : (props.option?.models ?? []),
    [needsOfficialLookup, props.option?.models, query.data?.data]
  )
  const filteredModels = useMemo(() => {
    const keyword = search.trim().toLowerCase()
    if (!keyword) return models
    return models.filter((model) => model.toLowerCase().includes(keyword))
  }, [models, search])

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen)
        if (!nextOpen) setSearch('')
      }}
    >
      <Button
        type='button'
        variant='outline'
        size='sm'
        className='gap-2'
        disabled={!props.option}
        onClick={() => setOpen(true)}
      >
        <Boxes className='size-4' />
        {t('可用模型')}
      </Button>
      <DialogContent className='flex max-h-[82dvh] !max-w-2xl flex-col gap-0 overflow-hidden p-0'>
        <DialogHeader className='border-b px-5 py-4 pr-12'>
          <div className='flex items-center gap-2'>
            <DialogTitle>{t('可用模型')}</DialogTitle>
            <Badge variant='secondary'>{models.length}</Badge>
          </div>
          <DialogDescription>
            {props.option
              ? t('{{group}} 当前可以调用的模型', {
                  group: props.option.label,
                })
              : t('请先选择分组')}
          </DialogDescription>
        </DialogHeader>
        <div className='border-b p-4'>
          <div className='relative'>
            <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2' />
            <Input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={t('搜索模型')}
              className='pl-9'
            />
          </div>
        </div>
        <div className='min-h-0 flex-1 overflow-y-auto p-4'>
          {query.isLoading ? (
            <ModelListSkeleton />
          ) : query.isError ? (
            <div className='text-destructive py-12 text-center text-sm'>
              {t('可用模型加载失败')}
            </div>
          ) : filteredModels.length === 0 ? (
            <div className='text-muted-foreground py-12 text-center text-sm'>
              {search ? t('没有匹配的模型') : t('当前分组暂无可用模型')}
            </div>
          ) : (
            <div className='grid gap-2 sm:grid-cols-2'>
              {filteredModels.map((model) => (
                <div
                  key={model}
                  className='bg-muted/30 border-border min-w-0 rounded-lg border px-3 py-2.5 font-mono text-xs break-all'
                >
                  {model}
                </div>
              ))}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}

function ModelListSkeleton() {
  return (
    <div className='grid gap-2 sm:grid-cols-2'>
      {Array.from({ length: 8 }).map((_, index) => (
        <Skeleton key={index} className='h-10 rounded-lg' />
      ))}
    </div>
  )
}
