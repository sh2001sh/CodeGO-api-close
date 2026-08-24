import { useEffect, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Activity, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getUserModelsForGroup } from '@/lib/api'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { NativeSelect } from '@/components/ui/native-select'
import { testApiKeyConnectivity } from '../api'
import type { ApiKey } from '../types'

export function ApiKeyConnectivityTestDialog(props: {
  apiKey: ApiKey | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const [model, setModel] = useState('')
  const models = useQuery({
    queryKey: ['api-key-test-models', props.apiKey?.id, props.apiKey?.group],
    queryFn: () => getUserModelsForGroup(props.apiKey?.group ?? ''),
    enabled: props.open && props.apiKey != null,
  })
  const test = useMutation({
    mutationFn: () => testApiKeyConnectivity(props.apiKey!.id, model),
    onSuccess: (result) => {
      if (!result.success || !result.data) {
        toast.error(result.message || t('测试失败'))
        return
      }
      toast.success(
        t('测试通过：{{latency}} ms', { latency: result.data.latency_ms })
      )
    },
    onError: () => toast.error(t('测试失败')),
  })

  useEffect(() => {
    const firstModel = models.data?.data?.[0] ?? ''
    if (firstModel) setModel(firstModel)
  }, [models.data?.data])

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('测试 API Key 连通性')}</DialogTitle>
          <DialogDescription>
            {t('使用该 Key 当前分组可路由的模型进行一次不计费连接测试。')}
          </DialogDescription>
        </DialogHeader>
        <div className='grid gap-2 py-2'>
          <label htmlFor='api-key-test-model' className='text-sm font-medium'>
            {t('测试模型')}
          </label>
          <NativeSelect
            id='api-key-test-model'
            value={model}
            onChange={(event) => setModel(event.target.value)}
            disabled={models.isLoading || test.isPending}
          >
            <option value=''>{t('选择模型')}</option>
            {(models.data?.data ?? []).map((item) => (
              <option key={item} value={item}>
                {item}
              </option>
            ))}
          </NativeSelect>
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('取消')}
          </Button>
          <Button
            disabled={!model || test.isPending}
            onClick={() => test.mutate()}
          >
            {test.isPending ? (
              <Loader2 className='animate-spin' />
            ) : (
              <Activity />
            )}
            {t('测试')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
