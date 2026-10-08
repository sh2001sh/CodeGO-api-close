import { useTranslation } from '../../lib/i18n'
import { useMutation, useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { resourceOptions } from '../../lib/queries'
import { Button, ErrorMessage, Field, Loading, Status } from '../../components/ui'
import { DataTable } from '../../components/data-table'
import { date } from '../../lib/format'
import { MarketForm, text, factor, integer } from './form'

export function OwnerAccess(props: { id: string; internalID: number | string | bigint }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const params = { path: { id: props.id } }
  const blocks = useQuery(
    resourceOptions(
      'market-blocks',
      (signal) =>
        api
          .GET('/api/marketplace/channels/{id}/user-blocks', { params, signal })
          .then((result) => unwrap(result)),
      [props.id],
    ),
  )
  const timed = useQuery(
    resourceOptions(
      'market-times',
      (signal) =>
        api
          .GET('/api/marketplace/channels/{id}/time-range-multipliers', { params, signal })
          .then((result) => unwrap(result)),
      [props.id],
    ),
  )
  const multipliers = useSuspenseQuery(
    resourceOptions('market-multipliers', (signal) =>
      api
        .GET('/api/marketplace/channels/mine/user-multipliers', { signal })
        .then((result) => unwrap(result)),
    ),
  ).data
  const refresh = () => {
    void client.invalidateQueries({ queryKey: ['market-blocks'] })
    void client.invalidateQueries({ queryKey: ['market-times'] })
    void client.invalidateQueries({ queryKey: ['market-multipliers'] })
  }
  const block = useMutation({
    mutationFn: (body: { user_id: bigint; blocked: boolean }) =>
      api.POST('/api/marketplace/channels/{id}/user-block', { params, body }),
    onSuccess: refresh,
  })
  const multiplier = useMutation({
    mutationFn: (body: { user_id: bigint; multiplier: number | null }) =>
      api.POST('/api/marketplace/channels/{id}/user-multiplier', { params, body }),
    onSuccess: refresh,
  })
  const batch = useMutation({
    mutationFn: (body: {
      targets: { channel_id: bigint; user_id: bigint }[]
      multiplier: number | null
    }) => api.POST('/api/marketplace/channels/mine/user-multipliers/batch', { body }),
    onSuccess: refresh,
  })
  const addTime = useMutation({
    mutationFn: (body: {
      start_timestamp: number
      end_timestamp: number
      multiplier: number
      label: string
    }) => api.POST('/api/marketplace/channels/{id}/time-range-multipliers', { params, body }),
    onSuccess: refresh,
  })
  const deleteTime = useMutation({
    mutationFn: (ruleId: string) =>
      api.DELETE('/api/marketplace/channels/{id}/time-range-multipliers/{ruleId}', {
        params: { path: { id: props.id, ruleId } },
      }),
    onSuccess: refresh,
  })
  return (
    <section className="section">
      <h2>{t('用户访问与专属倍率')}</h2>
      <ErrorMessage
        error={
          block.error ??
          multiplier.error ??
          batch.error ??
          addTime.error ??
          deleteTime.error ??
          blocks.error ??
          timed.error
        }
      />
      <MarketForm
        pending={multiplier.isPending}
        submit="设置用户倍率"
        onSubmit={(fields) =>
          multiplier.mutate({
            user_id: integer(fields, 'multiplier-user'),
            multiplier: fields.has('clear') ? null : factor(fields, 'user-multiplier'),
          })
        }
      >
        <Field name="multiplier-user" label="用户 ID" required />
        <Field name="user-multiplier" label="专属倍率" defaultValue="1" required />
        <label>
          <input type="checkbox" name="clear" /> {t('恢复公开倍率')}
        </label>
      </MarketForm>
      <DataTable
        rows={multipliers.filter((item) => String(item.channel_id) === String(props.internalID))}
        rowKey={(row) => String(row.user_id)}
        columns={[
          { label: '用户', render: (row) => String(row.user_id) },
          {
            label: '专属倍率',
            render: (row) => Number(row.multiplier_ppm) / 1_000_000,
            numeric: true,
          },
          { label: '更新时间', render: (row) => date(row.updated_at) },
        ]}
      />
      <details className="section">
        <summary>{t('批量调整用户倍率')}</summary>
        <MarketForm
          pending={batch.isPending}
          onSubmit={(fields) => {
            const users = text(fields, 'batch-users')
              .split(',')
              .map((user) => user.trim())
            if (users.some((user) => !/^[1-9]\d*$/.test(user)))
              throw new Error('用户 ID 使用逗号分隔的正整数')
            batch.mutate({
              targets: users.map((user) => ({
                channel_id: BigInt(props.internalID),
                user_id: BigInt(user),
              })),
              multiplier: fields.has('batch-clear') ? null : factor(fields, 'batch-multiplier'),
            })
          }}
        >
          <Field name="batch-users" label="用户 ID（逗号分隔）" required />
          <Field name="batch-multiplier" label="批量专属倍率" required defaultValue="1" />
          <label>
            <input type="checkbox" name="batch-clear" /> {t('全部恢复公开倍率')}
          </label>
        </MarketForm>
      </details>
      <MarketForm
        pending={block.isPending}
        submit="封禁用户"
        onSubmit={(fields) =>
          block.mutate({ user_id: integer(fields, 'block-user'), blocked: true })
        }
      >
        <Field name="block-user" label="封禁用户 ID" required />
      </MarketForm>
      {blocks.isPending ? (
        <Loading />
      ) : (
        <DataTable
          rows={blocks.data ?? []}
          rowKey={(row) => String(row.user_id)}
          columns={[
            { label: '用户', render: (row) => row.username || String(row.user_id) },
            { label: '封禁时间', render: (row) => date(row.blocked_at) },
            {
              label: '操作',
              render: (row) => (
                <Button
                  variant="quiet"
                  disabled={block.isPending}
                  onClick={() => block.mutate({ user_id: BigInt(row.user_id), blocked: false })}
                >
                  {t('解除封禁')}
                </Button>
              ),
            },
          ]}
        />
      )}
      <h3 className="section">{t('限时倍率')}</h3>
      <MarketForm
        pending={addTime.isPending}
        submit="添加限时倍率"
        onSubmit={(fields) => {
          const start = Math.floor(new Date(text(fields, 'starts')).getTime() / 1000)
          const end = Math.floor(new Date(text(fields, 'ends')).getTime() / 1000)
          if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start)
            throw new Error('结束时间必须晚于开始时间')
          addTime.mutate({
            start_timestamp: start,
            end_timestamp: end,
            multiplier: factor(fields, 'time-multiplier'),
            label: text(fields, 'time-label'),
          })
        }}
      >
        <Field name="starts" label="开始时间" type="datetime-local" required />
        <Field name="ends" label="结束时间" type="datetime-local" required />
        <Field name="time-multiplier" label="限时倍率" required defaultValue="1" />
        <Field name="time-label" label="说明" maxLength={255} />
      </MarketForm>
      {timed.isPending ? (
        <Loading />
      ) : (
        <DataTable
          rows={timed.data ?? []}
          rowKey={(row) => row.id}
          columns={[
            { label: '说明', render: (row) => row.label },
            { label: '开始时间', render: (row) => date(Number(row.start_timestamp) * 1000) },
            { label: '结束时间', render: (row) => date(Number(row.end_timestamp) * 1000) },
            { label: '倍率', render: (row) => String(row.multiplier), numeric: true },
            {
              label: '操作',
              render: (row) => (
                <Button
                  variant="danger"
                  disabled={deleteTime.isPending}
                  onClick={() => deleteTime.mutate(row.id)}
                >
                  {t('删除规则')}
                </Button>
              ),
            },
          ]}
        />
      )}
    </section>
  )
}

