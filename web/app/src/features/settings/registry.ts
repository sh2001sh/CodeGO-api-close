// Typed catalog of known /api/settings keys, grounded in v3 backend reads.
// Every key in registry-fields-core.ts and registry-fields-extra.ts was found
// via a live (non-test) read from v3_platform.settings in the Go backend; the
// source file is noted next to each entry there. Keys with no confirmed
// backend reader are intentionally left out and fall back to the raw/advanced
// editor in raw-settings.tsx.
export type {
  SettingType,
  SettingGroupId,
  SettingFieldDef,
  SettingGroupDef,
} from './registry-types'
import type { SettingGroupDef, SettingFieldDef } from './registry-types'
import { coreFields } from './registry-fields-core'
import { extraFields } from './registry-fields-extra'

export const settingGroups: readonly SettingGroupDef[] = [
  { id: 'site', label: '站点', description: '站点地址与维护公告' },
  { id: 'auth', label: '登录与认证', description: '邮箱策略、第三方登录与分组访问' },
  { id: 'payment', label: '支付', description: '支付活动与折扣策略' },
  { id: 'billing', label: '计费与额度', description: '计费模式、表达式与订阅策略' },
  { id: 'model', label: '模型与部署', description: '外部算力部署服务配置' },
  { id: 'blindbox', label: '盲盒与活动', description: '盲盒与活动相关的路由配置' },
  { id: 'mail', label: '邮件', description: 'SMTP 发信配置' },
  { id: 'other', label: '其他', description: '内容安全与敏感词策略' },
]

export const settingFields: readonly SettingFieldDef[] = [...coreFields, ...extraFields]

export function fieldByKey(key: string): SettingFieldDef | undefined {
  return settingFields.find((field) => field.key === key)
}
