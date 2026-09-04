import { AlertTriangle, RefreshCw } from 'lucide-react'

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
        重新加载
      </button>
    </div>
  )
}
