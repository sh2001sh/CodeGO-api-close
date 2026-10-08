// Settings field definitions: blind box routing, mail and other (sensitive words).
// Split out of registry.ts to stay under the 400-line file limit.
// Every key is grounded in a confirmed live (non-test) read in the v3 Go backend.
//
// Daily lucky numbers are retired. Historical settings remain stored for
// compatibility, but no editor or endpoint can issue new numbers or draws.
import type { SettingFieldDef } from './registry-types'

export const extraFields: readonly SettingFieldDef[] = [
  // 盲盒与活动 — v3/internal/gateway/routing/card.go:12
  {
    key: 'blind_box_setting.multiplier_card_route_group',
    label: '倍率卡路由分组',
    description: '倍率卡请求路由到的分组名',
    type: 'string',
    group: 'blindbox',
    placeholder: '纯Pro号池',
  },

  // 邮件 — v3/cmd/internal/boot/mail.go:80
  { key: 'SMTPServer', label: 'SMTP 服务器地址', type: 'string', group: 'mail' },
  { key: 'SMTPPort', label: 'SMTP 端口', type: 'number', group: 'mail', placeholder: '587' },
  { key: 'SMTPAccount', label: 'SMTP 账号', type: 'string', group: 'mail' },
  { key: 'SMTPToken', label: 'SMTP 密码 / 令牌', type: 'secret', group: 'mail' },
  { key: 'SMTPFrom', label: '发信地址', type: 'string', group: 'mail' },
  { key: 'SMTPSSLEnabled', label: '启用隐式 TLS', type: 'boolean', group: 'mail' },
  {
    key: 'SMTPForceAuthLogin',
    label: '强制使用 AUTH LOGIN',
    description: 'Outlook / Azure 等需要强制 LOGIN 认证方式的服务商',
    type: 'boolean',
    group: 'mail',
  },
  {
    key: 'EmailLoginAuthServerList',
    label: '强制 LOGIN 认证的服务器列表',
    type: 'json',
    group: 'mail',
    hint: '可填 JSON 数组或逗号分隔字符串',
  },

  // 其他 / 敏感词与内容安全 — v3/internal/gateway/sensitive_config.go (未配置时默认开启)
  {
    key: 'CheckSensitiveEnabled',
    label: '启用敏感词检测',
    type: 'boolean',
    group: 'other',
    section: '敏感词',
    defaultTrue: true,
  },
  {
    key: 'CheckSensitiveOnPromptEnabled',
    label: '对输入内容检测',
    type: 'boolean',
    group: 'other',
    section: '敏感词',
    defaultTrue: true,
  },
  {
    key: 'StopOnSensitiveEnabled',
    label: '命中后终止请求',
    type: 'boolean',
    group: 'other',
    section: '敏感词',
    defaultTrue: true,
  },
  {
    key: 'SensitiveWords',
    label: '敏感词规则',
    description: '每行一条规则，支持 re: 正则前缀或 contains: 包含前缀',
    type: 'string',
    group: 'other',
    section: '敏感词',
  },
]
