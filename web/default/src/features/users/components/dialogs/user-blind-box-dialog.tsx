import { useCallback, useEffect, useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { ConfirmDialog } from '@/components/confirm-dialog'
import type { BlindBoxSelfData } from '@/features/wallet/types'
import { getUserBlindBoxOverview, grantUserBlindBoxes } from '../../api'

function formatTime(timestamp?: number): string {
  if (!timestamp) return '-'
  return new Date(timestamp * 1000).toLocaleString()
}

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  user: { id: number; username?: string; usedQuota?: number } | null
}

export function UserBlindBoxDialog(props: Props) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [data, setData] = useState<BlindBoxSelfData | null>(null)
  const [grantQuantity, setGrantQuantity] = useState('1')
  const [grantReason, setGrantReason] = useState('')
  const [granting, setGranting] = useState(false)
  const [grantConfirmOpen, setGrantConfirmOpen] = useState(false)
  const [grantIdempotencyKey, setGrantIdempotencyKey] = useState('')

  const loadData = useCallback(async () => {
    if (!props.user?.id) return
    setLoading(true)
    try {
      const response = await getUserBlindBoxOverview(props.user.id)
      if (response.success && response.data) {
        setData(response.data)
      } else {
        toast.error(response.message || t('Loading failed'))
      }
    } catch {
      toast.error(t('Loading failed'))
    } finally {
      setLoading(false)
    }
  }, [props.user?.id, t])

  useEffect(() => {
    if (props.open) {
      void loadData()
    }
  }, [loadData, props.open])

  const validateGrant = () => {
    if (!props.user?.id) return
    const quantity = Number(grantQuantity)
    if (!Number.isInteger(quantity) || quantity < 1 || quantity > 1000) {
      toast.error(t('Quantity must be between 1 and 1000'))
      return false
    }
    setGrantIdempotencyKey(createIdempotencyKey())
    setGrantConfirmOpen(true)
    return true
  }

  const handleGrant = async () => {
    if (!props.user?.id) return
    const quantity = Number(grantQuantity)
    const reason = grantReason.trim()

    setGranting(true)
    try {
      const response = await grantUserBlindBoxes(props.user.id, {
        quantity,
        reason,
        idempotency_key: grantIdempotencyKey || createIdempotencyKey(),
      })
      if (!response.success) {
        toast.error(response.message || t('Grant blind boxes failed'))
        return
      }
      toast.success(t('Blind boxes granted successfully'))
      setGrantQuantity('1')
      setGrantReason('')
      setGrantIdempotencyKey('')
      setGrantConfirmOpen(false)
      await loadData()
    } catch {
      toast.error(t('Grant blind boxes failed'))
    } finally {
      setGranting(false)
    }
  }

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className='overflow-y-auto sm:max-w-3xl'>
        <SheetHeader>
          <SheetTitle>{t('Blind Box Management')}</SheetTitle>
          <SheetDescription>
            {props.user?.username || '-'} (ID: {props.user?.id || '-'})
          </SheetDescription>
        </SheetHeader>

        <div className='mt-4 space-y-4'>
          <div className='flex items-start justify-between gap-3 rounded-lg border p-4'>
            <div className='space-y-1 text-sm'>
              <div className='font-medium'>{t('Quota metric note')}</div>
              <div className='text-muted-foreground'>
                {t(
                  'Dashboard used quota is the user-wide cumulative consumption. Subscription used quota is package-local usage. Blind box rewards are credited directly to the matching wallet.'
                )}
              </div>
            </div>
            <Button
              variant='outline'
              size='sm'
              onClick={() => void loadData()}
              disabled={loading}
            >
              <RefreshCw
                className={cn('mr-1 h-4 w-4', loading && 'animate-spin')}
              />
              {t('Refresh')}
            </Button>
          </div>

          <div className='rounded-lg border p-4'>
            <div className='text-sm font-medium'>{t('Grant blind boxes')}</div>
            <div className='text-muted-foreground mt-1 text-sm'>
              {t(
                'Granted boxes do not charge the user. The user can open them from the blind box page.'
              )}
            </div>
            <div className='mt-4 grid gap-4 sm:grid-cols-[140px_minmax(0,1fr)]'>
              <div className='space-y-2'>
                <Label htmlFor='blind-box-grant-quantity'>
                  {t('Quantity')}
                </Label>
                <Input
                  id='blind-box-grant-quantity'
                  type='number'
                  min={1}
                  max={1000}
                  step={1}
                  value={grantQuantity}
                  onChange={(event) => setGrantQuantity(event.target.value)}
                  disabled={granting}
                />
              </div>
              <div className='space-y-2'>
                <Label htmlFor='blind-box-grant-reason'>
                  {t('Grant reason')}
                </Label>
                <Textarea
                  id='blind-box-grant-reason'
                  value={grantReason}
                  onChange={(event) => setGrantReason(event.target.value)}
                  placeholder={t('Example: campaign reward')}
                  maxLength={255}
                  className='min-h-9 resize-y'
                  disabled={granting}
                />
              </div>
            </div>
            <Button
              className='mt-4'
              onClick={validateGrant}
              disabled={granting}
            >
              {t('Grant blind boxes')}
            </Button>
          </div>

          <div className='grid gap-3 md:grid-cols-2 xl:grid-cols-4'>
            <MetricCard
              label={t('Feature status')}
              value={data?.enabled ? t('Enabled') : t('Disabled')}
            />
            <MetricCard
              label={t('Available boxes')}
              value={String(data?.overview?.available_boxes || 0)}
            />
            <MetricCard
              label={t('Wallet balance')}
              value={formatQuota(data?.overview?.remaining_quota || 0)}
            />
            <MetricCard
              label={t('Claude balance')}
              value={formatQuota(data?.overview?.claude_quota || 0)}
            />
            <MetricCard
              label={t('User used quota')}
              value={formatQuota(props.user?.usedQuota || 0)}
            />
            <MetricCard
              label={t('Pending boxes')}
              value={String(data?.overview?.pending_boxes || 0)}
            />
            <MetricCard
              label={t('Pity progress')}
              value={`${data?.overview?.pity_progress || 0}/${data?.pity_threshold || 0}`}
            />
            <MetricCard
              label={t('Subscription prize')}
              value={`${((data?.subscription_prize_probability || 0) * 100).toFixed(2)}%`}
            />
          </div>

          <div className='grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]'>
            <div className='rounded-lg border'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('Reward')}</TableHead>
                    <TableHead>{t('Tier')}</TableHead>
                    <TableHead>{t('Type')}</TableHead>
                    <TableHead>{t('Created at')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {loading ? (
                    <TableRow>
                      <TableCell colSpan={4} className='py-8 text-center'>
                        {t('Loading...')}
                      </TableCell>
                    </TableRow>
                  ) : (data?.overview?.recent_records?.length || 0) === 0 ? (
                    <TableRow>
                      <TableCell
                        colSpan={4}
                        className='text-muted-foreground py-8 text-center'
                      >
                        {t('No blind box records yet')}
                      </TableCell>
                    </TableRow>
                  ) : (
                    (data?.overview?.recent_records || []).map((record) => (
                      <TableRow key={record.id}>
                        <TableCell>
                          <div className='flex items-center gap-2'>
                            <span className='font-medium'>
                              {record.reward_title}
                            </span>
                            {record.is_pity ? (
                              <Badge variant='outline'>{t('Pity')}</Badge>
                            ) : null}
                          </div>
                        </TableCell>
                        <TableCell>{record.reward_tier || '-'}</TableCell>
                        <TableCell>
                          {record.reward_type === 'subscription'
                            ? t('Subscription')
                            : `${Number(record.reward_usd || 0).toFixed(2)} USD`}
                        </TableCell>
                        <TableCell>{formatTime(record.create_time)}</TableCell>
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
            </div>

            <div className='space-y-4'>
              <div className='rounded-lg border p-4'>
                <div className='text-sm font-medium'>{t('Grant history')}</div>
                <div className='mt-3 space-y-3'>
                  {(data?.grants || []).length === 0 ? (
                    <div className='text-muted-foreground text-sm'>
                      {t('No grant records yet')}
                    </div>
                  ) : (
                    (data?.grants || []).map((grant) => (
                      <div
                        key={grant.id}
                        className='border-border/60 border-b pb-3 last:border-0 last:pb-0'
                      >
                        <div className='flex items-center justify-between gap-3 text-sm'>
                          <span className='font-medium'>
                            {t('{{count}} blind boxes', {
                              count: grant.quantity,
                            })}
                          </span>
                          <span className='text-muted-foreground text-xs'>
                            {formatTime(grant.created_at)}
                          </span>
                        </div>
                        <div className='text-muted-foreground mt-1 text-xs'>
                          {grant.reason}
                        </div>
                      </div>
                    ))
                  )}
                </div>
              </div>
              <div className='rounded-lg border p-4'>
                <div className='text-sm font-medium'>{t('Current rules')}</div>
                <div className='text-muted-foreground mt-3 space-y-2 text-sm'>
                  <div>
                    {t('Unit price')}: {(data?.unit_price || 0).toFixed(2)} USD
                  </div>
                  <div>
                    {t('Daily purchase limit')}: {data?.daily_limit || 0}
                  </div>
                  <div>
                    {t('Monthly purchase limit')}: {data?.monthly_limit || 0}
                  </div>
                  <div>
                    {t('Daily open limit')}: {data?.daily_open_limit || 0}
                  </div>
                  <div>
                    {t('Pity guarantee')}:{' '}
                    {(data?.pity_guarantee_usd || 0).toFixed(2)} USD
                  </div>
                </div>
              </div>

              <div className='rounded-lg border p-4'>
                <div className='text-sm font-medium'>{t('Reward tiers')}</div>
                <div className='mt-3 space-y-2 text-sm'>
                  {(data?.tiers || []).map((tier) => (
                    <div
                      key={tier.name}
                      className='flex items-center justify-between gap-3'
                    >
                      <span className='text-muted-foreground'>
                        {tier.name} - {tier.min_usd}-{tier.max_usd} USD
                      </span>
                      <span>{(tier.probability * 100).toFixed(2)}%</span>
                    </div>
                  ))}
                  <div className='flex items-center justify-between gap-3 border-t pt-2 font-medium'>
                    <span>
                      {data?.subscription_plan_title || t('Subscription')}
                    </span>
                    <span>
                      {(
                        (data?.subscription_prize_probability || 0) * 100
                      ).toFixed(2)}
                      %
                    </span>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </SheetContent>
      <ConfirmDialog
        open={grantConfirmOpen}
        onOpenChange={setGrantConfirmOpen}
        title={t('Confirm blind box grant')}
        desc={t('Grant {{count}} blind boxes to {{user}}? Reason: {{reason}}', {
          count: grantQuantity,
          user: props.user?.username || props.user?.id || '-',
          reason: grantReason.trim() || t('None'),
        })}
        confirmText={granting ? t('Granting...') : t('Confirm grant')}
        isLoading={granting}
        handleConfirm={() => void handleGrant()}
      />
    </Sheet>
  )
}

function createIdempotencyKey() {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
    return crypto.randomUUID()
  }
  return `${Date.now()}-${Math.random()}`
}

function MetricCard(props: { label: string; value: string }) {
  return (
    <div className='rounded-lg border p-3'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div className='mt-1 text-sm font-semibold break-all'>{props.value}</div>
    </div>
  )
}
