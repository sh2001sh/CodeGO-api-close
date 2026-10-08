import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import { storedLocale, storeLocale } from './preferences'
import { languages, resolveLocale, type Locale } from './locales'
import { DirectionProvider } from '@base-ui/react/direction-provider'

type Dictionary = Record<string, string>
export type TranslationParameters = Record<string, string | number>
export type Translate = (key: string, parameters?: TranslationParameters) => string

export function interpolateTranslation(
  message: string,
  parameters?: TranslationParameters,
): string {
  return message.replace(/\{(\w+)\}/g, (placeholder, key: string) =>
    parameters?.[key] === undefined ? placeholder : String(parameters[key]),
  )
}
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
const loaders: Record<Exclude<Locale, 'zh-CN'>, () => Promise<{ default: Dictionary }>> = {
  'zh-HK': () => import('../locales/zh-HK.json'),
  en: () => import('../locales/en'),
  ja: () => import('../locales/ja.json'),
  ru: () => import('../locales/ru.json'),
  ko: () => import('../locales/ko.json'),
  fr: () => import('../locales/fr.json'),
  de: () => import('../locales/de.json'),
  ar: () => import('../locales/ar.json'),
}
const Language = createContext({
  locale: 'zh-CN' as Locale,
  t: ((key, parameters) => interpolateTranslation(key, parameters)) as Translate,
  change: async (_locale: string) => {},
  pending: false,
  error: false,
})

export function LanguageProvider(props: { children: ReactNode }) {
  const [locale, setLocale] = useState<Locale>('zh-CN')
  const [dictionary, setDictionary] = useState<Dictionary>(chinese)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState(false)
  const [initialized, setInitialized] = useState(false)
  const request = useRef(0)
  const fallback = useRef<Dictionary>({})
  const change = async (value: string) => {
    const next = resolveLocale(value)
    if (!next) return
    const current = ++request.current
    setPending(true)
    setError(false)
    try {
      const messages = next === 'zh-CN' ? chinese : (await loaders[next]()).default
      // Unknown localized text uses English; source Chinese is only the final fallback.
      const english = next === 'zh-CN' ? {} : (await loaders.en()).default
      if (current !== request.current) return
      fallback.current = english
      setDictionary(messages)
      setLocale(next)
      storeLocale(next)
      document.documentElement.lang = next
      document.documentElement.dir = languages.find((language) => language.code === next)!.dir
    } catch {
      if (current === request.current) setError(true)
    } finally {
      if (current === request.current) {
        setPending(false)
        setInitialized(true)
      }
    }
  }
  useEffect(() => {
    void change(storedLocale())
    return () => {
      request.current++
    }
  }, [])
  return (
    <Language.Provider
      value={{
        locale,
        t: (key, parameters) =>
          interpolateTranslation(dictionary[key] ?? fallback.current[key] ?? key, parameters),
        change,
        pending,
        error,
      }}
    >
      <DirectionProvider direction={locale === 'ar' ? 'rtl' : 'ltr'}>
        {initialized ? (
          props.children
        ) : (
          <div className="loading" role="status" aria-label="CodeGo AI">
            <span />
            <span />
            <span />
          </div>
        )}
      </DirectionProvider>
    </Language.Provider>
  )
}

export const useTranslation = () => useContext(Language)
