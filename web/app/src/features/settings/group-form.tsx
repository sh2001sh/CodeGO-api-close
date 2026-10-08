// One form per settings group. Submits only the keys the admin actually
// changed, as individual PUT /api/settings/{key} calls. Secret fields are
// skipped entirely unless the admin explicitly chose to replace them.
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../../lib/api'
import { useTranslation } from '../../lib/i18n'
import { useToast } from '../../hooks/use-toast'
import { Button, ErrorMessage, Panel } from '../../components/ui'
import type { Schema } from '../../lib/types'
import type { SettingFieldDef, SettingGroupDef } from './registry-types'
import { SettingFieldInput } from './field-input'
import { initialBooleanValue, initialTextValue, serializeValue } from './values'

type Setting = Schema['CatalogSetting']

function groupSections(fields: readonly SettingFieldDef[]): (string | undefined)[] {
  const seen: (string | undefined)[] = []
  for (const field of fields) if (!seen.includes(field.section)) seen.push(field.section)
  return seen
}

export function SettingGroupForm(props: {
  group: SettingGroupDef
  fields: readonly SettingFieldDef[]
  settings: readonly Setting[]
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const toast = useToast((state) => state.add)
  const byKey = new Map(props.settings.map((item) => [item.key, item]))
  const [replacing, setReplacing] = useState<Record<string, boolean>>({})
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({})
  const [savedKeys, setSavedKeys] = useState<string[]>([])

  const save = useMutation({
    mutationFn: async (entries: { key: string; value: unknown; sensitive: boolean }[]) => {
      setSavedKeys([])
      for (const entry of entries) {
        await api.PUT('/api/settings/{key}', {
          params: { path: { key: entry.key } },
          body: { value: entry.value, sensitive: entry.sensitive },
        })
        setSavedKeys((current) => [...current, entry.key])
      }
    },
    onSuccess: () => {
      setReplacing({})
      setSavedKeys([])
      toast(t('已保存设置'), 'success')
      void queryClient.invalidateQueries({ queryKey: ['settings'] })
    },
    onError: () => {
      // Earlier PUTs have already committed even when a later one fails.
      void queryClient.invalidateQueries({ queryKey: ['settings'] })
    },
  })

  const onSubmit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setFieldErrors({})
    const form = new FormData(event.currentTarget)
    const entries: { key: string; value: unknown; sensitive: boolean }[] = []
    const errors: Record<string, string> = {}
    for (const field of props.fields) {
      const existing = byKey.get(field.key)
      if (field.type === 'secret') {
        if (!replacing[field.key]) continue
        const next = String(form.get(field.key) ?? '')
        if (!next) continue
        entries.push({ key: field.key, value: next, sensitive: true })
        continue
      }
      if (field.type === 'boolean') {
        const next = form.has(field.key)
        if (next !== initialBooleanValue(field, existing?.value))
          entries.push({ key: field.key, value: next, sensitive: false })
        continue
      }
      const text = String(form.get(field.key) ?? '')
      if (text === initialTextValue(field, existing?.value)) continue
      try {
        const value = serializeValue(field, text)
        entries.push({ key: field.key, value, sensitive: existing?.sensitive ?? false })
      } catch (cause) {
        errors[field.key] = cause instanceof Error ? cause.message : '参数无效'
      }
    }
    if (Object.keys(errors).length > 0) {
      setFieldErrors(errors)
      return
    }
    if (entries.length === 0) return
    save.mutate(entries)
  }

  return (
    <Panel title={props.group.label} description={props.group.description}>
      <form className="form-stack" onSubmit={onSubmit}>
        <ErrorMessage error={save.error} />
        {save.isError && savedKeys.length > 0 && (
          <p role="status" className="notice">
            {t('已保存 {count} 项：{keys}。其余配置未保存，请检查错误后重试。', {
              count: savedKeys.length,
              keys: savedKeys.join(', '),
            })}
          </p>
        )}
        {groupSections(props.fields).map((section) => (
          <fieldset key={section ?? '__default'} className="form-panel">
            {section && <legend>{t(section)}</legend>}
            {props.fields
              .filter((field) => field.section === section)
              .map((field) => (
                <SettingFieldInput
                  key={field.key}
                  field={field}
                  setting={byKey.get(field.key)}
                  error={fieldErrors[field.key]}
                  replacing={!!replacing[field.key]}
                  onToggleReplace={() =>
                    setReplacing((prev) => ({ ...prev, [field.key]: !prev[field.key] }))
                  }
                />
              ))}
          </fieldset>
        ))}
        <div className="form-actions">
          <Button type="submit" loading={save.isPending}>
            {t('保存')}
          </Button>
        </div>
      </form>
    </Panel>
  )
}
