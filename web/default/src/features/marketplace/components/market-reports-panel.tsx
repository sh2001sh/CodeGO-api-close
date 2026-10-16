import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import {
  getMarketReports,
  resolveMarketReport,
  type MarketReport,
} from '../shop-api'

export function MarketReportsPanel() {
  const { t } = useTranslation()
  const [status, setStatus] = useState('pending')
  const [page, setPage] = useState(1)
  const query = useQuery({
    queryKey: ['marketplace-reports', status, page],
    queryFn: () => getMarketReports(status, page),
  })
  return (
    <section className='governance-reports' aria-label={t('市场举报管理')}>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <h2 className='font-semibold'>{t('市场举报管理')}</h2>
        <NativeSelect
          aria-label={t('举报状态')}
          value={status}
          onChange={(event) => {
            setStatus(event.target.value)
            setPage(1)
          }}
        >
          <option value='pending'>{t('待处理')}</option>
          <option value='resolved'>{t('已处理')}</option>
          <option value='dismissed'>{t('已驳回')}</option>
          <option value=''>{t('全部')}</option>
        </NativeSelect>
      </div>
      {query.isPending && (
        <p role='status' className='mt-4 text-sm'>
          {t('加载中')}
        </p>
      )}
      {query.isError && (
        <div role='alert' className='mt-4 text-sm'>
          <p>{query.error.message}</p>
          <Button variant='outline' onClick={() => void query.refetch()}>
            {t('重试')}
          </Button>
        </div>
      )}
      {query.data && (
        <div className='mt-4 space-y-4'>
          {!query.data.items.length && (
            <p className='text-muted-foreground text-sm'>{t('暂无举报记录')}</p>
          )}
          {query.data.items.map((report) => (
            <ReportReview key={report.id} report={report} />
          ))}
          {query.data.total > 20 && (
            <nav
              className='flex items-center justify-between gap-3 text-sm'
              aria-label={t('举报分页')}
            >
              <span>
                {page} / {Math.ceil(query.data.total / 20)}
              </span>
              <div className='flex gap-2'>
                <Button
                  variant='outline'
                  disabled={page <= 1}
                  onClick={() => setPage(page - 1)}
                >
                  {t('上一页')}
                </Button>
                <Button
                  variant='outline'
                  disabled={page * 20 >= query.data.total}
                  onClick={() => setPage(page + 1)}
                >
                  {t('下一页')}
                </Button>
              </div>
            </nav>
          )}
        </div>
      )}
    </section>
  )
}

function ReportReview(props: { report: MarketReport }) {
  const { t } = useTranslation()
  const [note, setNote] = useState('')
  const client = useQueryClient()
  const mutation = useMutation({
    mutationFn: resolveMarketReport,
    onSuccess: () => {
      toast.success(t('举报处理已保存'))
      void client.invalidateQueries({ queryKey: ['marketplace-reports'] })
      void client.invalidateQueries({ queryKey: ['marketplace-shops'] })
      void client.invalidateQueries({ queryKey: ['marketplace-groups'] })
    },
    onError: (error) => toast.error(error.message),
  })
  const report = props.report
  const reasons: Record<string, string> = {
    advertising: t('广告或站外引流'),
    fraud: t('虚假宣传或欺诈'),
    inappropriate: t('不当内容'),
    other: t('其他'),
  }
  return (
    <article className='governance-report-review'>
      <h3 className='font-medium'>
        #{report.id} · {report.target_type === 'shop' ? t('店铺') : t('渠道')}{' '}
        {report.target_id} · {reasons[report.reason] || report.reason}
      </h3>
      <p className='text-muted-foreground break-words'>
        {report.details || t('未填写补充说明')}
      </p>
      <pre className='bg-muted/40 max-h-48 overflow-auto rounded-md p-3 text-xs break-words whitespace-pre-wrap'>
        {report.evidence || t('无内容快照')}
      </pre>
      {report.status === 'pending' ? (
        <form
          className='space-y-2'
          onSubmit={(event) => {
            event.preventDefault()
            mutation.mutate({ id: report.id, action: 'remove_content', note })
          }}
        >
          <label htmlFor={`report-note-${report.id}`} className='font-medium'>
            {t('处理说明')}
          </label>
          <Textarea
            id={`report-note-${report.id}`}
            value={note}
            maxLength={500}
            required
            onChange={(event) => setNote(event.target.value)}
            placeholder={t('请填写处理理由，用户可据此了解处理结果')}
          />
          <div className='flex flex-wrap gap-2'>
            <Button
              type='submit'
              variant='destructive'
              disabled={mutation.isPending || !note.trim()}
            >
              {t('清除被举报的公开内容')}
            </Button>
            <Button
              type='button'
              variant='outline'
              disabled={mutation.isPending || !note.trim()}
              onClick={() =>
                mutation.mutate({ id: report.id, action: 'dismiss', note })
              }
            >
              {t('驳回举报')}
            </Button>
          </div>
        </form>
      ) : (
        <p className='break-words'>
          {t('处理说明')}：{report.resolution}
        </p>
      )}
    </article>
  )
}
