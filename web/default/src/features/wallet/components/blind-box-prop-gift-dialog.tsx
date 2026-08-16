import { useState } from 'react'
import { Gift, Loader2, Search, ShieldAlert } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import {
  giftBlindBoxProp,
  isApiSuccess,
  lookupWalletTransferRecipient,
} from '../api'
import type { BlindBoxProp, WalletTransferRecipient } from '../types'

export function BlindBoxPropGiftDialog(props: {
  open: boolean
  prop: BlindBoxProp | null
  onOpenChange: (open: boolean) => void
  onGifted: () => Promise<void>
}) {
  const [externalId, setExternalId] = useState('')
  const [recipient, setRecipient] = useState<WalletTransferRecipient | null>(
    null
  )
  const [lookingUp, setLookingUp] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  const reset = () => {
    setExternalId('')
    setRecipient(null)
  }

  const handleOpenChange = (open: boolean) => {
    if (!open) reset()
    props.onOpenChange(open)
  }

  const lookup = async () => {
    const normalized = externalId.trim().toUpperCase()
    if (normalized.length !== 6) {
      toast.error('请输入有效的 6 位用户 ID')
      return
    }
    setLookingUp(true)
    setRecipient(null)
    try {
      const response = await lookupWalletTransferRecipient(normalized)
      if (!isApiSuccess(response) || !response.data) {
        throw new Error(response.message || '未找到接收用户')
      }
      setExternalId(response.data.external_id)
      setRecipient(response.data)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : '未找到接收用户')
    } finally {
      setLookingUp(false)
    }
  }

  const submit = async () => {
    if (!props.prop || !recipient) return
    setSubmitting(true)
    try {
      const response = await giftBlindBoxProp(
        props.prop.id,
        buildPropGiftRequestId(),
        recipient.external_id
      )
      if (!isApiSuccess(response) || !response.data) {
        throw new Error(response.message || '道具赠送失败')
      }
      toast.success(`已将 ${props.prop.title} 赠送给 ${recipient.external_id}`)
      reset()
      await props.onGifted()
    } catch (error) {
      toast.error(error instanceof Error ? error.message : '道具赠送失败')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={props.open} onOpenChange={handleOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <Gift className='text-primary size-4' />
            赠送道具
          </DialogTitle>
          <DialogDescription>
            {props.prop ? `将“${props.prop.title}”转移给其他用户。` : ''}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-4'>
          <div>
            <label
              htmlFor='prop-gift-recipient'
              className='text-sm font-medium'
            >
              接收用户 ID
            </label>
            <div className='mt-2 flex gap-2'>
              <Input
                id='prop-gift-recipient'
                value={externalId}
                maxLength={6}
                placeholder='6 位用户 ID'
                onChange={(event) => {
                  setExternalId(event.target.value.toUpperCase())
                  setRecipient(null)
                }}
                disabled={submitting}
              />
              <Button
                type='button'
                variant='outline'
                onClick={() => void lookup()}
                disabled={lookingUp || submitting}
              >
                {lookingUp ? (
                  <Loader2 className='size-4 animate-spin' />
                ) : (
                  <Search className='size-4' />
                )}
                查询
              </Button>
            </div>
          </div>

          {recipient ? (
            <div className='border-border bg-muted/25 rounded-lg border px-3 py-3'>
              <div className='text-sm font-medium'>{recipient.external_id}</div>
              <div className='text-muted-foreground mt-1 text-xs'>
                {recipient.display_name_masked || '已确认接收用户'}
              </div>
            </div>
          ) : null}

          <div className='bg-warning/10 text-warning-foreground flex gap-2 rounded-lg px-3 py-2.5 text-xs leading-5'>
            <ShieldAlert className='mt-0.5 size-4 shrink-0' />
            <span>
              赠送完成后不可撤回。已启用、暂停、锁定、已使用或已过期的道具不能赠送。
            </span>
          </div>
        </div>

        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            onClick={() => handleOpenChange(false)}
            disabled={submitting}
          >
            取消
          </Button>
          <Button
            type='button'
            onClick={() => void submit()}
            disabled={!recipient || submitting}
          >
            {submitting ? (
              <Loader2 className='size-4 animate-spin' />
            ) : (
              <Gift className='size-4' />
            )}
            确认赠送
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function buildPropGiftRequestId() {
  if (typeof crypto !== 'undefined' && crypto.randomUUID)
    return crypto.randomUUID()
  return `prop-gift-${Date.now()}`
}
