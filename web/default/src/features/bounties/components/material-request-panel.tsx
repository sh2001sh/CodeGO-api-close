import { useEffect, useState } from 'react'
import { Check, CircleAlert, Clock3, MessageSquare, Send } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import {
  useBountyAction,
  useMaterialReply,
  useResolveMaterialRequest,
  useMaterialTimeout,
} from '../hooks/use-bounty-actions'
import { formatBountyDate, materialStatusLabel } from '../lib/bounty-format'
import type { BountyMaterialRequest, BountyTask } from '../types'

interface MaterialRequestPanelProps {
  task: BountyTask
  requests: BountyMaterialRequest[]
}

export function MaterialRequestPanel(props: MaterialRequestPanelProps) {
  const { t } = useTranslation()
  const [requestText, setRequestText] = useState('')
  const [blocking, setBlocking] = useState(false)
  const requestMutation = useBountyAction(
    props.task.task_id,
    'material-requests'
  )
  const canAsk = props.task.can_start || props.task.can_submit
  const submitRequest = () => {
    if (!requestText.trim()) return
    requestMutation.mutate({
      content: requestText.trim(),
      is_blocking: blocking,
    })
    setRequestText('')
    setBlocking(false)
  }
  return (
    <section className='border-border/70 bg-card/50 space-y-5 rounded-xl border p-4 sm:p-5'>
      <div className='flex items-start justify-between gap-4'>
        <div>
          <h2 className='text-base font-semibold'>{t('Task discussion')}</h2>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t(
              'Keep questions and answers here so the final decision has an auditable record.'
            )}
          </p>
        </div>
        <MessageSquare
          className='text-muted-foreground mt-0.5 size-5 shrink-0'
          aria-hidden='true'
        />
      </div>
      {canAsk ? (
        <div className='border-border/70 bg-background/60 space-y-3 rounded-lg border p-3'>
          <Label htmlFor='material-request'>
            {t('What do you need from the publisher?')}
          </Label>
          <Textarea
            id='material-request'
            value={requestText}
            onChange={(event) => setRequestText(event.target.value)}
            placeholder={t(
              'For example: please provide the mobile reference image and confirm the target width.'
            )}
            className='min-h-24 resize-y'
          />
          <label className='flex items-center gap-2 text-sm'>
            <Checkbox
              checked={blocking}
              onCheckedChange={(checked) => setBlocking(checked === true)}
            />
            <span>{t('This blocks development')}</span>
          </label>
          <div className='flex justify-end'>
            <Button
              size='sm'
              onClick={submitRequest}
              disabled={!requestText.trim() || requestMutation.isPending}
            >
              <Send aria-hidden='true' />
              {t('Send request')}
            </Button>
          </div>
        </div>
      ) : null}
      {props.requests.length ? (
        <div className='space-y-4'>
          {props.requests.map((request) => (
            <MaterialRequestItem
              key={request.request_id}
              task={props.task}
              request={request}
            />
          ))}
        </div>
      ) : (
        <div className='text-muted-foreground flex items-center gap-2 text-sm'>
          <CircleAlert className='size-4' aria-hidden='true' />
          {t('No material requests yet.')}
        </div>
      )}
    </section>
  )
}

