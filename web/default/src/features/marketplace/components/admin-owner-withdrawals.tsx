import { useEffect, useMemo, useState } from 'react'
import {
  Check,
  ChevronDown,
  Clipboard,
  LoaderCircle,
  QrCode,
  RefreshCw,
  UserRoundPen,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import dayjs from '@/lib/dayjs'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import {
  useAdminOwnerWithdrawals,
  useAdminOwnerWithdrawalQRCode,
  useAdminOwnerWithdrawalUpdate,
} from '../hooks'
import type { OwnerWithdrawal, OwnerWithdrawalStatus } from '../types'

export function AdminOwnerWithdrawals() {
  const { t } = useTranslation()
  const [withdrawalsOpen, setWithdrawalsOpen] = useState(false)
  const [status, setStatus] = useState<OwnerWithdrawalStatus | ''>(
    'pending_transfer'
  )
  const [page, setPage] = useState(1)
  const [selected, setSelected] = useState<OwnerWithdrawal | null>(null)
  const [note, setNote] = useState('')
  const [qrPreview, setQRPreview] = useState<{
    id: string
    url: string
  } | null>(null)
  const query = useAdminOwnerWithdrawals(status, page)
  const update = useAdminOwnerWithdrawalUpdate()
  const qrCode = useAdminOwnerWithdrawalQRCode()
  const pages = Math.max(1, Math.ceil((query.data?.total ?? 0) / 20))
  const labels = useMemo<Record<OwnerWithdrawalStatus, string>>(
    () => ({
      reclaiming: t('额度扣减中'),
      pending_transfer: t('待人工转账'),
      needs_update: t('收款信息需修改'),
      paid: t('已回收'),
      failed: t('申请失败'),
    }),
    [t]
  )

  useEffect(
    () => () => {
      if (qrPreview) URL.revokeObjectURL(qrPreview.url)
    },
    [qrPreview]
  )

  const viewQRCode = (item: OwnerWithdrawal) => {
    qrCode.mutate(item.id, {
      onSuccess: (blob) => {
        setQRPreview({ id: item.id, url: URL.createObjectURL(blob) })
      },
      onError: () => toast.error(t('加载失败')),
    })
  }

  const runAction = (
    item: OwnerWithdrawal,
    nextStatus: 'needs_update' | 'paid'
  ) => {
    update.mutate(
      {
        id: item.id,
        status: nextStatus,
        admin_note: note.trim(),
      },
      {
        onSuccess: () => {
          setSelected(null)
          setNote('')
          toast.success(
            t(nextStatus === 'paid' ? '已回收' : '已通知修改收款信息')
          )
        },
        onError: (error) =>
          toast.error(error instanceof Error ? error.message : t('操作失败')),
      }
    )
  }

  return (
    <section className='border-border border-t pt-5'>
      <Collapsible open={withdrawalsOpen} onOpenChange={setWithdrawalsOpen}>
        <CollapsibleTrigger
          render={
            <button
              type='button'
              className='focus-visible:ring-ring flex w-full items-center justify-between gap-4 text-left outline-none focus-visible:ring-2'
            />
          }
        >
          <h3 className='font-semibold'>{t('人工转账队列')}</h3>
          <ChevronDown
            className={`text-muted-foreground size-4 shrink-0 transition-transform ${withdrawalsOpen ? 'rotate-180' : ''}`}
          />
        </CollapsibleTrigger>

        <CollapsibleContent className='pt-4'>
          <div className='flex justify-end gap-2'>
            <NativeSelect
              size='sm'
              value={status}
              onChange={(event) => {
                setStatus(
                  event.currentTarget.value as OwnerWithdrawalStatus | ''
                )
                setPage(1)
                setSelected(null)
              }}
              aria-label={t('回收申请状态')}
            >
              <option value=''>{t('全部状态')}</option>
              <option value='pending_transfer'>{t('待人工转账')}</option>
              <option value='needs_update'>{t('收款信息需修改')}</option>
              <option value='paid'>{t('已回收')}</option>
              <option value='reclaiming'>{t('额度扣减中')}</option>
              <option value='failed'>{t('申请失败')}</option>
            </NativeSelect>
            <Button
              type='button'
              variant='outline'
              size='icon'
              onClick={() => void query.refetch()}
              aria-label={t('刷新回收记录')}
              title={t('刷新回收记录')}
            >
              <RefreshCw className={query.isFetching ? 'animate-spin' : ''} />
            </Button>
          </div>

          <div className='mt-4 overflow-x-auto'>
            <table className='w-full min-w-[960px] text-sm'>
              <thead className='text-muted-foreground'>
                <tr>
                  <th className='py-2.5 text-left font-medium'>
                    {t('申请时间')}
                  </th>
                  <th className='py-2.5 text-left font-medium'>
                    {t('渠道主 ID')}
                  </th>
                  <th className='py-2.5 text-right font-medium'>credits</th>
                  <th className='py-2.5 text-right font-medium'>
                    {t('回收价格')}
                  </th>
                  <th className='py-2.5 text-left font-medium'>
                    {t('收款信息')}
                  </th>
                  <th className='py-2.5 text-left font-medium'>{t('状态')}</th>
                  <th className='py-2.5 text-right font-medium'>{t('操作')}</th>
                </tr>
              </thead>
              <tbody className='divide-border divide-y'>
                {(query.data?.items ?? []).map((item) => (
                  <tr key={item.id}>
                    <td className='py-3 pr-4 tabular-nums'>
                      {dayjs(item.created_at).format('YYYY-MM-DD HH:mm')}
                    </td>
                    <td className='py-3 font-mono'>
                      {item.owner_external_id || item.owner_user_id}
                    </td>
                    <td className='py-3 text-right font-medium tabular-nums'>
                      {item.requested_credits}
                    </td>
                    <td className='py-3 text-right font-medium tabular-nums'>
                      {formatCNY(item.payout_amount_cents)}
                    </td>
                    <td className='py-3 pl-4'>
                      <div className='font-medium'>
                        {t(paymentMethodLabel(item.payment_method))} ·{' '}
                        {item.payee_name}
                      </div>
                      {item.payment_bank_name && (
                        <div className='text-muted-foreground text-xs'>
                          {item.payment_bank_name}
                        </div>
                      )}
                      {item.has_payment_qr_code && item.payment_qr_code_url ? (
                        <Button
                          type='button'
                          variant='outline'
                          size='sm'
                          className='mt-1'
                          disabled={
                            qrCode.isPending && qrCode.variables === item.id
                          }
                          onClick={() => viewQRCode(item)}
                        >
                          {qrCode.isPending && qrCode.variables === item.id ? (
                            <LoaderCircle className='animate-spin' />
                          ) : (
                            <QrCode />
                          )}
                          {t('View')}
                        </Button>
                      ) : (
                        <div className='flex items-center gap-1 font-mono text-xs'>
                          <span>{item.payment_account}</span>
                          <Button
                            type='button'
                            variant='ghost'
                            size='icon-xs'
                            title={t('复制收款账号')}
                            aria-label={t('复制收款账号')}
                            onClick={() => {
                              void navigator.clipboard.writeText(
                                item.payment_account ?? ''
                              )
                              toast.success(t('已复制'))
                            }}
                          >
                            <Clipboard />
                          </Button>
                        </div>
                      )}
                    </td>
                    <td className='py-3 pl-4'>
                      <span className='font-medium'>{labels[item.status]}</span>
                      {(item.admin_note || item.failure_message) && (
                        <div className='text-muted-foreground mt-1 max-w-64 text-xs whitespace-normal'>
                          {item.admin_note || item.failure_message}
                        </div>
                      )}
                    </td>
                    <td className='py-3 text-right'>
                      {item.status === 'pending_transfer' && (
                        <div className='flex justify-end gap-1'>
                          <Button
                            type='button'
                            size='sm'
                            disabled={update.isPending}
                            onClick={() => runAction(item, 'paid')}
                          >
                            <Check />
                            {t('已回收')}
                          </Button>
                          <Button
                            type='button'
                            variant='ghost'
                            size='sm'
                            disabled={update.isPending}
                            onClick={() => {
                              setSelected(item)
                              setNote('')
                            }}
                          >
                            <UserRoundPen />
                            {t('要求修改')}
                          </Button>
                        </div>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {!query.isPending && (query.data?.items.length ?? 0) === 0 && (
              <div className='text-muted-foreground py-8 text-center text-sm'>
                {t('暂无回收申请')}
              </div>
            )}
          </div>

          {selected && (
            <div className='border-border bg-muted/20 mt-4 grid gap-3 border-y py-4 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end'>
              <label className='space-y-1.5'>
                <span className='text-sm font-medium'>{t('资料修改说明')}</span>
                <Input
                  value={note}
                  onChange={(event) => setNote(event.currentTarget.value)}
                  maxLength={1000}
                />
              </label>
              <div className='flex flex-wrap gap-2'>
                <Button
                  type='button'
                  variant='outline'
                  disabled={update.isPending || note.trim() === ''}
                  onClick={() => runAction(selected, 'needs_update')}
                >
                  <UserRoundPen />
                  {t('要求修改')}
                </Button>
                <Button
                  type='button'
                  variant='ghost'
                  onClick={() => setSelected(null)}
                >
                  {t('取消')}
                </Button>
              </div>
            </div>
          )}

          {query.data && query.data.total > query.data.page_size && (
            <nav className='mt-4 flex items-center justify-between text-sm'>
              <span>
                {t('Page {{current}} of {{total}}', {
                  current: page,
                  total: pages,
                })}
              </span>
              <div className='flex gap-2'>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  disabled={page <= 1 || query.isFetching}
                  onClick={() => setPage((current) => current - 1)}
                >
                  {t('上一页')}
                </Button>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  disabled={page >= pages || query.isFetching}
                  onClick={() => setPage((current) => current + 1)}
                >
                  {t('下一页')}
                </Button>
              </div>
            </nav>
          )}
        </CollapsibleContent>
      </Collapsible>
      <Dialog
        open={qrPreview !== null}
        onOpenChange={(open) => {
          if (!open) setQRPreview(null)
        }}
      >
        <DialogContent className='sm:max-w-md'>
          <DialogHeader>
            <DialogTitle>{t('收款码')}</DialogTitle>
          </DialogHeader>
          {qrPreview && (
            <img
              src={qrPreview.url}
              alt={t('收款码')}
              className='max-h-[70dvh] w-full object-contain'
            />
          )}
        </DialogContent>
      </Dialog>
    </section>
  )
}

function formatCNY(cents: number) {
  return new Intl.NumberFormat('zh-CN', {
    style: 'currency',
    currency: 'CNY',
    minimumFractionDigits: 2,
  }).format(cents / 100)
}

function paymentMethodLabel(method: OwnerWithdrawal['payment_method']) {
  if (method === 'bank_card') return '银行卡'
  if (method === 'qr_code') return '收款码'
  return '支付宝'
}
