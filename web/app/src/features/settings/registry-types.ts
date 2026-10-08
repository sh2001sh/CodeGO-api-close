// Shared types for the settings registry. See registry.ts for the merged
// field list and registry-fields-*.ts for the grounded field definitions.

export type SettingType = 'boolean' | 'string' | 'secret' | 'number' | 'json' | 'select'

export type SettingGroupId =
  'site' | 'auth' | 'payment' | 'billing' | 'model' | 'blindbox' | 'mail' | 'other'

export interface SettingFieldDef {
  key: string
  label: string
  description?: string
  hint?: string
  type: SettingType
  group: SettingGroupId
  /** Visual sub-heading inside the group form; purely presentational. */
  section?: string
  options?: readonly { value: string; label: string }[]
  placeholder?: string
  /** For boolean fields the backend treats as enabled when the key is absent. */
  defaultTrue?: boolean
}

export interface SettingGroupDef {
  id: SettingGroupId
  label: string
  description: string
}