function MaterialRequestItem(props: {
  task: BountyTask
  request: BountyMaterialRequest
}) {
  const { t } = useTranslation()
  const [replyText, setReplyText] = useState('')
  const replyMutation = useMaterialReply(
    props.task.task_id,
    props.request.request_id
  )
  const resolveMutation = useResolveMaterialRequest(
    props.task.task_id,
    props.request.request_id
  )
  const timeoutMutation = useMaterialTimeout(
    props.task.task_id,
    props.request.request_id
  )
  const [now, setNow] = useState(() => Date.now())
  const [sourceURL, setSourceURL] = useState('')
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 60_000)
    return () => window.clearInterval(timer)
  }, [])
  const canReply = props.task.can_manage && props.request.status !== 'closed'
  const canResolve =
    (props.task.can_start || props.task.can_submit) &&
    props.request.status !== 'closed'
  const timeoutReady = Boolean(
    props.request.timeout_at &&
    new Date(props.request.timeout_at).getTime() <= now &&
    props.request.status === 'open'
  )
  const submitReply = () => {
    if (!replyText.trim()) return
    const trimmedSourceURL = sourceURL.trim()
    replyMutation.mutate({
      content: replyText.trim(),
      source_type: trimmedSourceURL ? 'github' : 'platform',
      source_url: trimmedSourceURL || undefined,
    })
    setReplyText('')
    setSourceURL('')
  }
  return (
    <article className='border-border/60 space-y-3 rounded-lg border p-3'>
      <div className='flex items-start justify-between gap-3'>
        <div className='flex items-center gap-2 text-sm font-medium'>
          <span>{props.request.requester.display_name}</span>
          <span
            className={`bounty-status bounty-status-${materialStatusTone(props.request.status)}`}
          >
            {materialStatusLabel(props.request.status, t)}
          </span>
          {props.request.is_blocking ? (
            <span className='bounty-status bounty-status-warning'>
              {t('Blocking')}
            </span>
          ) : null}
        </div>
        <span className='text-muted-foreground text-xs'>
          {formatBountyDate(props.request.created_at)}
        </span>
      </div>
      <p className='text-sm leading-6 whitespace-pre-wrap'>
        {props.request.content}
      </p>
      {props.request.replies.length ? (
        <div className='border-border/60 space-y-3 border-t pt-3'>
          {props.request.replies.map((reply) => (
            <div key={reply.reply_id} className='bg-muted/35 rounded-lg p-3'>
              <div className='flex items-center justify-between gap-3 text-xs'>
                <span className='font-medium'>{reply.author.display_name}</span>
                <span className='text-muted-foreground'>
                  {formatBountyDate(reply.created_at)}
                </span>
              </div>
              <p className='mt-2 text-sm leading-6 whitespace-pre-wrap'>
                {reply.content}
              </p>
              {reply.source_url ? (
                <a
                  className='text-primary mt-2 inline-block text-xs underline underline-offset-4'
                  href={reply.source_url}
                  target='_blank'
                  rel='noreferrer'
                >
                  {t('Open GitHub reference')}
                </a>
              ) : null}
            </div>
          ))}
        </div>
      ) : null}
      {canReply ? (
        <div className='space-y-2'>
          <Label
            htmlFor={`material-reply-${props.request.request_id}`}
            className='text-xs'
          >
            {t('Reply to material request')}
          </Label>
          <Textarea
            id={`material-reply-${props.request.request_id}`}
            value={replyText}
            onChange={(event) => setReplyText(event.target.value)}
            placeholder={t('Reply with the missing material or decision.')}
            className='min-h-20 resize-y'
          />
          <div className='flex flex-col gap-2 sm:flex-row sm:items-end'>
            <div className='min-w-0 flex-1 space-y-1'>
              <Label
                htmlFor={`material-source-${props.request.request_id}`}
                className='text-xs'
              >
                {t('GitHub reference URL (optional)')}
              </Label>
              <Input
                id={`material-source-${props.request.request_id}`}
                value={sourceURL}
                onChange={(event) => setSourceURL(event.target.value)}
                placeholder='https://github.com/.../issues/42'
              />
            </div>
            <Button
              size='sm'
              onClick={submitReply}
              disabled={!replyText.trim() || replyMutation.isPending}
            >
              <Send aria-hidden='true' />
              {t('Reply')}
            </Button>
          </div>
        </div>
      ) : null}
      {canResolve ? (
        <div className='flex justify-end'>
          <Button
            variant='outline'
            size='sm'
            onClick={() => resolveMutation.mutate()}
            disabled={resolveMutation.isPending}
          >
            <Check aria-hidden='true' />
            {t('Mark as resolved')}
          </Button>
        </div>
      ) : null}
      {props.task.can_handle_material_timeout && timeoutReady ? (
        <div className='border-warning/30 bg-warning/8 flex flex-wrap items-center justify-between gap-3 rounded-lg border p-3 text-sm'>
          <div className='flex items-center gap-2'>
            <Clock3 className='text-warning size-4' aria-hidden='true' />
            <span>{t('No reply after 48 hours')}</span>
          </div>
          <div className='flex gap-2'>
            <Button
              size='sm'
              variant='outline'
              onClick={() => timeoutMutation.mutate({ action: 'extend' })}
              disabled={timeoutMutation.isPending}
            >
              {t('Extend 48 hours')}
            </Button>
            <Button
              size='sm'
              variant='destructive'
              onClick={() => timeoutMutation.mutate({ action: 'cancel' })}
              disabled={timeoutMutation.isPending}
            >
              {t('Cancel without fault')}
            </Button>
          </div>
        </div>
      ) : null}
    </article>
  )
}

function materialStatusTone(status: string) {
  if (status === 'closed') return 'success'
  if (status === 'awaiting_confirmation') return 'info'
  if (status === 'replied') return 'default'
  return 'warning'
}
