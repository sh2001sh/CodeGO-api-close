import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { RefreshCw, Wrench } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SectionPageLayout } from '@/components/layout'
import '@/features/channels/admin-workspace.css'
import '@/features/users/admin.css'

type Balance = { available_balance: number; reserved_balance: number }
type Reconciliation = {
  account_id: string
  consistent: boolean
  actual: Balance
  expected: Balance
}
type Metric = {
  model_name: string
  success_rate: number
  avg_latency_ms: number
  request_count?: number
}
type Response<T> = { success?: boolean; message?: string; data: T }

export function Operations() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [section, setSection] = useState('ledger')
  const [selected, setSelected] = useState<Reconciliation | null>(null)
  const [confirmation, setConfirmation] = useState('')
  const reconciliations = useQuery({
    queryKey: ['billing-reconciliations'],
    queryFn: async () => {
      const result = (
        await api.get<Response<Reconciliation[]>>(
          '/api/billing/reconciliations'
        )
      ).data
      if (result.success === false)
        throw new Error(result.message || 'Ledger reconciliation failed')
      return result.data ?? []
    },
  })
  const metrics = useQuery({
    queryKey: ['operations-slo'],
    queryFn: async () => {
      const result = (
        await api.get<Response<{ models?: Metric[] }>>(
          '/api/perf-metrics/summary',
          { params: { hours: 24 } }
        )
      ).data
      if (result.success === false)
        throw new Error(result.message || 'Routing metrics failed')
      return result.data?.models ?? []
    },
  })
  const repair = useMutation({
    mutationFn: async (accountID: string) => {
      const result = (
        await api.post<Response<unknown>>(
          `/api/billing/reconciliations/${encodeURIComponent(accountID)}/repair`
        )
      ).data
      if (result.success === false)
        throw new Error(result.message || 'Ledger repair failed')
      return result
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: ['billing-reconciliations'],
      })
      setSelected(null)
      setConfirmation('')
      toast.success(t('操作成功'))
    },
    onError: (error) => toast.error(error.message),
  })
  const items = reconciliations.data ?? []
  const inconsistent = items.filter((item) => !item.consistent)
  const refresh = () => {
    void reconciliations.refetch()
    void metrics.refetch()
  }

  return (
    <div className='admin-workspace solaris-admin'>
      <SectionPageLayout kicker=''>
        <SectionPageLayout.Title>{t('Operations')}</SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button
            variant='outline'
            onClick={refresh}
            disabled={reconciliations.isFetching || metrics.isFetching}
          >
            <RefreshCw aria-hidden='true' />
            {t('Refresh')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <Tabs
            value={section}
            onValueChange={setSection}
            className='solaris-operations-tabs gap-6'
          >
            <TabsList className='w-full'>
              <TabsTrigger value='ledger'>
                {t('Ledger reconciliation')}
              </TabsTrigger>
              <TabsTrigger value='routing'>{t('24h routing SLO')}</TabsTrigger>
            </TabsList>
            <TabsContent value='ledger' className='space-y-4'>
              <header className='admin-record-meta'>
                <h2>{t('Ledger integrity')}</h2>
                <dl>
                  <div>
                    <dt>{t('Consistent')}</dt>
                    <dd>
                      {reconciliations.isLoading || reconciliations.isError
                        ? '—'
                        : items.length - inconsistent.length}
                    </dd>
                  </div>
                  <div>
                    <dt>{t('Mismatch')}</dt>
                    <dd>
                      {reconciliations.isLoading || reconciliations.isError
                        ? '—'
                        : inconsistent.length}
                    </dd>
                  </div>
                </dl>
              </header>
              {reconciliations.isError && (
                <div role='alert' className='admin-query-error'>
                  <span>{t('Unable to load ledger reconciliation')}</span>
                  <Button
                    variant='outline'
                    onClick={() => void reconciliations.refetch()}
                  >
                    {t('重试')}
                  </Button>
                </div>
              )}
              <section
                className='admin-ledger-panel'
                aria-busy={reconciliations.isFetching}
                aria-label={t('Ledger reconciliation')}
              >
                {reconciliations.isLoading ? (
                  <div className='space-y-3 p-6'>
                    {Array.from({ length: 5 }, (_, index) => (
                      <Skeleton key={index} className='h-14 w-full' />
                    ))}
                  </div>
                ) : (
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t('Account')}</TableHead>
                        <TableHead>{t('Status')}</TableHead>
                        <TableHead className='text-right tabular-nums'>
                          {t('Available')}
                        </TableHead>
                        <TableHead className='text-right tabular-nums'>
                          {t('Reserved')}
                        </TableHead>
                        <TableHead className='text-right tabular-nums'>
                          {t('Action')}
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {items.map((item) => (
                        <TableRow key={item.account_id}>
                          <TableCell className='font-mono'>
                            {item.account_id}
                          </TableCell>
                          <TableCell>
                            <Badge
                              variant={
                                item.consistent ? 'secondary' : 'destructive'
                              }
                            >
                              {item.consistent
                                ? t('Consistent')
                                : t('Mismatch')}
                            </Badge>
                          </TableCell>
                          <TableCell className='text-right tabular-nums'>
                            {item.actual.available_balance}
                          </TableCell>
                          <TableCell className='text-right tabular-nums'>
                            {item.actual.reserved_balance}
                          </TableCell>
                          <TableCell className='text-right tabular-nums'>
                            {!item.consistent && (
                              <Button
                                variant='outline'
                                disabled={repair.isPending}
                                onClick={() => {
                                  setSelected(item)
                                  setConfirmation('')
                                }}
                              >
                                <Wrench aria-hidden='true' />
                                {t('Repair')}
                              </Button>
                            )}
                          </TableCell>
                        </TableRow>
                      ))}
                      {!reconciliations.isError && items.length === 0 && (
                        <TableRow>
                          <TableCell colSpan={5} className='h-40 text-center'>
                            {t('No ledger accounts to reconcile')}
                          </TableCell>
                        </TableRow>
                      )}
                    </TableBody>
                  </Table>
                )}
              </section>
            </TabsContent>
            <TabsContent value='routing' className='space-y-4'>
              <header className='admin-record-meta'>
                <h2>{t('24h routing SLO')}</h2>
                <dl>
                  <div>
                    <dt>{t('Models')}</dt>
                    <dd>
                      {metrics.isLoading || metrics.isError
                        ? '—'
                        : (metrics.data?.length ?? 0)}
                    </dd>
                  </div>
                </dl>
              </header>
              {metrics.isError && (
                <div role='alert' className='admin-query-error'>
                  <span>{t('加载失败')}</span>
                  <Button
                    variant='outline'
                    onClick={() => void metrics.refetch()}
                  >
                    {t('重试')}
                  </Button>
                </div>
              )}
              <section
                className='admin-ledger-panel'
                aria-busy={metrics.isFetching}
                aria-label={t('24h routing SLO')}
              >
                {metrics.isLoading ? (
                  <div className='space-y-3 p-6'>
                    {Array.from({ length: 5 }, (_, index) => (
                      <Skeleton key={index} className='h-14 w-full' />
                    ))}
                  </div>
                ) : (
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t('Models')}</TableHead>
                        <TableHead className='text-right tabular-nums'>
                          {t('成功率')}
                        </TableHead>
                        <TableHead className='text-right tabular-nums'>
                          {t('平均延迟')} (ms)
                        </TableHead>
                        <TableHead className='text-right tabular-nums'>
                          {t('请求数')}
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {metrics.data?.map((metric) => (
                        <TableRow key={metric.model_name}>
                          <TableCell className='font-mono'>
                            {metric.model_name}
                          </TableCell>
                          <TableCell className='text-right tabular-nums'>
                            {metric.success_rate}%
                          </TableCell>
                          <TableCell className='text-right tabular-nums'>
                            {metric.avg_latency_ms}
                          </TableCell>
                          <TableCell className='text-right tabular-nums'>
                            {metric.request_count ?? '—'}
                          </TableCell>
                        </TableRow>
                      ))}
                      {!metrics.isError && metrics.data?.length === 0 && (
                        <TableRow>
                          <TableCell colSpan={4} className='h-40 text-center'>
                            {t('暂无数据')}
                          </TableCell>
                        </TableRow>
                      )}
                    </TableBody>
                  </Table>
                )}
              </section>
            </TabsContent>
          </Tabs>
          <AlertDialog
            open={Boolean(selected)}
            onOpenChange={(open) => {
              if (!open && !repair.isPending) setSelected(null)
            }}
          >
            <AlertDialogContent className='solaris-admin-dialog'>
              <AlertDialogHeader>
                <AlertDialogTitle>{t('Repair')}</AlertDialogTitle>
                <AlertDialogDescription>
                  {t('Ledger reconciliation')} · {selected?.account_id}
                </AlertDialogDescription>
              </AlertDialogHeader>
              {selected && (
                <>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t('Account')}</TableHead>
                        <TableHead>{t('当前值')}</TableHead>
                        <TableHead>{t('预期值')}</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      <TableRow>
                        <TableCell>{t('Available')}</TableCell>
                        <TableCell>
                          {selected.actual.available_balance}
                        </TableCell>
                        <TableCell>
                          {selected.expected.available_balance}
                        </TableCell>
                      </TableRow>
                      <TableRow>
                        <TableCell>{t('Reserved')}</TableCell>
                        <TableCell>
                          {selected.actual.reserved_balance}
                        </TableCell>
                        <TableCell>
                          {selected.expected.reserved_balance}
                        </TableCell>
                      </TableRow>
                    </TableBody>
                  </Table>
                  <label className='space-y-2'>
                    <span className='block font-medium'>{t('Account')}</span>
                    <Input
                      value={confirmation}
                      onChange={(event) => setConfirmation(event.target.value)}
                      autoComplete='off'
                      disabled={repair.isPending}
                    />
                  </label>
                </>
              )}
              <AlertDialogFooter>
                <AlertDialogCancel disabled={repair.isPending}>
                  {t('Cancel')}
                </AlertDialogCancel>
                <Button
                  disabled={
                    repair.isPending ||
                    !selected ||
                    confirmation !== selected.account_id
                  }
                  onClick={() => selected && repair.mutate(selected.account_id)}
                >
                  {repair.isPending ? t('处理中') : t('Repair')}
                </Button>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    </div>
  )
}
