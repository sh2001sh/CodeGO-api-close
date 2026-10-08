import { useQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { useTranslation } from '../lib/i18n'
import { date } from '../lib/format'
import {
  Badge,
  Button,
  Callout,
  EmptyState,
  ErrorMessage,
  Loading,
  PageHeader,
  Panel,
} from '../components/ui'
import {
  groupTone,
  latestProbeAt,
  modelTone,
  overallCopy,
  overallTone,
  toneBadge,
  toneLabel,
} from '../features/public/status-helpers'

const statusOptions = () =>
  resourceOptions('public-status', (signal) => api.GET('/api/status', { signal }).then(unwrap))
const groupStatusOptions = () =>
  resourceOptions('public-group-status', (signal) =>
    api.GET('/api/marketplace/group-status', { signal }).then(unwrap),
  )

export default function StatusPage() {
  const { t } = useTranslation()
  const status = useQuery(statusOptions())
  const groups = useQuery(groupStatusOptions())
  const pending = status.isPending || groups.isPending
  const error = status.error ?? groups.error
  const data = groups.data ?? []
  const tone = overallTone(data)
  const updatedAt = data
    .map(latestProbeAt)
    .filter((value): value is string => Boolean(value))
    .sort((a, b) => Date.parse(a) - Date.parse(b))
    .at(-1)
  const refresh = () => {
    void status.refetch()
    void groups.refetch()
  }

  return (
    <div className="site-container site-page status-page">
      <PageHeader
        title="服务状态"
        description="展示各分组最近的连通性探测；探测延迟不代表真实请求的首字延迟。"
        action={
          <Button
            variant="secondary"
            onClick={refresh}
            loading={status.isFetching || groups.isFetching}
          >
            {t('刷新')}
          </Button>
        }
      />
      <ErrorMessage error={error} />
      {error && (
        <Button variant="secondary" onClick={refresh}>
          {t('重试')}
        </Button>
      )}
      {pending && !error && <Loading rows={4} />}
      {!pending && !error && (
        <>
          <Callout tone={tone === 'degraded' ? 'danger' : tone === 'healthy' ? 'success' : 'info'}>
            <span>{t(overallCopy[tone])}</span>
            {status.data && (
              <span className="subtle" style={{ marginLeft: 8 }}>
                v{status.data.version}
              </span>
            )}
          </Callout>
          <p className="subtle status-updated" role="status">
            {updatedAt ? String(t('最近更新')) + ' ' + String(date(updatedAt)) : t('暂无采样数据')}
          </p>
          {data.length === 0 ? (
            <EmptyState title="暂无公开分组" description="当前没有可展示状态的分组。" />
          ) : (
            <div className="status-group-list">
              {data.map((group) => (
                <StatusGroupRow key={group.group_id} group={group} />
              ))}
            </div>
          )}
        </>
      )}
    </div>
  )
}

function StatusGroupRow(props: { group: Parameters<typeof groupTone>[0] }) {
  const { t } = useTranslation()
  const tone = groupTone(props.group)
  const results = props.group.model_verification_results ?? []
  return (
    <Panel
      className="status-group"
      title={props.group.system_display_name || props.group.public_slug}
      action={<Badge tone={toneBadge[tone]}>{t(toneLabel[tone])}</Badge>}
    >
      {results.length === 0 ? (
        <p className="muted">{t('暂无探测记录')}</p>
      ) : (
        <ul className="status-model-list">
          {results.map((result) => {
            const resultTone = modelTone(result.status)
            return (
              <li key={result.model} className="status-model-row">
                <span className="mono">{result.model}</span>
                <span className="status-model-meta">
                  <Badge tone={toneBadge[resultTone]}>{t(toneLabel[resultTone])}</Badge>
                  <span className="subtle tabular">{result.latency_ms}ms</span>
                  <span className="subtle">{date(result.tested_at)}</span>
                </span>
                {result.error && <span className="status-model-error">{result.error}</span>}
              </li>
            )
          })}
        </ul>
      )}
    </Panel>
  )
}
