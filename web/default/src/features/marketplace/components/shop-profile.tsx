import { useId } from 'react'
import { z } from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import interfaceI18n from '@/i18n/config'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { getMyShop, updateMyShop, type MarketplaceShop } from '../shop-api'

const shopSchema = z.object({
  name: z.string().max(20, {
    error: () => interfaceI18n.t('标题最多 20 字，详细说明请填写下方备注'),
  }),
  description: z.string().max(300),
})

export function ShopProfile() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['marketplace-my-shop'],
    queryFn: getMyShop,
  })
  return (
    <section className='owner-shop-profile' aria-label={t('店铺设置')}>
      <h2 className='font-semibold'>{t('店铺设置')}</h2>
      {query.data && (
        <p className='text-muted-foreground mt-2 font-mono text-xs tabular-nums'>
          ID {query.data.id}
        </p>
      )}
      {query.data?.moderation_status === 'failed' && (
        <p role='alert' className='text-destructive mt-2 text-sm'>
          {t(
            '店铺公开内容未通过广告检查，已隐藏。请修改后重新运行渠道连通性测试。'
          )}
        </p>
      )}
      {query.isPending && (
        <p role='status' className='mt-3 text-sm'>
          {t('加载中')}
        </p>
      )}
      {query.isError && (
        <div role='alert' className='mt-3 text-sm'>
          <p>{query.error.message}</p>
          <Button variant='outline' onClick={() => void query.refetch()}>
            {t('重试')}
          </Button>
        </div>
      )}
      {query.data && (
        <ShopProfileForm
          key={`${query.data.name}:${query.data.description}`}
          shop={query.data}
        />
      )}
    </section>
  )
}

function ShopProfileForm(props: { shop: MarketplaceShop }) {
  const { t } = useTranslation()
  const fieldID = useId()
  const client = useQueryClient()
  const form = useForm<z.infer<typeof shopSchema>>({
    resolver: zodResolver(shopSchema),
    defaultValues: {
      name: props.shop.custom_name || '',
      description: props.shop.description,
    },
  })
  const mutation = useMutation({
    mutationFn: updateMyShop,
    onSuccess: () => {
      toast.success(t('店铺设置已保存'))
      void client.invalidateQueries({ queryKey: ['marketplace-my-shop'] })
      void client.invalidateQueries({ queryKey: ['marketplace-shops'] })
      void client.invalidateQueries({ queryKey: ['marketplace-groups'] })
    },
    onError: (error) => toast.error(error.message),
  })
  return (
    <form
      className='owner-shop-form'
      onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
    >
      <div className='space-y-1.5'>
        <label htmlFor={`${fieldID}-name`} className='text-sm font-medium'>
          {t('店铺名称')}
        </label>
        <Input
          id={`${fieldID}-name`}
          maxLength={20}
          placeholder={t('留空使用系统名称')}
          {...form.register('name')}
        />
        {form.formState.errors.name && (
          <p role='alert' className='text-destructive text-xs'>
            {form.formState.errors.name.message}
          </p>
        )}
      </div>
      <div className='space-y-1.5'>
        <label
          htmlFor={`${fieldID}-description`}
          className='text-sm font-medium'
        >
          {t('店铺介绍')}
        </label>
        <Textarea
          id={`${fieldID}-description`}
          rows={3}
          maxLength={300}
          {...form.register('description')}
        />
        {form.formState.errors.description && (
          <p role='alert' className='text-destructive text-xs'>
            {form.formState.errors.description.message}
          </p>
        )}
      </div>
      <p className='text-muted-foreground text-xs leading-5'>
        {t(
          '名称和备注禁止网址、联系方式和站外广告；连通性测试会同时检查公开内容。'
        )}
      </p>
      <Button type='submit' disabled={mutation.isPending}>
        {mutation.isPending ? t('正在保存') : t('保存店铺设置')}
      </Button>
    </form>
  )
}
