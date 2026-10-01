import { createContext, useContext, useState, type ReactNode } from 'react'

type Dictionary = Record<string, string>
const chinese: Dictionary = {
  active: '启用',
  enabled: '启用',
  disabled: '停用',
  auto_disabled: '自动停用',
  paid: '已支付',
  created: '待支付',
  pending: '进行中',
  expired: '已到期',
  canceled: '已取消',
  refunded: '已退款',
  forming: '成团中',
  open: '成团中',
  settled: '已结算',
  completed: '已完成',
  consumed: '已使用',
  paused: '已暂停',
  verified: '已验证',
  verifying: '验证中',
  draft: '待验证',
  rejected: '未通过',
  issued: '已开票',
  approved: '已批准',
  resolved: '已处理',
  ignored: '已忽略',
  available: '可用',
  used: '已使用',
  user: '用户',
  admin: '管理员',
  root: '站点管理员',
  Completed: '完成',
  CompletedNoUsage: '估算完成',
  UpstreamErrorBeforeOutput: '输出前失败',
  UpstreamErrorAfterOutput: '输出后失败',
  EmptyStream: '空响应',
  ClientCanceled: '客户端取消',
  Timeout: '超时',
}
const Language = createContext({
  locale: 'zh-CN',
  t: (key: string) => key,
  change: async (_locale: string) => {},
})

export function LanguageProvider(props: { children: ReactNode }) {
  const [locale, setLocale] = useState('zh-CN')
  const [dictionary, setDictionary] = useState<Dictionary>(chinese)
  const change = async (next: string) => {
    const messages = next === 'en' ? (await import('../locales/en')).default : chinese
    setDictionary(messages)
    setLocale(next)
    document.documentElement.lang = next
  }
  return (
    <Language.Provider value={{ locale, t: (key) => dictionary[key] ?? key, change }}>
      {props.children}
    </Language.Provider>
  )
}

export const useTranslation = () => useContext(Language)
