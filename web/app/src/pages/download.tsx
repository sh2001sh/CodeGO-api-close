import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Download as DownloadIcon } from 'lucide-react'
import { APIError } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { useTranslation } from '../lib/i18n'
import { date } from '../lib/format'
import { Button, EmptyState, ErrorMessage, Loading, PageHeader, Panel } from '../components/ui'
import { detectPlatform, formatBytes, platformName } from '../features/public/download-helpers'
import type { DesktopPlatform } from '../features/public/download-helpers'
import type { Schema } from '../lib/types'

// Unlike every other control-api route, this endpoint replies with the
// manifest object directly instead of the shared {success,data} envelope
// (see v3/internal/desktop/release.go). The generated api.GET client's
// transport hard-requires that envelope and throws before this response body
// is ever reachable, so it is fetched directly here. Reported as a backend
// inconsistency; api.ts is out of scope for this workstream.
async function fetchLatestRelease(signal: AbortSignal): Promise<Schema['DesktopReleaseManifest']> {
  const response = await fetch('/api/desktop/release/latest', {
    signal,
    credentials: 'same-origin',
  })
  if (!response.ok) throw new APIError('桌面服务暂时不可用', response.status)
  const payload: unknown = await response.json()
  if (!payload || typeof payload !== 'object' || !('version' in payload))
    throw new APIError('服务器返回了无效响应', response.status)
  return payload as Schema['DesktopReleaseManifest']
}

const releaseOptions = () => resourceOptions('desktop-release', fetchLatestRelease)

export default function DownloadPage() {
  const { t } = useTranslation()
  const release = useQuery(releaseOptions())
  const detected = useMemo(detectPlatform, [])
  const [selected, setSelected] = useState<DesktopPlatform>(detected)
  const assets = release.data?.assets ?? []
  const grouped = useMemo(() => {
    const byPlatform = new Map<string, typeof assets>()
    for (const asset of assets) {
      if (!asset.platform) continue
      const list = byPlatform.get(asset.platform) ?? []
      list.push(asset)
      byPlatform.set(asset.platform, list)
    }
    return byPlatform
  }, [assets])
  const platforms = [...grouped.keys()] as DesktopPlatform[]
  const highlighted = grouped.get(selected) ?? []

  return (
    <div className="site-container site-page download-page">
      <PageHeader title="下载客户端" description="适用于 macOS、Windows 与 Linux 的桌面客户端。" />
      <ErrorMessage error={release.error} />
      {release.isError && (
        <Button variant="secondary" onClick={() => void release.refetch()}>
          {t('重试')}
        </Button>
      )}
      {release.isPending && <Loading rows={4} />}
      {release.data && platforms.length === 0 && (
        <EmptyState title="暂无可用版本" description="当前版本没有可下载的安装包。" />
      )}
      {release.data && platforms.length > 0 && (
        <>
          <div className="download-platform-tabs" role="tablist" aria-label={t('选择平台')}>
            {platforms.map((platform) => (
              <button
                key={platform}
                type="button"
                role="tab"
                aria-selected={selected === platform}
                className="filter-option"
                onClick={() => setSelected(platform)}
              >
                {t(platformName(platform))}
                {platform === detected && <small>{t('推荐')}</small>}
              </button>
            ))}
          </div>
          <Panel
            title={platformName(selected)}
            description={`${t('版本')} ${release.data.version}`}
          >
            <ul className="download-asset-list">
              {highlighted.map((asset) => (
                <li key={asset.name} className="download-asset-row">
                  <div>
                    <strong className="mono">{asset.name}</strong>
                    <p className="subtle">
                      {asset.arch} · {formatBytes(asset.size)}
                      {asset.digest ? ` · ${asset.digest}` : ''}
                    </p>
                  </div>
                  <a className="button button-primary" href={asset.browser_download_url}>
                    <DownloadIcon size={16} aria-hidden />
                    {t('下载')}
                  </a>
                </li>
              ))}
            </ul>
          </Panel>
          {release.data.notes && (
            <Panel title="更新说明">
              <p className="muted" style={{ whiteSpace: 'pre-wrap' }}>
                {release.data.notes}
              </p>
              <p className="subtle">{date(release.data.published_at)}</p>
            </Panel>
          )}
          <Panel title="连接客户端" description="登录账号后，在客户端里授权本次会话。">
            <ol>
              <li>{t('安装并打开桌面客户端。')}</li>
              <li>{t('使用控制台账号登录并完成授权。')}</li>
              <li>
                {t('在')} <Link to="/desktop/devices">{t('桌面设备')}</Link>{' '}
                {t('中查看或撤销已授权的设备。')}
              </li>
            </ol>
          </Panel>
        </>
      )}
    </div>
  )
}
