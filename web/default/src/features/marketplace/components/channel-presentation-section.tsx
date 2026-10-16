import type { UseFormReturn } from 'react-hook-form'
import { Store } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import type { ChannelFormInput } from '../lib/channel-form'
import type { MarketplaceChannel } from '../types'
import { FormField, FormSection } from './channel-form-layout'

export function ChannelPresentationSection(props: {
  form: UseFormReturn<ChannelFormInput>
  channel?: MarketplaceChannel
}) {
  const { t } = useTranslation()
  return (
    <FormSection icon={Store} title={t('商品展示')}>
      <FormField
        label={t('渠道名称')}
        error={props.form.formState.errors.custom_name?.message}
      >
        <Input
          aria-label={t('渠道名称')}
          maxLength={20}
          placeholder={t('留空使用系统名称')}
          {...props.form.register('custom_name')}
        />
      </FormField>
      <FormField
        label={t('分组备注')}
        error={props.form.formState.errors.description?.message}
      >
        <Textarea
          aria-label={t('分组备注')}
          maxLength={300}
          rows={3}
          {...props.form.register('description')}
        />
      </FormField>
      <FormField
        label={t('分组标签')}
        error={props.form.formState.errors.tags?.message}
      >
        <Input
          aria-label={t('分组标签')}
          placeholder={t('用逗号分隔，最多 6 个，每个不超过 16 字')}
          {...props.form.register('tags', {
            setValueAs: (value: string | string[]) =>
              Array.isArray(value)
                ? value
                : value
                    .split(/[,，]/)
                    .map((item) => item.trim())
                    .filter(Boolean),
          })}
        />
      </FormField>
      {props.channel?.moderation_reason && (
        <p role='status' className='text-muted-foreground text-sm'>
          {t('公开内容检查')}：{props.channel.moderation_reason}
        </p>
      )}
    </FormSection>
  )
}
