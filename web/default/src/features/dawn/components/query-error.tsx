import { AlertTriangle, RefreshCw } from 'lucide-react'
import { Trans } from 'react-i18next'

export function DawnQueryError(props: {
  title: string
  description?: string
  onRetry: () => void
  retrying?: boolean
}) {
  return (
    <div className='empty'>
      <span className='eic'>
        <AlertTriangle size={20} />
      </span>
      <b>{props.title}</b>
      {props.description ? <span>{props.description}</span> : null}
      <button
        className='btn mini'
        onClick={props.onRetry}
        disabled={props.retrying}
      >
        <RefreshCw size={14} className={props.retrying ? 'animate-spin' : ''} />
        <Trans i18nKey={'重新加载'} />
      </button>
    </div>
  )
}
