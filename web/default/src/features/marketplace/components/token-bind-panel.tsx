import { useState } from 'react'
import { KeyRound, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/native-select'
import { useMarketplaceMutations, useMarketplaceTokens } from '../hooks'

export function TokenBindPanel(props: { groupId: string }) {
  const { t } = useTranslation()
  const tokens = useMarketplaceTokens()
  const mutations = useMarketplaceMutations()
  const [tokenId, setTokenId] = useState(0)

  const bind = async () => {
    if (!tokenId) return
    try {
      await mutations.bind.mutateAsync({ groupId: props.groupId, tokenId })
      toast.success(t('Token 已绑定到市场分组'))
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('绑定失败'))
    }
  }

  return (
    <div className='bg-muted/35 flex flex-col gap-3 rounded-md p-3 sm:flex-row sm:items-center sm:justify-between'>
      <div>
        <div className='flex items-center gap-2 text-sm font-medium'>
          <KeyRound className='size-4' />
          {t('绑定到指定 Token')}
        </div>
        <p className='text-muted-foreground mt-1 text-xs leading-5'>
          {t('仅影响所选 Token。用户渠道固定消耗通用额度，故障时不跨池回退。')}
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
          disabled={!tokenId || mutations.bind.isPending}
        >
          {mutations.bind.isPending && (
            <Loader2 className='size-4 animate-spin' />
          )}
          {t('确认绑定')}
        </Button>
      </div>
    </div>
  )
}
