import { useRouterState } from '@tanstack/react-router'
import { useEffect } from 'react'
import { useTranslation } from '../lib/i18n'

const pages: Record<string, string> = {
  '/': '模型与 API',
  '/models': '模型与价格',
  '/channel-market': '渠道市场',
  '/docs': '文档',
  '/status': '服务状态',
  '/help': '常见问题',
  '/support': '联系支持',
  '/about': '关于 CodeGo',
  '/terms': '服务条款',
  '/privacy': '隐私政策',
  '/refund-policy': '退款规则',
  '/supplier-agreement': '渠道供给与结算协议',
  '/supplier-terms': '渠道供给与结算协议',
  '/market-rules': '市场治理、评价与排名规则',
  '/dashboard': '仪表盘',
  '/keys': 'API Key',
  '/playground': '对话',
  '/my-channels': '渠道主工作台',
  '/billing': '账单明细',
  '/wallet': '钱包',
  '/orders': '订单',
  '/packages': '套餐',
  '/blind-box': '盲盒',
  '/notifications': '消息通知',
  '/profile': '个人设置',
  '/sign-in': '登录',
  '/sign-up': '注册',
}
const publicPages = new Set([
  '/',
  '/models',
  '/channel-market',
  '/docs',
  '/status',
  '/help',
  '/support',
  '/about',
  '/terms',
  '/privacy',
  '/refund-policy',
  '/supplier-agreement',
  '/supplier-terms',
  '/market-rules',
])

/** Keep the initial HTML metadata and update the same elements on navigation. */
export function PageMetadata() {
  const { t, locale } = useTranslation()
  const location = useRouterState({ select: (state) => state.location })
  const name = pages[location.pathname]
  const title = name ? `${t(name)} · CodeGo AI` : 'CodeGo AI'
  const description = t('浏览模型，比较分组，用熟悉的 SDK 接入。')
  const canonical = new URL(location.pathname, window.location.origin)
  const search = location.search as Record<string, unknown>
  // Keep stable public detail links; never include credentials or arbitrary search parameters.
  const fields =
    location.pathname === '/docs'
      ? ['article']
      : location.pathname === '/channel-market'
        ? ['group', 'shop']
        : []
  for (const field of fields) {
    const value = search[field]
    if (typeof value === 'string' && /^[A-Za-z0-9_-]{1,120}$/.test(value))
      canonical.searchParams.set(field, value)
  }
  const url = canonical.href
  const indexable = publicPages.has(location.pathname)
  useEffect(() => {
    document.title = title
    const meta = (attribute: 'name' | 'property', key: string, content: string) => {
      let element = document.head.querySelector<HTMLMetaElement>(`meta[${attribute}="${key}"]`)
      if (!element) {
        element = document.createElement('meta')
        element.setAttribute(attribute, key)
        document.head.append(element)
      }
      element.content = content
    }
    meta('name', 'description', description)
    meta('name', 'robots', indexable ? 'index,follow' : 'noindex,nofollow')
    meta('property', 'og:title', title)
    meta('property', 'og:description', description)
    meta('property', 'og:url', url)
    meta('property', 'og:site_name', 'CodeGo AI')
    meta('property', 'og:type', 'website')
    meta('property', 'og:locale', locale.replace('-', '_'))
    let link = document.head.querySelector<HTMLLinkElement>('link[rel="canonical"]')
    if (!link) {
      link = document.createElement('link')
      link.rel = 'canonical'
      document.head.append(link)
    }
    link.href = url
  }, [title, description, indexable, url, locale])
  return null
}
