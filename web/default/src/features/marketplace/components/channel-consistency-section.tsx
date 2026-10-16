import type { UseFormReturn } from 'react-hook-form'
import { ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { NativeSelect } from '@/components/ui/native-select'
import type { ChannelFormInput } from '../lib/channel-form'
import { FormField, FormSection } from './channel-form-layout'

export function ChannelConsistencySection(props: {
  form: UseFormReturn<ChannelFormInput>
}) {
  const { t } = useTranslation()
  return (
    <FormSection icon={ShieldCheck} title={t('模型一致性标注')}>
      <FormField label={t('一致性结果')}>
        <NativeSelect
          className='w-full sm:max-w-xs'
          {...props.form.register('model_consistency_status')}
        >
          <option value='none'>{t('暂无')}</option>
          <option value='passed'>{t('通过')}</option>
          <option value='failed'>{t('不通过')}</option>
          <option value='questionable'>{t('存疑')}</option>
        </NativeSelect>
      </FormField>
    </FormSection>
  )
}
