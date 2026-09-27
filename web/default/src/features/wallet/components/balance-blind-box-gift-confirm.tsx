import i18n from '@/i18n/config'
import { useTranslation, Trans } from 'react-i18next'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import type { WalletTransferRecipient } from '../types'

export function BalanceBoxGiftConfirm(props: {
  confirmGift: boolean
  count: number
  busy: boolean
  recipient: WalletTransferRecipient | null
  onConfirmGiftChange: (open: boolean) => void
  onGift: () => void
}) {
  useTranslation()
  return (
    <AlertDialog
      open={props.confirmGift}
      onOpenChange={props.onConfirmGiftChange}
    >
      <AlertDialogContent className='max-h-[calc(100dvh-2rem)] overflow-y-auto'>
        <AlertDialogHeader>
          <AlertDialogTitle>
            <Trans i18nKey={'确认赠送'} /> {props.count}{' '}
            <Trans i18nKey={'个统一盲盒？'} />
          </AlertDialogTitle>
          <AlertDialogDescription>
            <Trans i18nKey={'接收方为'} />{' '}
            {props.recipient?.display_name_masked}（
            {props.recipient?.external_id}
            <Trans i18nKey={'）。赠送后所有权立即转移，无法撤回。'} />
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={props.busy}>
            <Trans i18nKey={'取消'} />
          </AlertDialogCancel>
          <AlertDialogAction
            disabled={props.busy}
            onClick={(event) => {
              event.preventDefault()
              props.onGift()
            }}
          >
            {props.busy ? i18n.t('赠送中…') : i18n.t('确认赠送')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
