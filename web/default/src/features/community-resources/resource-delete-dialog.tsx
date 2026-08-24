import { Loader2, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
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
import type { CommunityResource } from './types'

export function ResourceDeleteDialog(props: {
  resource: CommunityResource | null
  deleting: boolean
  onCancel: () => void
  onConfirm: () => Promise<boolean>
}) {
  const { t } = useTranslation()

  return (
    <AlertDialog
      open={props.resource != null}
      onOpenChange={(open) => {
        if (!open && !props.deleting) props.onCancel()
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('Delete community resource?')}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(
              '“{{title}}” will be permanently deleted. This action cannot be undone.',
              { title: props.resource?.title ?? '' }
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={props.deleting}>
            {t('Cancel')}
          </AlertDialogCancel>
          <AlertDialogAction
            variant='destructive'
            disabled={props.deleting || props.resource == null}
            onClick={async (event) => {
              event.preventDefault()
              if (await props.onConfirm()) props.onCancel()
            }}
          >
            {props.deleting ? (
              <Loader2 className='animate-spin' />
            ) : (
              <Trash2 />
            )}
            {t('Delete permanently')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
