import { Link } from '@tanstack/react-router'
import { Gift, Settings2, Wallet } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { BalanceBlindBoxSettingsAdmin } from './balance-blind-box-settings-admin'

export function BlindBoxOperationsPanel() {
  return (
    <div className='space-y-4'>
      <div className='bg-muted/35 rounded-lg border p-4'>
        <div className='flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between'>
          <div className='space-y-2'>
            <div className='flex items-center gap-2 text-sm font-semibold'>
              <Gift className='text-primary size-4' aria-hidden='true' />
              统一盲盒运营
            </div>
            <p className='text-muted-foreground max-w-2xl text-sm leading-6'>
              人民币与通用额度入口共用同一售价、每日限购、普通奖池和独立保底池。下方配置即为当前实际运行规则。
            </p>
          </div>
          <div className='flex flex-wrap gap-2'>
            <Button
              variant='outline'
              size='sm'
              render={
                <Link
                  to='/system-settings/billing/$section'
                  params={{ section: 'blind-box' }}
                />
              }
            >
              <Settings2 className='size-4' />
              系统配置
            </Button>
            <Button
              size='sm'
              onClick={() => window.location.assign('/blind-box')}
            >
              <Wallet className='size-4' />
              打开盲盒页
            </Button>
          </div>
        </div>
      </div>
      <BalanceBlindBoxSettingsAdmin />
    </div>
  )
}
