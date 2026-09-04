import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { KeyRound, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/native-select'
import { useMarketplaceMutations, useMarketplaceTokens } from '../hooks'

export function TokenBindPanel(props: { groupId: string; compact?: boolean }) {
  const { t } = useTranslation()
  const tokens = useMarketplaceTokens()
  const mutations = useMarketplaceMutations()
  const [tokenId, setTokenId] = useState(0)

  const bind = async () => {
    try {
      const result = await mutations.bind.mutateAsync({ groupId: props.groupId, tokenId })
      toast.success(tokenId ? t('Token 已绑定到市场分组') : t('已创建并绑定新的市场分组 Key'))
      if (!tokenId && result?.token_id) setTokenId(result.token_id)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('绑定失败'))
    }
  }

  return (
    <div className='border-border bg-muted/35 flex flex-col gap-3 rounded-md border p-3 sm:flex-row sm:items-center sm:justify-between'>
      <div className='min-w-0'>
        <div className='flex items-center gap-2 text-sm font-medium'>
          <KeyRound className='size-4' />
          {t('绑定到指定 Token')}
        </div>
        <p className='text-muted-foreground mt-1 text-xs leading-5'>
          {t('仅影响所选 API Key；系统按资金源顺序使用套餐或通用余额。')}
        </p>
      </div>
      <div className='flex items-center gap-2'>
        <NativeSelect
          value={String(tokenId)}
          onChange={(event) => setTokenId(Number(event.target.value))}
          disabled={tokens.isLoading}
          aria-label={t('选择 Token')}
        >
          <option value='0'>{t('选择 Token')}</option>
          {(tokens.data ?? []).map((token) => (
            <option key={token.id} value={token.id}>
              {token.name}
            </option>
          ))}
        </NativeSelect>
        <Button
          size='sm'
          onClick={() => void bind()}
          disabled={mutations.bind.isPending || tokens.isLoading}
        >
          {mutations.bind.isPending && (
            <Loader2 className='size-4 animate-spin' />
          )}
          {tokenId ? t('确认绑定') : t('创建并绑定')}
        </Button>
        <Button variant='outline' size='sm' render={<Link to='/keys' />}>
          {t('新建此分组 Key')}
        </Button>
      </div>
    </div>
  )
}
