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
import * as z from 'zod'
import type { UseFormReturn } from 'react-hook-form'

const jsonString = z.string().refine((value) => {
  const trimmed = value.trim()
  if (!trimmed) return true
  try {
    JSON.parse(trimmed)
    return true
  } catch {
    return false
  }
}, 'Invalid JSON format')

const protocolPolicyJson = jsonString.refine((value) => {
  const trimmed = value.trim()
  if (!trimmed) return true
  try {
    const policy = JSON.parse(trimmed) as { mode?: unknown }
    return (
      policy !== null &&
      !Array.isArray(policy) &&
      (policy.mode === undefined ||
        policy.mode === 'auto' ||
        policy.mode === 'force' ||
        policy.mode === 'disabled')
    )
  } catch {
    return false
  }
}, 'Policy mode must be auto, force, or disabled')

export const globalSettingsSchema = z.object({
  global: z.object({
    pass_through_request_enabled: z.boolean(),
    thinking_model_blacklist: jsonString,
    chat_completions_to_responses_policy: protocolPolicyJson,
    responses_to_chat_completions_policy: protocolPolicyJson,
  }),
  general_setting: z.object({
    ping_interval_enabled: z.boolean(),
    ping_interval_seconds: z.coerce.number().min(1),
  }),
})

export type GlobalModelSettingsFormValues = z.output<
  typeof globalSettingsSchema
>
export type GlobalModelSettingsFormInput = z.input<typeof globalSettingsSchema>
export type GlobalModelSettingsForm = UseFormReturn<
  GlobalModelSettingsFormInput,
  unknown,
  GlobalModelSettingsFormValues
>
export type GlobalJsonFieldName =
  | 'global.thinking_model_blacklist'
  | 'global.chat_completions_to_responses_policy'
  | 'global.responses_to_chat_completions_policy'

export type FlatGlobalModelSettings = {
  'global.pass_through_request_enabled': boolean
  'global.thinking_model_blacklist': string
  'global.chat_completions_to_responses_policy': string
  'global.responses_to_chat_completions_policy': string
  'general_setting.ping_interval_enabled': boolean
  'general_setting.ping_interval_seconds': number
}

function normalizeJsonText(value: string, fallback: string) {
  const trimmed = (value ?? '').toString().trim()
  return trimmed ? trimmed : fallback
}

export const flattenGlobalValues = (
  values: GlobalModelSettingsFormValues
): FlatGlobalModelSettings => ({
  'global.pass_through_request_enabled':
    values.global.pass_through_request_enabled,
  'global.thinking_model_blacklist': normalizeJsonText(
    values.global.thinking_model_blacklist,
    '[]'
  ),
  'global.chat_completions_to_responses_policy': normalizeJsonText(
    values.global.chat_completions_to_responses_policy,
    '{}'
  ),
  'global.responses_to_chat_completions_policy': normalizeJsonText(
    values.global.responses_to_chat_completions_policy,
    '{}'
  ),
  'general_setting.ping_interval_enabled':
    values.general_setting.ping_interval_enabled,
  'general_setting.ping_interval_seconds':
    values.general_setting.ping_interval_seconds,
})
