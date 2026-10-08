import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions, keysOptions } from '../lib/queries'
import type { Schema } from '../lib/types'
import { date } from '../lib/format'
import { DataTable } from '../components/data-table'
import { Button, ErrorMessage, Field, Loading, PageHeader, Status } from '../components/ui'

export default function DesktopDevicesPage() {
  const client = useQueryClient()
  const devices = useQuery(
    resourceOptions('desktop-devices', (signal) =>
      api.GET('/api/desktop/devices', { signal }).then(unwrap),
    ),
  )
  const keys = useQuery(keysOptions())
  const [revokeID, setRevokeID] = useState('')
  const revoke = useMutation({
    mutationFn: () =>
      api.DELETE('/api/desktop/devices/{id}', { params: { path: { id: revokeID } } }),
    onSuccess: () => {
      setRevokeID('')
      void client.invalidateQueries({ queryKey: ['desktop-devices'] })
    },
  })
  const createImport = useMutation({
    mutationFn: (body: Schema['DesktopImportInput']) =>
      api
        .POST('/api/desktop/import/deeplink', {
          body,
        })
        .then(unwrap),
  })
  const [linkError, setLinkError] = useState<Error | null>(null)
  return (
    <>
      <PageHeader title="桌面设备" />
      <ErrorMessage
        error={devices.error ?? revoke.error ?? keys.error ?? createImport.error ?? linkError}
      />
      {devices.isPending && <Loading />}
      {revokeID && (
        <div className="form-panel">
          <p>确认撤销此设备？该设备将不能继续使用当前授权。</p>
          <Button variant="danger" disabled={revoke.isPending} onClick={() => revoke.mutate()}>
            确认撤销设备
          </Button>
          <Button variant="quiet" disabled={revoke.isPending} onClick={() => setRevokeID('')}>
            取消
          </Button>
        </div>
      )}
      {devices.data && (
        <DataTable
          rows={devices.data}
          rowKey={(item) => item.id}
          columns={[
            { label: '设备', render: (item) => item.device_name },
            { label: '平台与版本', render: (item) => `${item.platform} · ${item.app_version}` },
            { label: '状态', render: (item) => <Status value={item.status} /> },
            { label: '最近使用', render: (item) => date(Number(item.last_used_at) * 1000) },
            { label: '到期时间', render: (item) => date(Number(item.expires_at) * 1000) },
            {
              label: '操作',
              render: (item) => (
                <Button
                  variant="danger"
                  disabled={item.status !== 'active' || revoke.isPending}
                  onClick={() => {
                    revoke.reset()
                    setRevokeID(String(item.id))
                  }}
                >
                  撤销
                </Button>
              ),
            },
          ]}
          empty="暂无已授权设备。请从桌面客户端发起授权。"
        />
      )}
      <section className="section">
        <h2>一键导入工具配置</h2>
        <p>为当前账号的 API Key 生成短期导入链接，打开桌面客户端后确认应用配置。</p>
        <form
          className="form-panel"
          onSubmit={(event) => {
            event.preventDefault()
            setLinkError(null)
            const form = new FormData(event.currentTarget)
            createImport.mutate({
              target: form.get('target') === 'ccswitch' ? 'ccswitch' : 'codego',
              tool: String(form.get('tool')),
              token_id: String(form.get('token_id')),
              name: String(form.get('name')),
              model: String(form.get('model')),
              haiku_model: String(form.get('haiku-model')),
              sonnet_model: String(form.get('sonnet-model')),
              opus_model: String(form.get('opus-model')),
              enabled: true,
            })
          }}
        >
          <label className="field" htmlFor="import-key">
            <span>API Key</span>
            <select id="import-key" name="token_id" required>
              <option value="">选择密钥</option>
              {keys.data
                ?.filter((key) => key.status === 'active')
                .map((key) => (
                  <option key={String(key.id)} value={String(key.id)}>
                    {key.name} · {key.key_prefix}
                  </option>
                ))}
            </select>
          </label>
          <label className="field" htmlFor="import-tool">
            <span>工具</span>
            <select id="import-tool" name="tool">
              <option value="codex">Codex</option>
              <option value="claude">Claude Code</option>
              <option value="gemini">Gemini CLI</option>
              <option value="opencode">OpenCode</option>
              <option value="openclaw">OpenClaw</option>
              <option value="hermes">Hermes</option>
            </select>
          </label>
          <label className="field" htmlFor="import-target">
            <span>导入客户端</span>
            <select id="import-target" name="target">
              <option value="codego">CodeGo</option>
              <option value="ccswitch">CC Switch</option>
            </select>
          </label>
          <Field name="name" label="配置名称" required defaultValue="CodeGo" maxLength={128} />
          <Field name="model" label="模型" maxLength={256} />
          <Field name="haiku-model" label="Claude Haiku 模型（可选）" maxLength={256} />
          <Field name="sonnet-model" label="Claude Sonnet 模型（可选）" maxLength={256} />
          <Field name="opus-model" label="Claude Opus 模型（可选）" maxLength={256} />
          <Button type="submit" disabled={createImport.isPending}>
            {createImport.isPending ? '生成中…' : '生成导入链接'}
          </Button>
        </form>
        {createImport.data && (
          <div className="form-stack">
            <p role="status">
              {createImport.data.token_name} 的导入链接已生成，有效期{' '}
              {createImport.data.expires_in_seconds} 秒。
            </p>
            <Button
              onClick={() => {
                try {
                  const link = new URL(createImport.data.deep_link)
                  if (!['codego:', 'ccswitch:'].includes(link.protocol))
                    throw new Error('服务器返回了无效导入链接')
                  window.location.assign(link.href)
                } catch (cause) {
                  setLinkError(cause instanceof Error ? cause : new Error('导入链接无效'))
                }
              }}
            >
              打开客户端并导入
            </Button>
            <Button variant="quiet" onClick={() => createImport.reset()}>
              隐藏导入链接
            </Button>
          </div>
        )}
      </section>
    </>
  )
}
