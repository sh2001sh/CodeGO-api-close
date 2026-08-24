import { Check, LoaderCircle, Route } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'

/** Adds a marketplace group to the current user's Auto route pool. */
export function AddToRoutePoolButton(props: {
  groupName: string
  selected: boolean
  busy: boolean
  adding: boolean
  onAdd: () => void
}) {
  const { t } = useTranslation()
  let label = t('添加到路由池')
  let Icon = Route
  if (props.selected) {
    label = t('已在路由池')
    Icon = Check
  } else if (props.adding) {
    label = t('添加中')
    Icon = LoaderCircle
  }
  let accessibleLabel = t('将 {{name}} 添加到路由池', {
    name: props.groupName,
  })
  if (props.selected) {
    accessibleLabel = t('{{name}} 已在路由池', { name: props.groupName })
  } else if (props.adding) {
    accessibleLabel = t('正在将 {{name}} 添加到路由池', {
      name: props.groupName,
    })
  }

  return (
    <Button
      variant={props.selected ? 'secondary' : 'outline'}
      size='sm'
      className='size-8 px-0 sm:w-auto sm:px-3'
      disabled={props.selected || props.busy}
      onClick={props.onAdd}
      aria-label={accessibleLabel}
      aria-busy={props.adding}
      title={accessibleLabel}
    >
      <Icon className={props.adding ? 'animate-spin' : undefined} />
      <span className='hidden sm:inline'>{label}</span>
    </Button>
  )
}
