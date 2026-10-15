import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions, keysOptions } from '../lib/queries'
import type { Schema } from '../lib/types'
import { date } from '../lib/format'
import { DataTable } from '../components/data-table'
import { Button, ErrorMessage, Field, Loading, PageHeader, Status } from '../components/ui'
import { ccSwitchImportURL } from '../features/keys/client-config'

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
    gcTime: 0,
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
          <h2>撤销设备</h2>
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
          empty="暂无已授权设备"
        />
      )}
      <section className="section">
        <h2>CC Switch</h2>
        <form
          className="form-panel"
          onChange={() => createImport.reset()}
          onSubmit={(event) => {
            event.preventDefault()
            setLinkError(null)
            const form = new FormData(event.currentTarget)
            createImport.mutate({
              target: 'ccswitch',
              tool: String(form.get('tool')),
              token_id: String(form.get('token_id')),
              name: String(form.get('name')),
              model: String(form.get('model')),
              haiku_model: String(form.get('haiku-model') ?? ''),
              sonnet_model: String(form.get('sonnet-model') ?? ''),
              opus_model: String(form.get('opus-model') ?? ''),
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
          <Field name="name" label="配置名称" required defaultValue="CodeGo" maxLength={100} />
          <Field name="model" label="模型" required maxLength={256} />
          <Field name="haiku-model" label="Claude Haiku 模型（可选）" maxLength={256} />
          <Field name="sonnet-model" label="Claude Sonnet 模型（可选）" maxLength={256} />
          <Field name="opus-model" label="Claude Opus 模型（可选）" maxLength={256} />
          <Button type="submit" disabled={createImport.isPending}>
            {createImport.isPending ? '生成中…' : '生成导入链接'}
          </Button>
        </form>
        {createImport.data && (
          <div className="form-stack">
            <strong role="status">{createImport.data.token_name}</strong>
            <Button
              onClick={() => {
                try {
                  window.location.assign(ccSwitchImportURL(createImport.data.deep_link))
                } catch (cause) {
                  setLinkError(cause instanceof Error ? cause : new Error('导入链接无效'))
                }
              }}
            >
              导入 CC Switch
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
