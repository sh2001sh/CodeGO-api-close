/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useEffect, useState } from 'react'
import { useForm, type SubmitErrorHandler } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getUserModels } from '@/lib/api'
import { getCurrencyDisplay, getCurrencyLabel } from '@/lib/currency'
import { useStatus } from '@/hooks/use-status'
import { Button } from '@/components/ui/button'
import { Form } from '@/components/ui/form'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { createApiKey, getApiKey, updateApiKey } from '../api'
import { ERROR_MESSAGES, SUCCESS_MESSAGES } from '../constants'
import {
  getApiKeyFormSchema,
  type ApiKeyFormValues,
  getApiKeyFormDefaultValues,
  transformFormDataToPayload,
  transformApiKeyToFormDefaults,
} from '../lib'
import { type ApiKey } from '../types'
import { ApiKeyFormContent } from './api-key-form-content'
import { useApiKeys } from './api-keys-provider'
import { useApiKeyGroupOptions } from './use-api-key-group-options'

type ApiKeyMutateDrawerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentRow?: ApiKey
  side?: 'left' | 'right'
}

export function ApiKeysMutateDrawer({
  open,
  onOpenChange,
  currentRow,
  side = 'right',
}: ApiKeyMutateDrawerProps) {
  const { t } = useTranslation()
  const isUpdate = !!currentRow
  const { triggerRefresh } = useApiKeys()
  const { status } = useStatus()
  const [isSubmitting, setIsSubmitting] = useState(false)
  const defaultUseAutoGroup = status?.default_use_auto_group === true

  // Fetch models
  const { data: modelsData } = useQuery({
    queryKey: ['user-models'],
    queryFn: getUserModels,
    staleTime: 5 * 60 * 1000, // Cache for 5 minutes
  })

  const models = modelsData?.data || []
  const {
    options: groups,
    isPending: groupsLoading,
    error: groupsError,
    refetch: reloadGroups,
  } = useApiKeyGroupOptions()
  const backendHasAuto = groups.some((g) => g.value === 'auto')
  const schema = getApiKeyFormSchema(t)

  const form = useForm<ApiKeyFormValues>({
    resolver: zodResolver(schema),
    defaultValues: getApiKeyFormDefaultValues(defaultUseAutoGroup),
  })

  const detailsQuery = useQuery({
    queryKey: ['api-key-edit-details', currentRow?.id],
    queryFn: async () => {
      const result = await getApiKey(currentRow!.id)
      if (!result.success || !result.data) {
        throw new Error(result.message || 'Failed to load API key')
      }
      return result.data
    },
    enabled: open && isUpdate,
    gcTime: 0,
    retry: false,
    refetchOnWindowFocus: false,
  })
  const detailsReady =
    !isUpdate ||
    (!!detailsQuery.data && !detailsQuery.isFetching && !detailsQuery.isError)

  useEffect(() => {
    if (!open) return
    if (isUpdate && detailsQuery.data && !detailsQuery.isFetching) {
      form.reset(transformApiKeyToFormDefaults(detailsQuery.data))
    } else if (!isUpdate && !form.formState.isDirty) {
      form.reset(
        getApiKeyFormDefaultValues(defaultUseAutoGroup && backendHasAuto)
      )
    }
  }, [
    open,
    isUpdate,
    detailsQuery.data,
    detailsQuery.isFetching,
    form,
    defaultUseAutoGroup,
    backendHasAuto,
  ])

  // Keep the saved group even when metadata is still loading or has become
  // unavailable. Changing groups must be an explicit user action.

  const onSubmit = async (data: ApiKeyFormValues) => {
    if (!detailsReady) return
    setIsSubmitting(true)
    try {
      const basePayload = transformFormDataToPayload(data)

      if (isUpdate && currentRow) {
        const result = await updateApiKey({
          ...basePayload,
          id: currentRow.id,
        })
        if (result.success) {
          toast.success(t(SUCCESS_MESSAGES.API_KEY_UPDATED))
          onOpenChange(false)
          triggerRefresh()
        } else {
          toast.error(result.message || t(ERROR_MESSAGES.UPDATE_FAILED))
        }
      } else {
        // Create mode - handle batch creation
        const count = data.tokenCount || 1
        let successCount = 0

        for (let i = 0; i < count; i++) {
          const result = await createApiKey({
            ...basePayload,
            name:
              i === 0 && data.name
                ? data.name
                : `${data.name || 'default'}-${Math.random().toString(36).slice(2, 8)}`,
          })
          if (result.success) {
            successCount++
          } else {
            toast.error(result.message || t(ERROR_MESSAGES.CREATE_FAILED))
            break
          }
        }

        if (successCount > 0) {
          toast.success(
            t('Successfully created {{count}} API Key(s)', {
              count: successCount,
            })
          )
          onOpenChange(false)
          triggerRefresh()
        }
      }
    } catch (_error) {
      toast.error(t(ERROR_MESSAGES.UNEXPECTED))
    } finally {
      setIsSubmitting(false)
    }
  }

  const onInvalid: SubmitErrorHandler<ApiKeyFormValues> = () => {
    toast.error(t('Please fix the highlighted fields before saving'))
  }

  const { meta: currencyMeta } = getCurrencyDisplay()
  const currencyLabel = getCurrencyLabel()
  const tokensOnly = currencyMeta.kind === 'tokens'
  const quotaLabel = t('Quota ({{currency}})', { currency: currencyLabel })
  const quotaPlaceholder = tokensOnly
    ? t('Enter quota in tokens')
    : t('Enter quota in {{currency}}', { currency: currencyLabel })

  return (
    <Sheet
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v)
        if (!v) {
          form.reset()
        }
      }}
    >
      <SheetContent
        side={side}
        className='api-key-sheet bg-background flex !h-dvh !w-screen max-w-none gap-0 overflow-hidden p-0 sm:!w-full sm:!max-w-[560px]'
      >
        <SheetHeader className='bg-background border-b px-4 py-3 text-start sm:px-5 sm:py-4'>
          <SheetTitle className='text-base sm:text-lg'>
            {isUpdate ? t('Update API Key') : t('Create API Key')}
          </SheetTitle>
        </SheetHeader>
        {groupsLoading && (
          <p role='status' className='text-muted-foreground px-4 py-2 text-sm'>
            {t('Loading...')}
          </p>
        )}
        {groupsError && (
          <div
            role='alert'
            className='text-destructive flex items-center justify-between px-4 py-2 text-sm'
          >
            <span>{t('Unable to load groups')}</span>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => void reloadGroups()}
            >
              {t('Retry')}
            </Button>
          </div>
        )}
        {isUpdate && !detailsReady && (
          <div
            className='px-4 py-6'
            role={detailsQuery.isError ? 'alert' : 'status'}
          >
            <p>
              {detailsQuery.isError
                ? t(detailsQuery.error.message)
                : t('Loading...')}
            </p>
            {detailsQuery.isError && (
              <Button
                type='button'
                variant='outline'
                onClick={() => void detailsQuery.refetch()}
              >
                {t('Retry')}
              </Button>
            )}
          </div>
        )}
        {detailsReady && (
          <Form {...form}>
            <ApiKeyFormContent
              form={form}
              groups={groups}
              models={models}
              isUpdate={isUpdate}
              quotaLabel={quotaLabel}
              quotaPlaceholder={quotaPlaceholder}
              currencyLabel={currencyLabel}
              tokensOnly={tokensOnly}
              onSubmit={onSubmit}
              onInvalid={onInvalid}
            />
          </Form>
        )}
        <SheetFooter className='bg-background grid grid-cols-2 gap-2 border-t px-3 py-3 sm:flex sm:flex-row sm:justify-end sm:px-5 sm:py-4'>
          <SheetClose
            render={<Button variant='outline' className='w-full sm:w-auto' />}
          >
            {t('Close')}
          </SheetClose>
          <Button
            form='api-key-form'
            type='submit'
            disabled={isSubmitting || !detailsReady}
            className='w-full sm:w-auto'
          >
            {isSubmitting ? t('Saving...') : t('Save changes')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
