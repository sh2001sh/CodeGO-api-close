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
import { useState } from 'react'
import { ChevronDown, Route, Settings2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { FormField } from '@/components/ui/form'
import { StatusBadge } from '@/components/status-badge'
import type {
  GlobalJsonFieldName,
  GlobalModelSettingsForm,
} from './global-settings-form'
import { ProtocolBridgePolicyEditor } from './protocol-bridge-policy-editor'

type ProtocolBridgeSettingsProps = { form: GlobalModelSettingsForm }

function AutomaticProtocolStatus() {
  const { t } = useTranslation()
  return (
    <>
      <div className='flex flex-wrap items-center gap-2'>
        <h3 className='text-base font-semibold'>
          {t('OpenAI Protocol Compatibility')}
        </h3>
        <StatusBadge
          label={t('Automatic')}
          variant='success'
          copyable={false}
        />
      </div>
      <div className='flex gap-3 rounded-lg border p-4'>
        <div className='bg-muted text-muted-foreground flex size-9 shrink-0 items-center justify-center rounded-md'>
          <Route className='size-4' aria-hidden='true' />
        </div>
        <div className='space-y-1'>
          <p className='text-sm font-medium'>
            {t('Automatic protocol routing')}
          </p>
          <p className='text-muted-foreground max-w-[72ch] text-sm'>
            {t(
              'Native endpoints are preferred. The gateway converts only for known protocol-only targets or when an upstream endpoint reports that it is unsupported.'
            )}
          </p>
        </div>
      </div>
    </>
  )
}

function ProtocolOverrideFields({ form }: ProtocolBridgeSettingsProps) {
  const { t } = useTranslation()
  const policies = [
    {
      name: 'global.chat_completions_to_responses_policy' as const,
      title: t('Chat Completions -> Responses'),
      description: t('Override routing to Responses-only upstreams.'),
    },
    {
      name: 'global.responses_to_chat_completions_policy' as const,
      title: t('Responses -> Chat Completions'),
      description: t('Override routing to Chat Completions-only upstreams.'),
    },
  ]

  const formatJson = (field: GlobalJsonFieldName) => {
    const raw = form.getValues(field)
    if (!raw || !raw.trim()) return
    try {
      form.setValue(field, JSON.stringify(JSON.parse(raw), null, 2), {
        shouldDirty: true,
      })
    } catch {
      toast.error(t('Invalid JSON format'))
    }
  }

  return policies.map((policy) => (
    <FormField
      key={policy.name}
      control={form.control}
      name={policy.name}
      render={({ field }) => (
        <ProtocolBridgePolicyEditor
          title={policy.title}
          description={policy.description}
          value={field.value}
          onChange={field.onChange}
          onFormat={() => formatJson(policy.name)}
        />
      )}
    />
  ))
}

function AdvancedProtocolOverrides({ form }: ProtocolBridgeSettingsProps) {
  const { t } = useTranslation()
  const [advancedOpen, setAdvancedOpen] = useState(false)
  return (
    <Collapsible open={advancedOpen} onOpenChange={setAdvancedOpen}>
      <div className='rounded-lg border'>
        <CollapsibleTrigger
          render={
            <button
              type='button'
              className='hover:bg-muted/50 focus-visible:ring-ring flex w-full items-center gap-3 px-4 py-3 text-left transition-colors focus-visible:ring-2 focus-visible:outline-none'
            />
          }
        >
          <Settings2 className='text-muted-foreground size-4 shrink-0' />
          <div className='min-w-0 flex-1'>
            <p className='text-sm font-medium'>{t('Advanced overrides')}</p>
            <p className='text-muted-foreground mt-0.5 text-xs'>
              {t('Force or disable conversion for exceptional channels')}
            </p>
          </div>
          <ChevronDown
            className={cn(
              'text-muted-foreground size-4 shrink-0 transition-transform',
              advancedOpen && 'rotate-180'
            )}
          />
        </CollapsibleTrigger>
        <CollapsibleContent>
          <div className='space-y-5 border-t p-4'>
            <Alert>
              <AlertTitle>{t('Overrides take precedence')}</AlertTitle>
              <AlertDescription>
                {t(
                  'Leave both values as {} to keep automatic routing. Use force or disabled only for channels whose advertised capability is inaccurate.'
                )}
              </AlertDescription>
            </Alert>
            <ProtocolOverrideFields form={form} />
          </div>
        </CollapsibleContent>
      </div>
    </Collapsible>
  )
}

export function ProtocolBridgeSettings({ form }: ProtocolBridgeSettingsProps) {
  return (
    <div className='space-y-4'>
      <AutomaticProtocolStatus />
      <AdvancedProtocolOverrides form={form} />
    </div>
  )
}