export function OwnerBargains() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const bargains = useSuspenseQuery(
    resourceOptions('market-bargains', (signal) =>
      api
        .GET('/api/marketplace/channels/mine/bargain-requests', { signal })
        .then((result) => unwrap(result)),
    ),
  ).data
  const mutation = useMutation({
    mutationFn: (input: { id: string; accept: boolean }) =>
      api.POST('/api/marketplace/channels/mine/bargain-requests/{id}/resolve', {
        params: { path: { id: input.id } },
        body: { accept: input.accept, note: '' },
      }),
    onSuccess: () => client.invalidateQueries({ queryKey: ['market-bargains'] }),
  })
  return (
    <section className="section">
      <h2>{t('议价申请')}</h2>
      <ErrorMessage error={mutation.error} />
      <DataTable
        rows={bargains}
        rowKey={(row) => row.id}
        columns={[
          { label: '用户', render: (row) => String(row.user_id) },
          { label: '期望倍率', render: (row) => String(row.proposed_multiplier), numeric: true },
          { label: '理由', render: (row) => row.reason },
          { label: '状态', render: (row) => <Status value={row.status} /> },
          {
            label: '操作',
            render: (row) => (
              <div className="row-actions">
                <Button
                  variant="quiet"
                  disabled={mutation.isPending || row.status !== 'pending'}
                  onClick={() => mutation.mutate({ id: row.id, accept: true })}
                >
                  {t('接受')}
                </Button>
                <Button
                  variant="danger"
                  disabled={mutation.isPending || row.status !== 'pending'}
                  onClick={() => mutation.mutate({ id: row.id, accept: false })}
                >
                  {t('拒绝')}
                </Button>
              </div>
            ),
          },
        ]}
      />
    </section>
  )
}
