import { useId, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Flag } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { NativeSelect } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { submitMarketReport } from '../shop-api'

export function MarketReportButton(props: {
  type: 'channel' | 'shop'
  id: string
  name: string
  authed: boolean
}) {
  const { t } = useTranslation()
  const fieldID = useId()
  const [open, setOpen] = useState(false)
  const [reason, setReason] = useState('advertising')
  const [details, setDetails] = useState('')
  const mutation = useMutation({
    mutationFn: submitMarketReport,
    onSuccess: () => {
      toast.success(t('举报已提交，等待管理员处理'))
      setOpen(false)
      setDetails('')
    },
    onError: (error) => toast.error(error.message),
  })
  return (
    <div
      className='shrink-0'
      onClick={(event) => event.stopPropagation()}
      onKeyDown={(event) => event.stopPropagation()}
    >
      <button
        type='button'
        className='text-muted-foreground hover:text-foreground focus-visible:ring-ring inline-flex min-h-9 shrink-0 items-center gap-1.5 rounded px-2 text-xs whitespace-nowrap focus-visible:ring-2'
        onClick={() => {
          if (!props.authed) {
            toast.error(t('请先登录后举报'))
            return
          }
          setOpen(true)
        }}
        aria-label={t('举报 {{name}}', { name: props.name })}
      >
        <Flag size={14} aria-hidden='true' />
        {t('举报')}
      </button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('举报公开内容')}</DialogTitle>
            <DialogDescription>{props.name}</DialogDescription>
          </DialogHeader>
          <form
            className='space-y-4'
            onSubmit={(event) => {
              event.preventDefault()
              mutation.mutate({
                target_type: props.type,
                target_id: props.id,
                reason,
                details,
              })
            }}
          >
            <div className='space-y-2'>
              <label
                htmlFor={`${fieldID}-reason`}
                className='text-sm font-medium'
              >
                {t('举报原因')}
              </label>
              <NativeSelect
                id={`${fieldID}-reason`}
                className='w-full'
                value={reason}
                onChange={(event) => setReason(event.target.value)}
              >
                <option value='advertising'>{t('广告或站外引流')}</option>
                <option value='fraud'>{t('虚假宣传或欺诈')}</option>
                <option value='inappropriate'>{t('不当内容')}</option>
                <option value='other'>{t('其他')}</option>
              </NativeSelect>
            </div>
            <div className='space-y-2'>
              <label
                htmlFor={`${fieldID}-details`}
                className='text-sm font-medium'
              >
                {t('举报说明')}
              </label>
              <Textarea
                id={`${fieldID}-details`}
                value={details}
                maxLength={500}
                rows={4}
                onChange={(event) => setDetails(event.target.value)}
                placeholder={t('说明具体问题，最多 500 字')}
              />
            </div>
            <Button
              className='w-full'
              type='submit'
              disabled={mutation.isPending}
            >
              {mutation.isPending ? t('正在提交') : t('提交举报')}
            </Button>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}
