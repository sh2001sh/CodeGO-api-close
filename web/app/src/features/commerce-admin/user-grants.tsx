import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { resourceOptions } from '../../lib/queries'
import { useToast } from '../../hooks/use-toast'
import { useTranslation } from '../../lib/i18n'
import { credits, date } from '../../lib/format'
import { DataTable } from '../../components/data-table'
import { Button, Callout, confirmAction, ErrorMessage, Field, Loading } from '../../components/ui'

/** New operation ids follow the same pattern as components/balance-adjustment.tsx. */
function newOperationID(): string {
  return crypto.randomUUID()
}

export function UserGrants() {
  const { t } = useTranslation()
  const toast = useToast((s) => s.add)
  const client = useQueryClient()
  const [userID, setUserID] = useState('')
  const [requestID, setRequestID] = useState(newOperationID)
  const overview = useQuery({
    ...resourceOptions(
      'blind-box-user-overview',
      (signal) =>
        api
          .GET('/api/blind-box/admin/users/{id}/overview', {
            signal,
            params: { path: { id: userID } },
          })
          .then(unwrap),
      [userID],
    ),
    enabled: userID !== '',
  })
  const refresh = () => {
    setRequestID(newOperationID())
    void client.invalidateQueries({ queryKey: ['blind-box-user-overview'] })
  }
  const grant = useMutation({
    mutationFn: (count: number) =>
      api
        .POST('/api/blind-box/admin/users/{id}/grants', {
          params: { path: { id: userID } },
          body: { request_id: requestID, count, reason: '后台人工发放' },
        })
        .then(unwrap),
    onSuccess: () => {
      toast(t('已发放'), 'success')
      refresh()
    },
    onError: (error: Error) => toast(error.message, 'error'),
  })
  const revoke = useMutation({
    mutationFn: (count: number) =>
      api
        .POST('/api/blind-box/admin/users/{id}/revoke', {
          params: { path: { id: userID } },
          body: { request_id: requestID, count, reason: '后台人工撤销' },
        })
        .then(unwrap),
    onSuccess: () => {
      toast(t('已撤销'), 'success')
      refresh()
    },
    onError: (error: Error) => toast(error.message, 'error'),
  })
  const runGrant = (count: number) => {
    if (!Number.isInteger(count) || count < 1 || count > 100) {
      toast(t('数量须为 1 到 100 之间的整数'), 'error')
      return
    }
    grant.mutate(count)
  }
  const runRevoke = async (count: number) => {
    if (!Number.isInteger(count) || count < 1 || count > 100) {
      toast(t('数量须为 1 到 100 之间的整数'), 'error')
      return
    }
    const confirmed = await confirmAction({
      title: `${t('撤销 ')}${count}${t(' 个盲盒')}`,
      description: '将按发放顺序撤销该用户名下尚未开启的盲盒，操作不可恢复。',
      confirmLabel: '确认撤销',
      danger: true,
    })
    if (confirmed) revoke.mutate(count)
  }
  return (
    <section className="section">
      <h2>{t('用户盲盒发放与撤销')}</h2>
      <form
        className="filters"
        onSubmit={(event) => {
          event.preventDefault()
          const id = String(new FormData(event.currentTarget).get('grant-user-id') ?? '').trim()
          setUserID(id)
          setRequestID(newOperationID())
        }}
      >
        <Field name="grant-user-id" label="用户 ID" required placeholder="输入用户 ID 查询" />
        <Button type="submit">{t('查询')}</Button>
      </form>
      <ErrorMessage error={overview.error ?? grant.error ?? revoke.error} />
      {userID !== '' && overview.isPending && <Loading />}
      {overview.data && (
        <>
          <Callout tone="info">
            {t('该用户当前可用盲盒数量')}: {String(overview.data.available_count)}
          </Callout>
          <form
            className="form-panel"
            onSubmit={(event) => {
              event.preventDefault()
              const count = Number(new FormData(event.currentTarget).get('grant-count'))
              runGrant(count)
            }}
          >
            <Field
              name="grant-count"
              label="发放数量"
              type="number"
              min={1}
              max={100}
              required
              defaultValue={1}
            />
            <Button type="submit" disabled={grant.isPending}>
              {t('发放盲盒')}
            </Button>
          </form>
          <form
            className="form-panel"
            onSubmit={(event) => {
              event.preventDefault()
              const count = Number(new FormData(event.currentTarget).get('revoke-count'))
              void runRevoke(count)
            }}
          >
            <Field
              name="revoke-count"
              label="撤销数量"
              type="number"
              min={1}
              max={100}
              required
              defaultValue={1}
            />
            <Button type="submit" variant="danger" disabled={revoke.isPending}>
              {t('撤销盲盒')}
            </Button>
          </form>
          <h3>{t('道具')}</h3>
          <DataTable
            rows={overview.data.props ?? []}
            rowKey={(row) => String(row.id)}
            empty="暂无道具"
            columns={[
              { label: '道具', render: (row) => row.title },
              { label: '状态', render: (row) => row.status },
              { label: '剩余秒数', render: (row) => String(row.remaining_seconds) },
              { label: '到期时间', render: (row) => date(row.expires_at) },
            ]}
          />
          <h3>{t('池列表')}</h3>
          <DataTable
            rows={overview.data.pools ?? []}
            rowKey={(row) => String(row.id)}
            empty="暂无池"
            columns={[
              { label: '名称', render: (row) => row.name },
              { label: '单价', render: (row) => credits(row.price_micro), numeric: true },
            ]}
          />
        </>
      )}
    </section>
  )
}
