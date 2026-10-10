import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import {
  ArrowRight,
  ArrowUpRight,
  Check,
  KeyRound,
  MessageSquare,
  Plus,
  Store,
  Wallet,
} from 'lucide-react'
import { api, unwrap } from '../lib/api'
import { walletOptions, keysOptions, sessionOptions, resourceOptions } from '../lib/queries'
import { credits, date } from '../lib/format'
import { useTranslation } from '../lib/i18n'
import {
  Badge,
  Button,
  Callout,
  CopyButton,
  CopyField,
  EmptyState,
  ErrorMessage,
  Loading,
  PageHeader,
  Panel,
  Status,
} from '../components/ui'
import { DataTable } from '../components/data-table'
import { FundingEconomics } from '../features/funding-economics'
import { LogDetailDrawer } from '../features/analytics/log-detail'
import { displayInt } from '../features/analytics/format'
import { rangeFromPreset } from '../features/analytics/time-range'
import type { AuditUsage } from '../features/analytics/types'
import { KeyPicker } from '../features/playground/key-picker'
import { useModelOptions } from '../features/playground/use-model-options'
import '../styles/dashboard.css'

function sampleRequest(base: string, model: string, group: string) {
  const shellQuote = (value: string) => `'${value.replace(/'/g, `'"'"'`)}'`
  return `curl ${shellQuote(`${base}/chat/completions`)} \\
  -H "Authorization: Bearer $CODEGO_API_KEY" \\
  -H 'Content-Type: application/json' \\
${group ? `  -H ${shellQuote(`X-CodeGo-Group: ${group}`)} \\\n` : ''}  --data-binary @- <<'CODEGO_JSON'
${JSON.stringify({ model: model || 'YOUR_MODEL_ID', messages: [{ role: 'user', content: 'Hello' }] }, null, 2)}
CODEGO_JSON`
}

