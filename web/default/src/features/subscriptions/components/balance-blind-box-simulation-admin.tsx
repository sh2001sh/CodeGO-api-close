import { useState } from 'react'
import { FlaskConical, Search, Square } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import {
  getUser,
  getUserBlindBoxOverview,
  startUserBalanceBlindBoxSimulation,
  stopUserBalanceBlindBoxSimulation,
} from '@/features/users/api'
import type { User } from '@/features/users/types'
import type { BalanceBlindBoxSimulationOverview } from '@/features/wallet/types'

export function BalanceBlindBoxSimulationAdmin() {
  const [userId, setUserId] = useState('')
  const [duration, setDuration] = useState('60')
  const [reason, setReason] = useState('')
  const [targetUser, setTargetUser] = useState<User | null>(null)
  const [simulation, setSimulation] =
    useState<BalanceBlindBoxSimulationOverview>()
  const [busy, setBusy] = useState(false)

  const parsedUserId = Number(userId)
  const active = Boolean(
    simulation?.active && (simulation.expires_at || 0) > Date.now() / 1000
  )

  const loadTarget = async () => {
    if (!Number.isInteger(parsedUserId) || parsedUserId <= 0) {
      toast.error('请输入有效的用户 ID')
      return
    }
    setBusy(true)
    try {
      const [userResponse, overviewResponse] = await Promise.all([
        getUser(parsedUserId),
        getUserBlindBoxOverview(parsedUserId),
      ])
      if (!userResponse.success || !userResponse.data) {
        throw new Error(userResponse.message || '用户不存在')
      }
      if (!overviewResponse.success || !overviewResponse.data) {
        throw new Error(overviewResponse.message || '读取盲盒状态失败')
      }
      setTargetUser(userResponse.data)
      setSimulation(overviewResponse.data.balance_blind_box?.simulation)
    } catch (error) {
      setTargetUser(null)
      setSimulation(undefined)
      toast.error(error instanceof Error ? error.message : '查询用户失败')
    } finally {
      setBusy(false)
    }
  }

  const startSimulation = async () => {
    if (!targetUser || targetUser.id !== parsedUserId) return
    const durationMinutes = Number(duration)
    if (
      !Number.isInteger(durationMinutes) ||
      durationMinutes < 1 ||
      durationMinutes > 10080
    ) {
      toast.error('模拟时长必须在 1 到 10080 分钟之间')
      return
    }
    setBusy(true)
    try {
      const response = await startUserBalanceBlindBoxSimulation(targetUser.id, {
        duration_minutes: durationMinutes,
        reason: reason.trim(),
      })
      if (!response.success) throw new Error(response.message || '开启失败')
      toast.success(
        `已为 ${targetUser.username} 开启 ${durationMinutes} 分钟模拟`
      )
      await loadTargetState(targetUser.id, setSimulation)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : '开启失败')
    } finally {
      setBusy(false)
    }
  }

  const stopSimulation = async () => {
    if (!targetUser) return
    setBusy(true)
    try {
      const response = await stopUserBalanceBlindBoxSimulation(targetUser.id)
      if (!response.success) throw new Error(response.message || '结束失败')
      toast.success(`已结束 ${targetUser.username} 的余额盲盒模拟`)
      await loadTargetState(targetUser.id, setSimulation)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : '结束失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className='rounded-2xl border p-4'>
      <div className='flex items-start gap-3'>
        <div className='flex size-9 shrink-0 items-center justify-center rounded-lg bg-teal-500/10 text-teal-700 dark:text-teal-300'>
          <FlaskConical className='size-4' />
        </div>
        <div>
          <h3 className='text-sm font-semibold'>限时余额盲盒模拟</h3>
          <p className='text-muted-foreground mt-1 max-w-3xl text-sm leading-6'>
            管理员指定用户和有效时间。用户在原余额盲盒页面模拟抽取，不扣真实余额、不发放奖励，也不影响真实首抽和保底。
          </p>
        </div>
      </div>

      <div className='mt-4 grid gap-4 lg:grid-cols-[160px_160px_minmax(240px,1fr)_auto] lg:items-end'>
        <div className='space-y-2'>
          <Label htmlFor='simulation-user-id'>用户 ID</Label>
          <Input
            id='simulation-user-id'
            type='number'
            min={1}
            value={userId}
            disabled={busy}
            onChange={(event) => {
              setUserId(event.target.value)
              setTargetUser(null)
              setSimulation(undefined)
            }}
          />
        </div>
        <div className='space-y-2'>
          <Label htmlFor='simulation-duration'>时长（分钟）</Label>
          <Input
            id='simulation-duration'
            type='number'
            min={1}
            max={10080}
            value={duration}
            disabled={busy || active}
            onChange={(event) => setDuration(event.target.value)}
          />
        </div>
        <div className='space-y-2'>
          <Label htmlFor='simulation-reason'>测试原因</Label>
          <Textarea
            id='simulation-reason'
            value={reason}
            maxLength={255}
            disabled={busy || active}
            className='min-h-9 resize-y'
            placeholder='例如：验证余额盲盒概率分布'
            onChange={(event) => setReason(event.target.value)}
          />
        </div>
        <Button
          type='button'
          variant='outline'
          disabled={busy}
          onClick={() => void loadTarget()}
        >
          <Search className='size-4' />
          查询用户
        </Button>
      </div>

      {targetUser ? (
        <div className='bg-muted/45 mt-4 rounded-xl p-4'>
          <div className='flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between'>
            <div>
              <div className='text-sm font-medium'>
                {targetUser.username} · ID {targetUser.id}
              </div>
              <div className='text-muted-foreground mt-1 text-xs'>
                {active
                  ? `模拟有效至 ${formatSimulationTime(simulation?.expires_at)}`
                  : '当前未开启模拟权限'}
              </div>
            </div>
            {active ? (
              <Button
                type='button'
                size='sm'
                variant='outline'
                disabled={busy}
                onClick={() => void stopSimulation()}
              >
                <Square className='size-3.5' />
                提前结束
              </Button>
            ) : (
              <Button
                type='button'
                size='sm'
                disabled={busy}
                onClick={() => void startSimulation()}
              >
                <FlaskConical className='size-4' />
                开启模拟
              </Button>
            )}
          </div>

          {active ? (
            <div className='mt-4 grid grid-cols-2 gap-3 text-sm sm:grid-cols-4'>
              <SimulationValue
                label='累计抽数'
                value={`${simulation?.draw_count || 0} 抽`}
              />
              <SimulationValue
                label='模拟投入'
                value={`$${(simulation?.simulated_cost_usd || 0).toFixed(2)}`}
              />
              <SimulationValue
                label='奖励价值'
                value={`$${(simulation?.simulated_reward_value_usd || 0).toFixed(2)}`}
              />
              <SimulationValue
                label='模拟净值'
                value={`${(simulation?.simulated_net_usd || 0) >= 0 ? '+' : ''}$${(simulation?.simulated_net_usd || 0).toFixed(2)}`}
              />
            </div>
          ) : null}
        </div>
      ) : null}
    </section>
  )
}

async function loadTargetState(
  userId: number,
  setSimulation: (value?: BalanceBlindBoxSimulationOverview) => void
) {
  const response = await getUserBlindBoxOverview(userId)
  if (!response.success || !response.data) {
    throw new Error(response.message || '刷新模拟状态失败')
  }
  setSimulation(response.data.balance_blind_box?.simulation)
}

function SimulationValue(props: { label: string; value: string }) {
  return (
    <div>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div className='mt-1 font-medium tabular-nums'>{props.value}</div>
    </div>
  )
}

function formatSimulationTime(timestamp?: number) {
  if (!timestamp) return '-'
  return new Date(timestamp * 1000).toLocaleString()
}
