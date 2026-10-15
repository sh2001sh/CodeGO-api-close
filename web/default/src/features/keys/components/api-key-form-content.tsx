import type {
  SubmitErrorHandler,
  SubmitHandler,
  UseFormReturn,
} from 'react-hook-form'
import type { ApiKeyFormValues } from '../lib'
import { ApiKeyAccessSections } from './api-key-access-sections'
import { ApiKeyBasicSection } from './api-key-basic-section'
import type { ApiKeyGroupOption } from './api-key-group-combobox'

export function ApiKeyFormContent(props: {
  form: UseFormReturn<ApiKeyFormValues>
  groups: ApiKeyGroupOption[]
  models: string[]
  isUpdate: boolean
  quotaLabel: string
  quotaPlaceholder: string
  currencyLabel: string
  tokensOnly: boolean
  onSubmit: SubmitHandler<ApiKeyFormValues>
  onInvalid: SubmitErrorHandler<ApiKeyFormValues>
}) {
  return (
    <form
      id='api-key-form'
      onSubmit={props.form.handleSubmit(props.onSubmit, props.onInvalid)}
      className='min-h-0 flex-1 space-y-3 overflow-y-auto overscroll-contain px-3 py-3 sm:space-y-4 sm:px-4 sm:py-4'
    >
      <ApiKeyBasicSection
        form={props.form}
        groups={props.groups}
        isUpdate={props.isUpdate}
      />
      <ApiKeyAccessSections
        form={props.form}
        models={props.models}
        quotaLabel={props.quotaLabel}
        quotaPlaceholder={props.quotaPlaceholder}
        currencyLabel={props.currencyLabel}
        tokensOnly={props.tokensOnly}
      />
    </form>
  )
}