export default function DashboardPage() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const wallet = useSuspenseQuery(walletOptions()).data
  const user = useSuspenseQuery(sessionOptions()).data
  const keys = useSuspenseQuery(keysOptions()).data
  const [period, setPeriod] = useState<'7d' | '30d'>('7d')
  const [range, setRange] = useState(() => rangeFromPreset('7d'))
  const [detail, setDetail] = useState<AuditUsage | null>(null)
  const [exampleOpen, setExampleOpen] = useState(false)
  const [selectedKeyID, setSelectedKeyID] = useState('')
  const [exampleSecret, setExampleSecret] = useState<string | null>(null)
  const [exampleGroup, setExampleGroup] = useState('')
  const [exampleModel, setExampleModel] = useState('')
  const selectedKey = keys?.find((key) => String(key.id) === selectedKeyID) ?? null
  const availableModels = useModelOptions(selectedKey, exampleSecret, exampleGroup)
  const exampleGroups = useQuery({
    ...resourceOptions('conversation-groups', (signal) =>
      api.GET('/api/user/self/groups', { signal }).then(unwrap),
    ),
    enabled: exampleOpen,
  })
  const selectedModel = availableModels.models.includes(exampleModel)
    ? exampleModel
    : (availableModels.models[0] ?? '')
  const stat = useQuery(
    resourceOptions(
      'dashboard-usage-stat',
      (signal) => api.GET('/api/log/self/stat', { signal, params: { query: range } }).then(unwrap),
      [range.from, range.to],
    ),
  )
  const recent = useQuery(
    resourceOptions(
      'dashboard-usage',
      (signal) =>
        api
          .GET('/api/log/self', { signal, params: { query: { ...range, page_size: 50 } } })
          .then(unwrap),
      [range.from, range.to],
    ),
  )
  const subscriptions = useQuery(
    resourceOptions('subscriptions', (signal) =>
      api.GET('/api/subscription/self', { signal }).then(unwrap),
    ),
  )
  const endpoint = `${window.location.origin}/v1`
  const activeKeys = keys?.filter((key) => key.status === 'active').length ?? 0
  const activeSubscriptions =
    subscriptions.data?.filter(
      (item) => item.state === 'active' && new Date(item.expires_at).getTime() > Date.now(),
    ) ?? []
  const planBalance = activeSubscriptions.reduce((total, item) => total + BigInt(item.balance), 0n)
  const hasFunding = BigInt(wallet.balance_micro_credits) >= 1_000_000n || planBalance >= 1_000_000n
  const rows = recent.data?.items ?? []
  const modelCounts = new Map<string, number>()
  for (const row of rows) modelCounts.set(row.model, (modelCounts.get(row.model) ?? 0) + 1)
  const models = [...modelCounts.entries()].sort((a, b) => b[1] - a[1]).slice(0, 4)
  const tokenCount =
    stat.data?.prompt_tokens !== undefined && stat.data?.completion_tokens !== undefined
      ? BigInt(stat.data.prompt_tokens) + BigInt(stat.data.completion_tokens)
      : undefined
  const steps = [
    { done: hasFunding, label: '准备可用额度', to: '/wallet', action: '充值或选购套餐' },
    { done: activeKeys > 0, label: '创建 API Key', to: '/keys', action: '创建' },
  ]
  const sample = sampleRequest(endpoint, selectedModel, exampleGroup)

  return (
    <div className="overview-page">
      <PageHeader
        title="仪表板"
        description={`${t('欢迎回来')}，${user.display_name || user.username}`}
        action={
          <>
            <a href="#api-integration" className="button button-ghost">
              {t('API 接入')}
            </a>
            <Link to="/playground" className="button button-secondary">
              <MessageSquare size={16} aria-hidden />
              {t('开始对话')}
            </Link>
            <Link to="/keys" className="button button-primary">
              <Plus size={16} aria-hidden />
              {t('创建 API Key')}
            </Link>
          </>
        }
      />

      {!subscriptions.isPending && !subscriptions.error && !hasFunding && (
        <Callout tone="warning" title={t('可用额度不足')}>
          {t('钱包与有效套餐的剩余额度均低于 1 credit。')} <Link to="/wallet">{t('前往钱包')}</Link>
        </Callout>
      )}

      <div className="overview-top-grid">
        <section className="overview-funds" aria-label={t('账户额度')}>
          <div className="overview-section-heading">
            <h2>{t('账户额度')}</h2>
            <Badge>{t(user.group)}</Badge>
          </div>
          <div className="overview-wallet-label">
            <Wallet size={16} aria-hidden />
            {t('钱包余额')}
          </div>
          <p className="overview-wallet-value">{credits(wallet.balance_micro_credits)}</p>
          <div className="overview-funds-actions">
            <Link to="/wallet" className="button button-primary">
              {t('充值')}
              <ArrowUpRight size={15} aria-hidden />
            </Link>
            <Link to="/billing" className="button button-ghost">
              {t('账单明细')}
              <ArrowRight size={15} aria-hidden />
            </Link>
          </div>
          <div className="overview-plan-balance">
            <div>
              <span>{t('有效套餐剩余额度')}</span>
              <strong>
                {subscriptions.isPending || subscriptions.error ? '—' : credits(planBalance)}
              </strong>
            </div>
            <Link to="/wallet" className="subtle">
              {t('管理套餐')}
              <ArrowUpRight size={14} aria-hidden />
            </Link>
          </div>
          <ErrorMessage error={subscriptions.error} />
          {subscriptions.error && (
            <Button variant="quiet" onClick={() => void subscriptions.refetch()}>
              {t('重试')}
            </Button>
          )}
        </section>

        <section className="overview-usage" aria-label={t('用量概览')}>
          <div className="overview-section-heading">
            <h2>{t('用量概览')}</h2>
            <div className="overview-period" role="group" aria-label={t('统计时段')}>
              {(['7d', '30d'] as const).map((value) => (
                <button
                  key={value}
                  type="button"
                  aria-pressed={period === value}
                  onClick={() => {
                    setPeriod(value)
                    setRange(rangeFromPreset(value))
                  }}
                >
                  {t(value === '7d' ? '最近 7 天' : '最近 30 天')}
                </button>
              ))}
            </div>
          </div>
          <ErrorMessage error={stat.error} />
          {stat.isPending ? (
            <Loading rows={2} />
          ) : (
            <dl className="overview-usage-metrics">
              <div>
                <dt>{t('请求数')}</dt>
                <dd>{displayInt(stat.data?.requests)}</dd>
              </div>
              <div>
                <dt>{t('扣费合计')}</dt>
                <dd>{credits(stat.data?.amount)}</dd>
              </div>
              <div>
                <dt>{t('输入与输出 token')}</dt>
                <dd>{displayInt(tokenCount)}</dd>
              </div>
              <div>
                <dt>{t('缓存 token')}</dt>
                <dd>{displayInt(stat.data?.cached_tokens)}</dd>
              </div>
            </dl>
          )}
          {BigInt(stat.data?.prompt_tokens_unknown_requests ?? 0) > 0 && (
            <p className="subtle">
              {t('输入 token 汇总不含历史统计异常的请求；扣费合计包含全部请求。')}
            </p>
          )}
          {stat.error && (
            <Button variant="quiet" onClick={() => void stat.refetch()}>
              {t('重试')}
            </Button>
          )}
          <Link to="/usage-logs" className="overview-text-link">
            {t('查看完整使用日志')}
            <ArrowRight size={15} aria-hidden />
          </Link>
        </section>
      </div>

      <div className="overview-workspace-grid">
        <div className="overview-primary">
          <Panel
            title="最近调用"
            description="所选时段内最近的 5 条调用，可打开查看详情。"
            action={
              <Link to="/usage-logs" className="button button-ghost" data-size="sm">
                {t('查看全部')}
                <ArrowUpRight size={15} aria-hidden />
              </Link>
            }
          >
            <ErrorMessage error={recent.error} />
            {recent.isPending ? (
              <Loading />
            ) : recent.error ? (
              <Button variant="quiet" onClick={() => void recent.refetch()}>
                {t('重试')}
              </Button>
            ) : (
              <DataTable
                rows={rows.slice(0, 5)}
                rowKey={(row) => `${row.request_id}-${row.created_at}`}
                onRowClick={setDetail}
                empty="暂无使用记录"
                columns={[
                  {
                    label: '模型',
                    render: (row) => (
                      <span className="overview-model-name" title={row.model}>
                        {row.model}
                      </span>
                    ),
                  },
                  { label: '时间', render: (row) => date(row.created_at), hideOnMobile: true },
                  { label: '扣费', render: (row) => credits(row.amount), numeric: true },
                  { label: '终态', render: (row) => <Status value={row.terminal} /> },
                ]}
              />
            )}
          </Panel>
          <Panel
            title="模型调用分布"
            description="仅根据所选时段最新 50 条调用统计，不代表全量用量。"
          >
            {recent.isPending ? (
              <Loading rows={2} />
            ) : recent.error ? (
              <p className="muted">{t('调用记录加载失败，重试后即可查看分布。')}</p>
            ) : models.length === 0 ? (
              <EmptyState
                title="还没有调用记录"
                description="选择分组和模型，开始你的第一次对话。"
                action={
                  <Link to="/playground" className="button button-secondary">
                    {t('开始对话')}
                  </Link>
                }
              />
            ) : (
              <ul className="overview-model-mix">
                {models.map(([model, count]) => (
                  <li key={model}>
                    <div>
                      <span>{model}</span>
                      <strong>
                        {count} <span>{t('次调用')}</span>
                      </strong>
                    </div>
                    <progress
                      value={count}
                      max={rows.length}
                      aria-label={`${model} ${count}/${rows.length}`}
                    />
                  </li>
                ))}
              </ul>
            )}
          </Panel>
        </div>

        <aside
          id="api-integration"
          className="overview-secondary"
          aria-label={t('接入与账户')}
          style={{ scrollMarginTop: 96 }}
        >
          <Panel title="API 接入" action={<KeyRound size={18} className="muted" aria-hidden />}>
            <div className="overview-key-count">
              <strong>{activeKeys}</strong>
              <span>{t('活跃 API Key')}</span>
              <Link to="/keys">{t('管理')}</Link>
            </div>
            <span className="overview-endpoint-label">{t('API 地址')}</span>
            <CopyField value={endpoint} label="复制地址" />
            <details
              className="overview-code-disclosure"
              onToggle={(event) => setExampleOpen(event.currentTarget.open)}
            >
              <summary>{t('查看调用示例')}</summary>
              {exampleOpen && (
                <>
                  <KeyPicker
                    keys={keys ?? []}
                    selectedID={selectedKeyID}
                    onSelect={(id) => {
                      setSelectedKeyID(id)
                      setExampleSecret(null)
                      setExampleModel('')
                      setExampleGroup('')
                    }}
                    hasSecret={Boolean(exampleSecret)}
                    onReveal={setExampleSecret}
                  />
                  <div className="field">
                    <label htmlFor="dashboard-example-group">{t('分组')}</label>
                    <select
                      id="dashboard-example-group"
                      value={exampleGroup}
                      onChange={(event) => {
                        setExampleGroup(event.target.value)
                        setExampleModel('')
                      }}
                    >
                      <option value="">{t('Key 默认分组')}</option>
                      {exampleGroups.data?.map((group) => (
                        <option key={group} value={group}>
                          {group}
                        </option>
                      ))}
                    </select>
                  </div>
                  <ErrorMessage error={exampleGroups.error} />
                  {exampleGroups.error && (
                    <Button variant="quiet" onClick={() => void exampleGroups.refetch()}>
                      {t('重试')}
                    </Button>
                  )}
                  <div className="field">
                    <label htmlFor="dashboard-example-model">{t('模型')}</label>
                    <select
                      id="dashboard-example-model"
                      value={selectedModel}
                      disabled={
                        !exampleSecret ||
                        availableModels.isPending ||
                        !availableModels.models.length
                      }
                      onChange={(event) => setExampleModel(event.target.value)}
                    >
                      {!availableModels.models.length && (
                        <option value="">
                          {t(availableModels.isPending ? '正在加载' : '选择模型')}
                        </option>
                      )}
                      {availableModels.models.map((model) => (
                        <option key={model} value={model}>
                          {model}
                        </option>
                      ))}
                    </select>
                  </div>
                  <ErrorMessage error={availableModels.error} />
                  {availableModels.error && (
                    <Button
                      variant="quiet"
                      onClick={() =>
                        void client.invalidateQueries({
                          queryKey: [
                            'playground-models',
                            selectedKey?.id,
                            Boolean(exampleSecret),
                            exampleGroup,
                          ],
                        })
                      }
                    >
                      {t('重试')}
                    </Button>
                  )}
                  {!exampleSecret && (
                    <p className="muted">
                      {t('选择 API Key 并加载可用模型后，示例会使用你的当前权限。')}
                    </p>
                  )}
                  {exampleSecret &&
                    !availableModels.isPending &&
                    !availableModels.error &&
                    !availableModels.models.length && (
                      <p role="status" className="muted">
                        {t('该 Key 在所选分组暂无可用模型')}
                      </p>
                    )}
                  <p className="muted">{t('这里只提供可复制示例，不会发送付费请求。')}</p>
                </>
              )}
              <div className="code-block">
                <CopyButton value={sample} label="复制示例" />
                <pre>
                  <code>{sample}</code>
                </pre>
              </div>
            </details>
            <Link to="/docs" className="overview-text-link">
              {t('接入文档')}
              <ArrowUpRight size={15} aria-hidden />
            </Link>
          </Panel>
          <Panel title="账户准备">
            <ol className="overview-readiness">
              {steps.map((step) => (
                <li key={step.label}>
                  <span className="overview-readiness-icon" data-done={step.done || undefined}>
                    {step.done ? <Check size={14} aria-hidden /> : <span aria-hidden>○</span>}
                  </span>
                  <div>
                    <strong>{t(step.label)}</strong>
                    {!step.done && <Link to={step.to}>{t(step.action)}</Link>}
                  </div>
                </li>
              ))}
            </ol>
            <Link to="/channel-market" className="overview-market-link">
              <Store size={18} aria-hidden />
              <span>
                <strong>{t('探索渠道市场')}</strong>
                <span>{t('比较分组质量与倍率')}</span>
              </span>
              <ArrowUpRight size={15} aria-hidden />
            </Link>
          </Panel>
        </aside>
      </div>
      {user.role === 'root' && <FundingEconomics />}
      <LogDetailDrawer usage={detail} onClose={() => setDetail(null)} />
    </div>
  )
}
