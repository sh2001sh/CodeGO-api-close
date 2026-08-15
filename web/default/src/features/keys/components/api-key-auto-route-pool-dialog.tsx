import { useState } from 'react'
import { Route } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { MarketplaceAutoPool } from '@/features/pricing/components/marketplace-auto-pool'

export function ApiKeyAutoRoutePoolDialog() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <Button
        type='button'
        variant='outline'
        size='sm'
        className='gap-2'
        onClick={() => setOpen(true)}
      >
        <Route className='size-4' />
        {t('配置第三方 Auto 路由池')}
      </Button>
      <DialogContent className='flex max-h-[88dvh] !max-w-5xl flex-col gap-0 overflow-hidden p-0'>
        <DialogHeader className='border-b px-5 py-4 pr-12'>
          <DialogTitle>{t('配置第三方 Auto 路由池')}</DialogTitle>
          <DialogDescription>
            {t('选择第三方分组并调整优先级，保存后即可在当前 API Key 中选择第三方 Auto。')}
          </DialogDescription>
        </DialogHeader>
        <div className='min-h-0 flex-1 overflow-y-auto p-4 sm:p-5'>
          <MarketplaceAutoPool authenticated />
        </div>
      </DialogContent>
    </Dialog>
  )
}
