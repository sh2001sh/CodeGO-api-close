// Value coercion between the registry's typed fields and the raw JSON values
// stored behind /api/settings. Secrets are never read back from the server,
// so there is no "initial secret value" — only whether one is configured.
import type { SettingFieldDef } from './registry-types'

/** Editable string representation of a non-boolean field's current value. */
export function initialTextValue(field: SettingFieldDef, raw: unknown): string {
  if (field.type === 'secret') return ''
  if (raw === undefined) return ''
  if (field.type === 'json') return JSON.stringify(raw, null, 2)
  if (typeof raw === 'string') return raw
  if (typeof raw === 'number' || typeof raw === 'boolean') return String(raw)
  return JSON.stringify(raw)
}

/** Editable boolean for a boolean field; falls back to the field's documented default. */
export function initialBooleanValue(field: SettingFieldDef, raw: unknown): boolean {
  if (raw === undefined) return field.defaultTrue ?? false
  if (typeof raw === 'boolean') return raw
  if (typeof raw === 'string') return raw.toLowerCase() === 'true'
  return field.defaultTrue ?? false
}

/** Converts an edited string/boolean back into the JSON value the API expects. */
export function serializeValue(field: SettingFieldDef, value: string | boolean): unknown {
  if (field.type === 'boolean') return Boolean(value)
  const text = String(value)
  if (field.type === 'number') {
    if (!/^-?\d+(\.\d+)?$/.test(text.trim())) throw new Error('请输入有效数字')
    return Number(text)
  }
  if (field.type === 'json') {
    try {
      return JSON.parse(text)
    } catch {
      throw new Error('配置值必须为有效 JSON')
    }
  }
  // string / secret
  return text
}

export function isSecretField(field: SettingFieldDef): boolean {
  return field.type === 'secret'
}
