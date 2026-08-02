import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  assignBounty,
  createBounty,
  handleMaterialTimeout,
  postAdminBountyAction,
  postBountyAction,
  postMaterialReply,
  reportBounty,
  resolveAdminBountyDispute,
  resolveMaterialRequest,
  publishBountyDraft,
  saveBountyDraft,
  updateBountyDraft,
} from '../api'
import type {
  AdminResolutionPayload,
  BountyDraftPayload,
  CreateBountyPayload,
} from '../types'

function invalidate(
  queryClient: ReturnType<typeof useQueryClient>,
  taskId?: string
) {
  void queryClient.invalidateQueries({ queryKey: ['bounties'] })
  void queryClient.invalidateQueries({ queryKey: ['bounty-balances'] })
  if (taskId) {
    void queryClient.invalidateQueries({ queryKey: ['bounty', taskId] })
  }
}

export function useCreateBounty() {
  const queryClient = useQueryClient()
  const { t } = useTranslation()
  return useMutation({
    mutationFn: (payload: CreateBountyPayload) => createBounty(payload),
    onSuccess: (result) => {
      invalidate(queryClient, result?.task.task_id)
      toast.success(t('Task published and reward quota frozen'))
    },
  })
}

export function useSaveBountyDraft() {
  const queryClient = useQueryClient()
  const { t } = useTranslation()
  return useMutation({
    mutationFn: (payload: BountyDraftPayload) => saveBountyDraft(payload),
    onSuccess: (result) => {
      invalidate(queryClient, result?.task.task_id)
      toast.success(t('Draft saved'))
    },
  })
}

export function useUpdateBountyDraft(taskId: string) {
  const queryClient = useQueryClient()
  const { t } = useTranslation()
  return useMutation({
    mutationFn: (payload: BountyDraftPayload) =>
      updateBountyDraft(taskId, payload),
    onSuccess: () => {
      invalidate(queryClient, taskId)
      toast.success(t('Draft updated'))
    },
  })
}

export function usePublishBountyDraft(taskId: string) {
  const queryClient = useQueryClient()
  const { t } = useTranslation()
  return useMutation({
    mutationFn: () => publishBountyDraft(taskId),
    onSuccess: (result) => {
      invalidate(queryClient, result?.task.task_id ?? taskId)
      toast.success(t('Task published and reward quota frozen'))
    },
  })
}

export function useBountyAction(taskId: string, action: string) {
  const queryClient = useQueryClient()
  const { t } = useTranslation()
  return useMutation({
    mutationFn: (payload?: Record<string, unknown>) =>
      postBountyAction(taskId, action, payload),
    onSuccess: () => {
      invalidate(queryClient, taskId)
      toast.success(t('Operation completed'))
    },
  })
}

export function useAssignBounty(taskId: string) {
  const queryClient = useQueryClient()
  const { t } = useTranslation()
  return useMutation({
    mutationFn: (applicationId: string) => assignBounty(taskId, applicationId),
    onSuccess: () => {
      invalidate(queryClient, taskId)
      toast.success(t('Executor confirmed'))
    },
  })
}

export function useMaterialReply(taskId: string, requestId: string) {
  const queryClient = useQueryClient()
  const { t } = useTranslation()
  return useMutation({
    mutationFn: (payload: {
      content: string
      source_type?: string
      source_url?: string
    }) => postMaterialReply(taskId, requestId, payload),
    onSuccess: () => {
      invalidate(queryClient, taskId)
      toast.success(t('Reply sent'))
    },
  })
}

export function useResolveMaterialRequest(taskId: string, requestId: string) {
  const queryClient = useQueryClient()
  const { t } = useTranslation()
  return useMutation({
    mutationFn: () => resolveMaterialRequest(taskId, requestId),
    onSuccess: () => {
      invalidate(queryClient, taskId)
      toast.success(t('Material request resolved'))
    },
  })
}

export function useMaterialTimeout(taskId: string, requestId: string) {
  const queryClient = useQueryClient()
  const { t } = useTranslation()
  return useMutation({
    mutationFn: (payload: {
      action: 'extend' | 'cancel'
      extension_hours?: number
    }) => handleMaterialTimeout(taskId, requestId, payload),
    onSuccess: () => {
      invalidate(queryClient, taskId)
      toast.success(t('Material timeout action saved'))
    },
  })
}

export function useReportBounty(taskId: string) {
  const queryClient = useQueryClient()
  const { t } = useTranslation()
  return useMutation({
    mutationFn: (payload: { reason: string; details?: string }) =>
      reportBounty(taskId, payload),
    onSuccess: () => {
      invalidate(queryClient, taskId)
      toast.success(t('Report submitted'))
    },
  })
}

export function useAdminBountyAction(taskId: string, action: string) {
  const queryClient = useQueryClient()
  const { t } = useTranslation()
  return useMutation({
    mutationFn: (payload?: Record<string, unknown>) =>
      postAdminBountyAction(taskId, action, payload),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['admin-bounties'] })
      void queryClient.invalidateQueries({
        queryKey: ['admin-bounty-disputes'],
      })
      void queryClient.invalidateQueries({ queryKey: ['admin-bounty-reports'] })
      void queryClient.invalidateQueries({ queryKey: ['bounty', taskId] })
      toast.success(t('Operation completed'))
    },
  })
}

export function useResolveAdminBountyDispute(taskId: string) {
  const queryClient = useQueryClient()
  const { t } = useTranslation()
  return useMutation({
    mutationFn: (payload: AdminResolutionPayload) =>
      resolveAdminBountyDispute(taskId, payload),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['admin-bounties'] })
      void queryClient.invalidateQueries({
        queryKey: ['admin-bounty-disputes'],
      })
      void queryClient.invalidateQueries({ queryKey: ['bounty', taskId] })
      toast.success(t('Dispute resolution saved'))
    },
  })
}
