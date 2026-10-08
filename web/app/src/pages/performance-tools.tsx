import { useTranslation } from '../lib/i18n'
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { date } from '../lib/format'
import { Button, ErrorMessage, Field, Loading, PageHeader } from '../components/ui'
import { DataTable } from '../components/data-table'

export default function PerformanceToolsPage() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const stats = useQuery(
    resourceOptions('performance', (signal) =>
      api.GET('/api/performance/stats', { signal }).then(unwrap),
    ),
  )
  const logs = useQuery(
    resourceOptions('performance-logs', (signal) =>
      api.GET('/api/performance/logs', { signal }).then(unwrap),
    ),
  )
  const [action, setAction] = useState<'gc' | 'reset' | 'cache' | null>(null)
  const run = useMutation({
    mutationFn: () =>
      action === 'gc'
        ? api.POST('/api/performance/gc')
        : action === 'reset'
          ? api.POST('/api/performance/reset_stats')
          : api.DELETE('/api/performance/disk_cache'),
    onSuccess: () => {
      setAction(null)
      void client.invalidateQueries({ queryKey: ['performance'] })
    },
  })
  const clean = useMutation({
    mutationFn: (query: { mode: 'by_count' | 'by_days'; value: number }) =>
      api.DELETE('/api/performance/logs', { params: { query } }).then(unwrap),
    onSuccess: () => client.invalidateQueries({ queryKey: ['performance-logs'] }),
  })
  return (
    <>
      <PageHeader
        title="运行维护"
        action={
          <Button
            variant="quiet"
            onClick={() => {
              void stats.refetch()
              void logs.refetch()
            }}
          >
            {t('刷新')}
          </Button>
        }
      />
      <ErrorMessage error={stats.error ?? logs.error ?? run.error ?? clean.error} />
      {stats.isPending && <Loading />}
      {stats.data && (
        <dl className="metrics">
          <div>
            <dt>{t('服务')}</dt>
            <dd>{stats.data.service}</dd>
          </div>
          <div>
            <dt>{t('运行秒数')}</dt>
            <dd>{String(stats.data.uptime_seconds)}</dd>
          </div>
          <div>
            <dt>{t('内存使用（字节）')}</dt>
            <dd>{String(stats.data.memory_stats.alloc)}</dd>
          </div>
          <div>
            <dt>{t('系统分配（字节）')}</dt>
            <dd>{String(stats.data.memory_stats.sys)}</dd>
          </div>
          <div>
            <dt>{t('并发协程')}</dt>
            <dd>{stats.data.memory_stats.num_goroutine}</dd>
          </div>
          <div>
            <dt>{t('垃圾回收次数')}</dt>
            <dd>{stats.data.memory_stats.num_gc}</dd>
          </div>
        </dl>
      )}
      <div className="row-actions">
        <Button
          variant="quiet"
          onClick={() => {
            run.reset()
            setAction('gc')
          }}
        >
          {t('释放闲置内存')}
        </Button>
        <Button
          variant="quiet"
          onClick={() => {
            run.reset()
            setAction('reset')
          }}
        >
          {t('重置维护统计')}
        </Button>
        <Button
          variant="danger"
          disabled={!stats.data?.disk_cache_info.exists}
          onClick={() => {
            run.reset()
            setAction('cache')
          }}
        >
          {t('清理过期磁盘缓存')}
        </Button>
      </div>
      {action && (
        <div className="form-panel">
          <p>
            {action === 'gc'
              ? t('确认运行一次垃圾回收？')
              : action === 'reset'
                ? t('确认重置维护接口请求统计？')
                : t('确认删除过期的磁盘缓存文件？')}
          </p>
          <Button disabled={run.isPending} onClick={() => run.mutate()}>
            {t('确认执行')}
          </Button>
          <Button variant="quiet" disabled={run.isPending} onClick={() => setAction(null)}>
            {t('取消')}
          </Button>
        </div>
      )}
      {run.isSuccess && <p role="status">{t('操作已完成。')}</p>}
      <section className="section">
        <h2>{t('日志文件')}</h2>
        {logs.data &&
          (logs.data.enabled ? (
            <>
              <DataTable
                rows={logs.data.files}
                rowKey={(item) => item.name}
                columns={[
                  { label: '文件名', render: (item) => item.name },
                  { label: '大小（字节）', render: (item) => String(item.size) },
                  { label: '修改时间', render: (item) => date(item.mod_time) },
                ]}
              />
              <form
                className="form-panel"
                onSubmit={(event) => {
                  event.preventDefault()
                  const form = new FormData(event.currentTarget)
                  clean.mutate({
                    mode: form.get('mode') === 'by_count' ? 'by_count' : 'by_days',
                    value: Number(form.get('value')),
                  })
                }}
              >
                <label className="field" htmlFor="cleanup-mode">
                  <span>{t('日志保留方式')}</span>
                  <select name="mode" id="cleanup-mode">
                    <option value="by_days">{t('保留最近天数')}</option>
                    <option value="by_count">{t('保留最新文件数')}</option>
                  </select>
                </label>
                <Field
                  name="value"
                  label="保留天数或文件数"
                  type="number"
                  min={1}
                  required
                  defaultValue={30}
                />
                <p>{t('确认后清理超出保留范围的文件，当前活动日志保留。')}</p>
                <Button type="submit" variant="danger" disabled={clean.isPending}>
                  {t('确认清理旧日志')}
                </Button>
              </form>
            </>
          ) : (
            <p className="notice">{t('当前服务输出容器日志，未启用本地日志文件管理。')}</p>
          ))}
        {clean.data && (
          <p role="status">
            {t('已清理')} {clean.data.deleted_count} {t('个文件，释放')}{' '}
            {String(clean.data.freed_bytes)} {t('字节。')}
            {clean.data.failed_files.length > 0 &&
              t('未删除：') + String(clean.data.failed_files.join('、'))}
          </p>
        )}
      </section>
    </>
  )
}
