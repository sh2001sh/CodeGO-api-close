import { useEffect, useMemo, useRef, useState } from 'react'
import { z } from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import {
  Banknote,
  ChevronDown,
  ImageUp,
  Pencil,
  RefreshCw,
  Send,
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
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import {
  useOwnerWithdrawalCreate,
  useOwnerWithdrawalOverview,
  useOwnerWithdrawalPaymentUpdate,
  useOwnerWithdrawalQRCodeUpload,
  useOwnerWithdrawals,
} from '../hooks'
import type {
  OwnerWithdrawal,
  OwnerWithdrawalPaymentInput,
  OwnerWithdrawalPaymentMethod,
  OwnerWithdrawalStatus,
} from '../types'

const paymentSchema = z
  .object({
    payment_method: z.enum(['alipay', 'bank_card', 'qr_code']),
    payee_name: z.string().trim().min(2).max(100),
    payment_account: z.string().trim().max(200),
    payment_bank_name: z.string().trim().max(100),
    payment_qr_code_id: z.string().trim(),
  })
  .superRefine((value, context) => {
    if (
      value.payment_method === 'bank_card' &&
      value.payment_bank_name.length < 2
    ) {
      context.addIssue({
        code: 'custom',
        path: ['payment_bank_name'],
        message: 'Bank name is required',
      })
    }
    if (
      value.payment_method !== 'qr_code' &&
      value.payment_account.length < 3
    ) {
      context.addIssue({
        code: 'custom',
        path: ['payment_account'],
        message: 'Payment account is required',
      })
    }
    if (value.payment_method === 'qr_code' && value.payment_qr_code_id === '') {
      context.addIssue({
        code: 'custom',
        path: ['payment_qr_code_id'],
        message: 'Payment QR code is required',
      })
    }
  })

const createSchema = paymentSchema.and(
  z.object({ credits: z.number().int().min(100) })
)

type CreateForm = z.infer<typeof createSchema>
type PaymentForm = z.infer<typeof paymentSchema>

const defaultPayment: PaymentForm = {
  payment_method: 'alipay',
  payee_name: '',
  payment_account: '',
  payment_bank_name: '',
  payment_qr_code_id: '',
}

export function OwnerWithdrawalPanel() {
  const { t } = useTranslation()
  const overview = useOwnerWithdrawalOverview()
  const requests = useOwnerWithdrawals()
  const create = useOwnerWithdrawalCreate()
  const updatePayment = useOwnerWithdrawalPaymentUpdate()
  const uploadQRCode = useOwnerWithdrawalQRCodeUpload()
  const operation = useRef<{ signature: string; id: string } | null>(null)
  const [withdrawalOpen, setWithdrawalOpen] = useState(false)
  const [editing, setEditing] = useState<OwnerWithdrawal | null>(null)
  const [qrPreview, setQRPreview] = useState('')
  const [editQRPreview, setEditQRPreview] = useState('')
  const form = useForm<CreateForm>({
    resolver: zodResolver(createSchema),
    defaultValues: { ...defaultPayment, credits: 100 },
  })
  const editForm = useForm<PaymentForm>({
    resolver: zodResolver(paymentSchema),
    defaultValues: defaultPayment,
  })
  const paymentMethod = form.watch('payment_method')
  const editPaymentMethod = editForm.watch('payment_method')
  const paymentQRCodeID = form.watch('payment_qr_code_id')
  const editPaymentQRCodeID = editForm.watch('payment_qr_code_id')
  const credits = Number(form.watch('credits') || 0)
  const rate = overview.data?.rate_cny_cents_per_credit ?? 95
  const payout = Number.isFinite(credits) ? credits * rate : 0
  const canSubmit =
    !overview.isError &&
    credits >= (overview.data?.minimum_credits ?? 100) &&
    credits <= (overview.data?.reclaimable_credits ?? 0)

  useEffect(
    () => () => {
      if (qrPreview) URL.revokeObjectURL(qrPreview)
      if (editQRPreview) URL.revokeObjectURL(editQRPreview)
    },
    [editQRPreview, qrPreview]
  )

  useEffect(() => {
    const savedPayment = overview.data?.saved_payment
    if (!savedPayment || form.formState.isDirty) return
    form.reset({
      ...savedPayment,
      credits: Number(form.getValues('credits') || 100),
    })
  }, [form, form.formState.isDirty, overview.data?.saved_payment])

  const submit = form.handleSubmit((values) => {
    if (!canSubmit) {
      toast.error(t('申请金额超过当前可回收收益'))
      return
    }
    const payload = {
      credits: values.credits,
      payment_method: values.payment_method,
      payee_name: values.payee_name.trim(),
      payment_account: values.payment_account.trim(),
      payment_bank_name: values.payment_bank_name.trim(),
      payment_qr_code_id: values.payment_qr_code_id,
    }
    const signature = JSON.stringify(payload)
    if (operation.current?.signature !== signature) {
      operation.current = { signature, id: crypto.randomUUID() }
    }
    create.mutate(
      { operation_id: operation.current.id, ...payload },
      {
        onSuccess: () => {
          operation.current = null
          form.reset({ ...normalizedPayment(values), credits: 100 })
          setQRPreview('')
          toast.success(t('回收申请已提交'))
        },
        onError: (error) =>
          toast.error(
            error instanceof Error ? error.message : t('回收申请提交失败')
          ),
      }
    )
  })

  const startEdit = (item: OwnerWithdrawal) => {
    setEditing(item)
    const savedPayment = overview.data?.saved_payment
    editForm.reset(
      savedPayment ?? {
        payment_method: item.payment_method,
        payee_name: item.payee_name,
        payment_account: '',
        payment_bank_name: '',
        payment_qr_code_id: '',
      }
    )
  }

  const submitEdit = editForm.handleSubmit((values) => {
    if (!editing) return
    updatePayment.mutate(
      { id: editing.id, payment: normalizedPayment(values) },
      {
        onSuccess: () => {
          setEditing(null)
          toast.success(t('收款信息已更新'))
        },
        onError: (error) =>
          toast.error(
            error instanceof Error ? error.message : t('收款信息更新失败')
          ),
      }
    )
  })

  const uploadPaymentQRCode = (file: File, editingForm: boolean) => {
    if (file.size > 2 * 1024 * 1024) {
      toast.error(t('收款码图片不能超过 2 MiB'))
      return
    }
    if (!['image/png', 'image/jpeg', 'image/webp'].includes(file.type)) {
      toast.error(t('收款码仅支持 PNG、JPEG 或 WebP 图片'))
      return
    }
    uploadQRCode.mutate(file, {
      onSuccess: ({ id }) => {
        const preview = URL.createObjectURL(file)
        if (editingForm) {
          if (editQRPreview) URL.revokeObjectURL(editQRPreview)
          setEditQRPreview(preview)
          editForm.setValue('payment_qr_code_id', id, {
            shouldDirty: true,
            shouldValidate: true,
          })
        } else {
          if (qrPreview) URL.revokeObjectURL(qrPreview)
          setQRPreview(preview)
          form.setValue('payment_qr_code_id', id, {
            shouldDirty: true,
            shouldValidate: true,
          })
        }
      },
      onError: (error) =>
        toast.error(
          error instanceof Error ? error.message : t('收款码上传失败')
        ),
    })
  }

  const statusLabels = useMemo<Record<OwnerWithdrawalStatus, string>>(
    () => ({
      reclaiming: t('额度扣减中'),
      pending_transfer: t('待人工转账'),
      needs_update: t('收款信息需修改'),
      paid: t('已回收'),
      failed: t('申请失败'),
    }),
    [t]
  )

  return (
    <section className='border-border border-t px-4 py-5 sm:px-5'>
      <Collapsible open={withdrawalOpen} onOpenChange={setWithdrawalOpen}>
        <CollapsibleTrigger
          render={
            <button
              type='button'
              className='focus-visible:ring-ring flex w-full items-center justify-between gap-4 text-left outline-none focus-visible:ring-2'
            />
          }
        >
          <div>
            <h3 className='font-semibold'>{t('渠道收益回收')}</h3>
            <div className='mt-1 flex flex-wrap gap-x-4 gap-y-1 text-sm tabular-nums'>
              <span>
                {t('当前可回收')}{' '}
                <strong>
                  {overview.isPending
                    ? t('加载中…')
                    : `${overview.data?.reclaimable_credits ?? 0} credits`}
                </strong>
              </span>
              <span className='text-muted-foreground'>
                {formatCNY(overview.data?.reclaimable_amount_cny_cents ?? 0)}
              </span>
              <span className='text-muted-foreground'>
                1 credit = {formatCNY(rate)}
              </span>
            </div>
          </div>
          <ChevronDown
            className={`text-muted-foreground size-4 shrink-0 transition-transform ${withdrawalOpen ? 'rotate-180' : ''}`}
          />
        </CollapsibleTrigger>

        <CollapsibleContent>
          <div className='mt-4 flex justify-end'>
            <Button
              type='button'
              variant='outline'
              size='icon'
              onClick={() => {
                void overview.refetch()
                void requests.refetch()
              }}
              aria-label={t('刷新回收记录')}
              title={t('刷新回收记录')}
            >
              <RefreshCw
                className={
                  overview.isFetching || requests.isFetching
                    ? 'animate-spin'
                    : ''
                }
              />
            </Button>
          </div>

          <form
            onSubmit={submit}
            className='mt-3 grid gap-3 md:grid-cols-2 xl:grid-cols-6 xl:items-end'
          >
            <FormField
              label={t('回收 credits')}
              error={form.formState.errors.credits}
            >
              <Input
                type='number'
                min={overview.data?.minimum_credits ?? 100}
                max={overview.data?.reclaimable_credits ?? undefined}
                step='1'
                {...form.register('credits', { valueAsNumber: true })}
              />
            </FormField>
            <FormField label={t('收款方式')}>
              <NativeSelect
                className='w-full'
                {...form.register('payment_method')}
              >
                <option value='alipay'>{t('支付宝')}</option>
                <option value='bank_card'>{t('银行卡')}</option>
                <option value='qr_code'>{t('收款码')}</option>
              </NativeSelect>
            </FormField>
            <FormField
              label={t('收款人姓名')}
              error={form.formState.errors.payee_name}
            >
              <Input autoComplete='name' {...form.register('payee_name')} />
            </FormField>
            {paymentMethod === 'bank_card' && (
              <FormField
                label={t('开户银行')}
                error={form.formState.errors.payment_bank_name}
              >
                <Input {...form.register('payment_bank_name')} />
              </FormField>
            )}
            {paymentMethod !== 'qr_code' ? (
              <FormField
                label={
                  paymentMethod === 'bank_card'
                    ? t('银行卡号')
                    : t('支付宝账号')
                }
                error={form.formState.errors.payment_account}
              >
                <Input
                  autoComplete='off'
                  inputMode={paymentMethod === 'bank_card' ? 'numeric' : 'text'}
                  {...form.register('payment_account')}
                />
              </FormField>
            ) : (
              <QRCodeUploadField
                label={t('收款码')}
                preview={qrPreview}
                selected={paymentQRCodeID !== ''}
                uploading={uploadQRCode.isPending}
                error={form.formState.errors.payment_qr_code_id}
                onFile={(file) => uploadPaymentQRCode(file, false)}
              />
            )}
            <div className='flex min-w-0 flex-col gap-1'>
              <span className='text-muted-foreground text-xs'>
                {t('回收价格')}
              </span>
              <strong className='truncate text-lg tabular-nums'>
                {formatCNY(payout)}
              </strong>
              <Button
                type='submit'
                disabled={
                  !canSubmit ||
                  create.isPending ||
                  overview.isPending ||
                  uploadQRCode.isPending
                }
              >
                <Send />
                {create.isPending ? t('提交中') : t('提交回收申请')}
              </Button>
            </div>
          </form>

          {overview.isError && (
            <p role='alert' className='text-destructive mt-3 text-sm'>
              {t('可回收金额加载失败')}
            </p>
          )}

          {editing && (
            <form
              onSubmit={submitEdit}
              className='border-border bg-muted/20 mt-5 grid gap-3 border-y py-4 md:grid-cols-2 xl:grid-cols-5 xl:items-end'
            >
              <FormField label={t('收款方式')}>
                <NativeSelect
                  className='w-full'
                  {...editForm.register('payment_method')}
                >
                  <option value='alipay'>{t('支付宝')}</option>
                  <option value='bank_card'>{t('银行卡')}</option>
                  <option value='qr_code'>{t('收款码')}</option>
                </NativeSelect>
              </FormField>
              <FormField
                label={t('收款人姓名')}
                error={editForm.formState.errors.payee_name}
              >
                <Input {...editForm.register('payee_name')} />
              </FormField>
              {editPaymentMethod === 'bank_card' && (
                <FormField
                  label={t('开户银行')}
                  error={editForm.formState.errors.payment_bank_name}
                >
                  <Input {...editForm.register('payment_bank_name')} />
                </FormField>
              )}
              {editPaymentMethod !== 'qr_code' ? (
                <FormField
                  label={
                    editPaymentMethod === 'bank_card'
                      ? t('银行卡号')
                      : t('支付宝账号')
                  }
                  error={editForm.formState.errors.payment_account}
                >
                  <Input {...editForm.register('payment_account')} />
                </FormField>
              ) : (
                <QRCodeUploadField
                  label={t('收款码')}
                  preview={editQRPreview}
                  selected={editPaymentQRCodeID !== ''}
                  uploading={uploadQRCode.isPending}
                  error={editForm.formState.errors.payment_qr_code_id}
                  onFile={(file) => uploadPaymentQRCode(file, true)}
                />
              )}
              <div className='flex gap-2'>
                <Button type='submit' disabled={updatePayment.isPending}>
                  <Send />
                  {t('保存')}
                </Button>
                <Button
                  type='button'
                  variant='outline'
                  onClick={() => setEditing(null)}
                >
                  {t('取消')}
                </Button>
              </div>
            </form>
          )}

          <div className='border-border mt-5 overflow-x-auto border-t'>
            <table className='w-full min-w-[720px] text-sm'>
              <thead className='text-muted-foreground'>
                <tr>
                  <th className='py-2.5 text-left font-medium'>
                    {t('申请时间')}
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
                {(requests.data?.items ?? []).map((item) => (
                  <tr key={item.id}>
                    <td className='py-3 pr-4 tabular-nums'>
                      {dayjs(item.created_at).format('YYYY-MM-DD HH:mm')}
                    </td>
                    <td className='py-3 text-right font-medium tabular-nums'>
                      {item.requested_credits}
                    </td>
                    <td className='py-3 text-right font-medium tabular-nums'>
                      {formatCNY(item.payout_amount_cents)}
                    </td>
                    <td className='py-3 pl-4'>
                      {t(paymentMethodLabel(item.payment_method))} ·{' '}
                      {item.payee_name}
                      {item.has_payment_qr_code
                        ? ` · ${t('已上传')}`
                        : ` · ****${item.payment_details_tail}`}
                    </td>
                    <td className='py-3 pl-4'>
                      <span className={statusClass(item.status)}>
                        {statusLabels[item.status]}
                      </span>
                      {(item.admin_note || item.failure_message) && (
                        <div className='text-muted-foreground mt-1 max-w-64 text-xs whitespace-normal'>
                          {item.admin_note || item.failure_message}
                        </div>
                      )}
                    </td>
                    <td className='py-3 text-right'>
                      {item.status === 'needs_update' && (
                        <Button
                          type='button'
                          variant='outline'
                          size='sm'
                          onClick={() => startEdit(item)}
                        >
                          <Pencil />
                          {t('修改')}
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {!requests.isPending &&
              (requests.data?.items.length ?? 0) === 0 && (
                <div className='text-muted-foreground py-8 text-center text-sm'>
                  <Banknote className='mx-auto mb-2' />
                  {t('暂无回收申请')}
                </div>
              )}
          </div>
        </CollapsibleContent>
      </Collapsible>
    </section>
  )
}

function FormField(props: {
  label: string
  error?: { message?: string }
  children: React.ReactNode
}) {
  return (
    <label className='min-w-0 space-y-1.5'>
      <span className='text-sm font-medium'>{props.label}</span>
      {props.children}
      {props.error && (
        <span className='text-destructive block text-xs'>{props.label}</span>
      )}
    </label>
  )
}

function normalizedPayment(values: PaymentForm): OwnerWithdrawalPaymentInput {
  return {
    payment_method: values.payment_method as OwnerWithdrawalPaymentMethod,
    payee_name: values.payee_name.trim(),
    payment_account: values.payment_account.trim(),
    payment_bank_name: values.payment_bank_name.trim(),
    payment_qr_code_id: values.payment_qr_code_id,
  }
}

function QRCodeUploadField(props: {
  label: string
  preview: string
  selected: boolean
  uploading: boolean
  error?: { message?: string }
  onFile: (file: File) => void
}) {
  const { t } = useTranslation()
  return (
    <label className='min-w-0 space-y-1.5'>
      <span className='text-sm font-medium'>{props.label}</span>
      <span className='border-input bg-card relative flex h-11 items-center gap-2 rounded-lg border px-3'>
        <ImageUp className='size-4 shrink-0' />
        <span className='min-w-0 flex-1 truncate text-sm'>
          {props.uploading
            ? t('加载中…')
            : props.selected
              ? t('已上传')
              : props.label}
        </span>
        {props.preview && (
          <img src={props.preview} alt='' className='size-8 object-contain' />
        )}
        <input
          type='file'
          accept='image/png,image/jpeg,image/webp'
          className='absolute inset-0 cursor-pointer opacity-0'
          disabled={props.uploading}
          onChange={(event) => {
            const file = event.currentTarget.files?.[0]
            if (file) props.onFile(file)
            event.currentTarget.value = ''
          }}
        />
      </span>
      {props.error && (
        <span className='text-destructive block text-xs'>{props.label}</span>
      )}
    </label>
  )
}

function paymentMethodLabel(method: OwnerWithdrawalPaymentMethod) {
  if (method === 'bank_card') return '银行卡'
  if (method === 'qr_code') return '收款码'
  return '支付宝'
}

function formatCNY(cents: number) {
  return new Intl.NumberFormat('zh-CN', {
    style: 'currency',
    currency: 'CNY',
    minimumFractionDigits: 2,
  }).format(cents / 100)
}

function statusClass(status: OwnerWithdrawalStatus) {
  if (status === 'paid') return 'text-[var(--telemetry-emerald)] font-medium'
  if (status === 'failed' || status === 'needs_update')
    return 'text-destructive font-medium'
  return 'text-foreground font-medium'
}
