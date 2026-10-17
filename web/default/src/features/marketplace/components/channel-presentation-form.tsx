import { useRef } from 'react'
import { isAxiosError } from 'axios'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { useMarketplaceChannelUpdate } from '../hooks'
import {
  channelPresentationSchema,
  type ChannelPresentationInput,
} from '../lib/channel-form'
import type { MarketplaceChannel } from '../types'
import { ChannelPresentationSection } from './channel-presentation-section'

export function ChannelPresentationForm(props: {
  channel: MarketplaceChannel
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const update = useMarketplaceChannelUpdate(false)
  const submitting = useRef(false)
  const form = useForm<ChannelPresentationInput>({
    resolver: zodResolver(channelPresentationSchema),
    defaultValues: {
      custom_name: props.channel.custom_name ?? '',
      description: props.channel.description ?? '',
      tags: props.channel.tags ?? [],
    },
  })
  const submit = form.handleSubmit(async (values) => {
    if (submitting.current) return
    submitting.current = true
    form.clearErrors('root.server')
    try {
      await update.mutateAsync({
        id: props.channel.id,
        values: {
          custom_name: values.custom_name,
          description: values.description,
          tags: values.tags,
        },
      })
      toast.success(t('Saved successfully'))
      props.onSaved()
    } catch (error) {
      const responseMessage = isAxiosError<{ message?: unknown }>(error)
        ? error.response?.data?.message
        : undefined
      let message = error instanceof Error ? error.message : t('保存失败')
      if (typeof responseMessage === 'string' && responseMessage) {
        message = responseMessage
      }
      form.setError('root.server', { message })
    } finally {
      submitting.current = false
    }
  })
  return (
    <form onSubmit={submit}>
      <ChannelPresentationSection form={form} channel={props.channel} />
      {form.formState.errors.root?.server?.message && (
        <p role='alert' className='text-destructive px-4 pb-4 text-sm sm:px-5'>
          {form.formState.errors.root.server.message}
        </p>
      )}
      <div className='bg-muted/15 flex justify-end px-4 py-4 sm:px-5'>
        <Button type='submit' disabled={update.isPending}>
          {update.isPending && <Loader2 className='animate-spin' aria-hidden />}
          {t('保存')}
        </Button>
      </div>
    </form>
  )
}
