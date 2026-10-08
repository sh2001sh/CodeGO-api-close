import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearch } from '@tanstack/react-router'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { Button, ErrorMessage, Loading, PageHeader, Status } from '../components/ui'
import { date } from '../lib/format'

export default function DesktopAuthorizePage() {
  const search = useSearch({ strict: false })
  const sessionID = ('session_id' in search ? search.session_id : '') ?? ''
  const code = ('code' in search ? search.code : '') ?? ''
  const client = useQueryClient()
  const session = useQuery({
    ...resourceOptions(
      'desktop-authorize',
      (signal) =>
        api
          .GET('/api/desktop/auth/session', {
            signal,
            params: { query: { session_id: sessionID, code } },
          })
          .then(unwrap),
      [sessionID, code],
    ),
    enabled: !!sessionID && !!code,
    retry: false,
  })
  const decide = useMutation({
    mutationFn: (approve: boolean) =>
      approve
        ? api.POST('/api/desktop/auth/approve', { body: { session_id: sessionID } })
        : api.POST('/api/desktop/auth/reject', { body: { session_id: sessionID } }),
    onSuccess: () => client.invalidateQueries({ queryKey: ['desktop-authorize'] }),
  })
  const pending =
    session.data?.status === 'pending' && Number(session.data.expires_at) * 1000 > Date.now()
  return (
    <>
      <PageHeader title="桌面客户端授权" />
      {!sessionID || !code ? (
        <p role="alert">授权链接信息不完整，请从桌面客户端重新发起授权。</p>
      ) : session.isPending ? (
        <Loading />
      ) : null}
      <ErrorMessage error={session.error ?? decide.error} />
      {session.data && (
        <section className="section">
          <p>请核对桌面客户端显示的设备信息和授权码。只有你主动发起的请求才应批准。</p>
          <dl className="metrics">
            <div>
              <dt>设备</dt>
              <dd>{session.data.device_name}</dd>
            </div>
            <div>
              <dt>平台与版本</dt>
              <dd>
                {session.data.platform} · {session.data.app_version}
              </dd>
            </div>
            <div>
              <dt>授权码</dt>
              <dd>
                <code>{session.data.user_code}</code>
              </dd>
            </div>
            <div>
              <dt>有效期</dt>
              <dd>{date(Number(session.data.expires_at) * 1000)}</dd>
            </div>
            <div>
              <dt>状态</dt>
              <dd>
                <Status value={session.data.status} />
              </dd>
            </div>
          </dl>
          <h2>客户端将获得以下权限</h2>
          <ul>
            {session.data.permissions.map((permission) => (
              <li key={permission}>{permission}</li>
            ))}
          </ul>
          {pending ? (
            <div className="row-actions">
              <Button disabled={decide.isPending} onClick={() => decide.mutate(true)}>
                批准桌面访问
              </Button>
              <Button
                variant="danger"
                disabled={decide.isPending}
                onClick={() => decide.mutate(false)}
              >
                拒绝桌面访问
              </Button>
            </div>
          ) : (
            <p role="status">
              {session.data.status === 'approved'
                ? '此请求已批准。你可以在桌面设备页撤销访问。'
                : session.data.status === 'rejected'
                  ? '此请求已拒绝。'
                  : '此请求已经结束，请从桌面客户端重新发起授权。'}
            </p>
          )}
        </section>
      )}
    </>
  )
}
