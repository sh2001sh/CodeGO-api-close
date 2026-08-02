import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { quotaUnitsToUsd } from '@/lib/format'
import {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerHeader,
  DrawerTitle,
} from '@/components/ui/drawer'
import {
  useCreateBounty,
  usePublishBountyDraft,
  useSaveBountyDraft,
  useUpdateBountyDraft,
} from '../hooks/use-bounty-actions'
import { useBountyBalances } from '../hooks/use-bounty-list'
import {
  bountyFormSchema,
  tagsFromInput,
  type BountyFormValues,
} from '../lib/bounty-form'
import { bountyUsdToQuota } from '../lib/bounty-format'
import type { BountyTask } from '../types'
import { BountyPublishForm } from './bounty-publish-form'
import { BountyPublishReview } from './bounty-publish-review'

interface BountyPublishDrawerProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  task?: BountyTask
}

function defaultDeadline() {
  const value = new Date(Date.now() + 7 * 24 * 60 * 60 * 1000)
  const offset = value.getTimezoneOffset()
  return new Date(value.getTime() - offset * 60 * 1000)
    .toISOString()
    .slice(0, 16)
}

function toLocalDateTime(value: string) {
  const date = new Date(value)
  const offset = date.getTimezoneOffset()
  return new Date(date.getTime() - offset * 60 * 1000)
    .toISOString()
    .slice(0, 16)
}

function defaultFormValues(): BountyFormValues {
  return {
    title: '',
    description: '',
    repo_url: '',
    task_type: 'general',
    tags: '',
    reward_wallet_type: 'wallet',
    reward_amount: 10,
    deadline_at: defaultDeadline(),
  }
}

function createIdempotencyKey(prefix: string) {
  const random =
    typeof crypto !== 'undefined' && 'randomUUID' in crypto
      ? crypto.randomUUID()
      : `${Date.now()}-${Math.random()}`
  return `${prefix}-${random}`
}

function taskFormValues(task: BountyTask): BountyFormValues {
  return {
    title: task.title,
    description: task.description,
    repo_url: task.repo_url,
    task_type: task.task_type as BountyFormValues['task_type'],
    tags: task.tags.join(', '),
    reward_wallet_type: task.reward_wallet_type,
    reward_amount: quotaUnitsToUsd(task.reward_amount),
    deadline_at: toLocalDateTime(task.deadline_at),
  }
}

export function BountyPublishDrawer(props: BountyPublishDrawerProps) {
  const { t } = useTranslation()
  const [reviewing, setReviewing] = useState(false)
  const [draftTaskId, setDraftTaskId] = useState(props.task?.task_id)
  const [createKey] = useState(() => createIdempotencyKey('bounty-create'))
  const [draftKey, setDraftKey] = useState(() =>
    createIdempotencyKey('bounty-draft')
  )
  const balanceQuery = useBountyBalances()
  const createMutation = useCreateBounty()
  const draftMutation = useSaveBountyDraft()
  const updateDraftMutation = useUpdateBountyDraft(draftTaskId ?? '')
  const publishDraftMutation = usePublishBountyDraft(draftTaskId ?? '')
  const form = useForm<BountyFormValues>({
    resolver: zodResolver(bountyFormSchema),
    defaultValues: props.task
      ? taskFormValues(props.task)
      : defaultFormValues(),
  })

  const values = form.getValues()
  const onReview = () => {
    void form.trigger().then((valid) => {
      if (valid) setReviewing(true)
    })
  }
  const onSubmit = form.handleSubmit(async (data) => {
    const payload = {
      title: data.title.trim(),
      description: data.description.trim(),
      repo_url: data.repo_url.trim(),
      task_type: data.task_type,
      tags: tagsFromInput(data.tags),
      reward_wallet_type: data.reward_wallet_type,
      reward_amount: bountyUsdToQuota(data.reward_amount),
      deadline_at: new Date(data.deadline_at).toISOString(),
      idempotency_key: createKey,
    }
    if (draftTaskId) {
      await updateDraftMutation.mutateAsync(payload)
      await publishDraftMutation.mutateAsync()
    } else {
      await createMutation.mutateAsync(payload)
    }
    props.onOpenChange(false)
  })
  const saveDraft = () => {
    const data = form.getValues()
    const payload = {
      title: data.title.trim(),
      description: data.description.trim(),
      repo_url: data.repo_url.trim(),
      task_type: data.task_type,
      tags: tagsFromInput(data.tags),
      reward_wallet_type: data.reward_wallet_type,
      reward_amount: bountyUsdToQuota(data.reward_amount),
      deadline_at: data.deadline_at
        ? new Date(data.deadline_at).toISOString()
        : undefined,
      idempotency_key: draftKey,
    }
    if (draftTaskId) {
      updateDraftMutation.mutate(payload)
    } else {
      draftMutation.mutate(payload, {
        onSuccess: (result) => {
          setDraftTaskId(result.task.task_id)
          setDraftKey(createIdempotencyKey('bounty-draft'))
        },
      })
    }
  }

  return (
    <Drawer
      open={props.open}
      onOpenChange={props.onOpenChange}
      direction='right'
    >
      <DrawerContent className='sm:max-w-xl'>
        <DrawerHeader className='border-border/70 border-b text-left'>
          <DrawerTitle>
            {props.task ? t('Edit bounty draft') : t('Publish a coding task')}
          </DrawerTitle>
          <DrawerDescription>
            {t(
              'Only the information needed to start a clear coding request is required.'
            )}
          </DrawerDescription>
        </DrawerHeader>
        {reviewing ? (
          <BountyPublishReview
            values={values}
            onSaveDraft={saveDraft}
            onBackToEdit={() => setReviewing(false)}
            onSubmit={() => void onSubmit()}
            error={
              createMutation.error ??
              updateDraftMutation.error ??
              publishDraftMutation.error
            }
            balances={balanceQuery.data ?? []}
            balancesError={balanceQuery.isError}
            balancesLoading={balanceQuery.isLoading}
            saving={draftMutation.isPending || updateDraftMutation.isPending}
            publishing={
              createMutation.isPending ||
              updateDraftMutation.isPending ||
              publishDraftMutation.isPending
            }
          />
        ) : (
          <BountyPublishForm
            form={form}
            onCancel={() => props.onOpenChange(false)}
            onReview={onReview}
          />
        )}
      </DrawerContent>
    </Drawer>
  )
}
