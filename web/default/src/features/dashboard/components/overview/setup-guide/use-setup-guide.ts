import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  BookOpen,
  CreditCard,
  FileText,
  KeyRound,
  Play,
  RadioTower,
  ShieldCheck,
  TerminalSquare,
  Timer,
} from 'lucide-react'
import { useAuthStore } from '@/stores/auth-store'
import { getUserModels } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { fetchTokenKey, getApiKeys } from '@/features/keys/api'
import { normalizeFullApiKey } from '@/features/keys/lib/normalize-full-api-key'
import type {
  HeroSignal,
  QuickAction,
  RequestExample,
  StartStep,
} from './types'
import {
  buildCurlCommand,
  formatDisplayKey,
  getPreferredKey,
  normalizeAnthropicEndpoint,
  getSavedSetupGuideExpanded,
  normalizeEndpoint,
  saveSetupGuideExpanded,
} from './utils'

const API_ENDPOINTS = [
  { label: 'OpenAI 兼容', url: 'https://shu26.cfd/v1' },
  { label: '通用入口', url: 'https://shu26.cfd' },
] as const

export interface SetupGuideState {
  startSteps: StartStep[]
  visibleQuickActions: QuickAction[]
  heroSignals: HeroSignal[]
  requestExample: RequestExample
  completedStepCount: number
  setupComplete: boolean
  setupGuideExpanded: boolean
  nextSetupStep: StartStep
  onToggle: () => void
}

export function useSetupGuide(): SetupGuideState {
  const user = useAuthStore((state) => state.auth.user)
  const [manualSetupGuideExpanded, setManualSetupGuideExpanded] = useState<
    boolean | null
  >(() => getSavedSetupGuideExpanded())

  const requestCount = Number(user?.request_count ?? 0)
  const remainQuota = Number(user?.quota ?? 0)
  const usedQuota = Number(user?.used_quota ?? 0)
  const isAdmin = Boolean(user?.role && user.role >= ROLE.ADMIN)

  const apiKeysQuery = useQuery({
    queryKey: ['dashboard', 'overview', 'api-keys'],
    queryFn: async () => {
      const result = await getApiKeys({ p: 1, size: 10 })
      return result.success ? (result.data?.items ?? []) : []
    },
    staleTime: 60 * 1000,
  })

  const modelsQuery = useQuery({
    queryKey: ['dashboard', 'overview', 'user-models'],
    queryFn: async () => {
      const result = await getUserModels()
      return result.success ? (result.data ?? []) : []
    },
    staleTime: 5 * 60 * 1000,
  })

  const preferredKey = useMemo(
    () => getPreferredKey(apiKeysQuery.data ?? []),
    [apiKeysQuery.data]
  )

  const realKeyQuery = useQuery({
    queryKey: ['dashboard', 'overview', 'token-key', preferredKey?.id],
    queryFn: async () => {
      if (!preferredKey?.id) return ''
      const result = await fetchTokenKey(preferredKey.id)
      return result.success && result.data?.key
        ? normalizeFullApiKey(result.data.key)
        : ''
    },
    enabled: Boolean(preferredKey?.id),
    staleTime: 5 * 60 * 1000,
  })

  const startSteps = useMemo<StartStep[]>(
    () => [
      {
        title: '创建 API Key',
        description: '先生成一把可用密钥，后续请求和脚本都靠它接入。',
        to: '/keys',
        icon: KeyRound,
        completed: Boolean(preferredKey),
      },
      {
        title: '补充额度',
        description: '先准备余额或套餐额度，避免请求测试到一半中断。',
        to: '/wallet',
        icon: CreditCard,
        completed: remainQuota > 0 || usedQuota > 0,
      },
      {
        title: '发起请求',
        description: '用 Playground 或客户端先跑通一条真实请求。',
        to: '/playground',
        icon: TerminalSquare,
        completed: requestCount > 0,
      },
    ],
    [preferredKey, remainQuota, requestCount, usedQuota]
  )

  const visibleQuickActions = useMemo<QuickAction[]>(() => {
    const actions: QuickAction[] = [
      {
        title: 'Playground',
        description: '先在浏览器里直接试模型和提示词。',
        to: '/playground',
        icon: Play,
      },
      {
        title: '使用日志',
        description: '看请求、扣费和报错记录，排查最直接。',
        to: '/usage-logs',
        icon: FileText,
      },
      {
        title: '价格总览',
        description: '先确认模型价格，再决定用余额还是套餐。',
        to: '/pricing',
        icon: BookOpen,
      },
      {
        title: '渠道管理',
        description: '配置上游和路由策略，仅管理员可见。',
        to: '/channels',
        icon: RadioTower,
        adminOnly: true,
      },
    ]
    return actions.filter((action) => !action.adminOnly || isAdmin)
  }, [isAdmin])

  const heroSignals = useMemo<HeroSignal[]>(
    () => [
      {
        label: '路由状态',
        value: '固定入口',
        icon: RadioTower,
      },
      {
        label: '鉴权状态',
        value: preferredKey ? '已就绪' : '缺少 API Key',
        icon: ShieldCheck,
      },
      {
        label: '默认模型',
        value: modelsQuery.data?.[0] ?? '加载中',
        icon: Timer,
      },
    ],
    [modelsQuery.data, preferredKey]
  )

  const requestExample = useMemo<RequestExample>(() => {
    const endpoint = normalizeEndpoint(API_ENDPOINTS[0].url)
    const anthropicEndpoint = normalizeAnthropicEndpoint(API_ENDPOINTS[1].url)
    const model = modelsQuery.data?.[0] ?? 'gpt-4o-mini'
    const apiKey = realKeyQuery.data ?? ''
    const keyName = preferredKey?.name ?? '还没有 API Key'
    const ready = Boolean(apiKey && model)

    return {
      endpoint,
      openaiEndpoint: endpoint,
      anthropicEndpoint,
      model,
      keyName,
      displayKey: formatDisplayKey(apiKey),
      ready,
      curl: buildCurlCommand({ endpoint, apiKey: apiKey || 'sk-...', model }),
    }
  }, [modelsQuery.data, preferredKey, realKeyQuery.data])

  const completedStepCount = startSteps.filter((step) => step.completed).length
  const setupComplete = completedStepCount === startSteps.length
  const setupGuideExpanded = manualSetupGuideExpanded ?? false
  const nextSetupStep =
    startSteps.find((step) => !step.completed) ??
    startSteps[startSteps.length - 1]

  const onToggle = () => {
    const nextExpanded = !setupGuideExpanded
    setManualSetupGuideExpanded(nextExpanded)
    saveSetupGuideExpanded(nextExpanded)
  }

  return {
    startSteps,
    visibleQuickActions,
    heroSignals,
    requestExample,
    completedStepCount,
    setupComplete,
    setupGuideExpanded,
    nextSetupStep,
    onToggle,
  }
}
