import { useState } from 'react'
import { useMutation, useSuspenseQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { useTranslation } from '../lib/i18n'
import type { User, Schema } from '../lib/types'
import { DataTable } from '../components/data-table'
import { Button, ErrorMessage, Field, PageHeader, Status } from '../components/ui'
import { BalanceAdjustment } from '../components/balance-adjustment'
import { AdminTwoFactor } from '../features/admin-two-factor'

export default function UsersPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [before, setBefore] = useState('')
  const [editing, setEditing] = useState<User | null>(null)
  const [adjusting, setAdjusting] = useState(false)
  const { data } = useSuspenseQuery(
    resourceOptions(
      'users',
      (signal) =>
        api
          .GET('/api/user/', {
            signal,
            params: { query: { page_size: 50, before: before || undefined } },
          })
          .then((result) => unwrap(result)),
      [before],
    ),
  )
  const save = useMutation({
    mutationFn: (input: { id: User['id']; body: Schema['UserUpdate'] }) =>
      api.PUT('/api/user/{id}', { params: { path: { id: String(input.id) } }, body: input.body }),
    onSuccess: () => {
      setEditing(null)
      void queryClient.invalidateQueries({ queryKey: ['users'] })
    },
  })
  return (
    <>
      <PageHeader
        title="用户"
        action={
          <Button variant="quiet" onClick={() => setAdjusting(!adjusting)}>
            {t('余额调整')}
          </Button>
        }
      />
      {adjusting && <BalanceAdjustment onClose={() => setAdjusting(false)} />}
      <AdminTwoFactor />
      <ErrorMessage error={save.error} />
      {editing && (
        <form
          key={editing.id}
          className="form-panel"
          onSubmit={(event) => {
            event.preventDefault()
            const fields = new FormData(event.currentTarget)
            save.mutate({
              id: editing.id,
              body: {
                display_name: String(fields.get('display_name')),
                email: String(fields.get('email')),
                group: String(fields.get('group')),
                role: String(fields.get('role')),
                status: String(fields.get('status')),
              },
            })
          }}
        >
          <h2>{editing.username}</h2>
          <Field
            name="display_name"
            label="显示名称"
            defaultValue={editing.display_name}
            maxLength={100}
          />
          <Field name="email" label="邮箱" type="email" defaultValue={editing.email} />
          <Field name="group" label="分组" required defaultValue={editing.group} />
          <label className="field" htmlFor="user-role">
            <span>{t('角色')}</span>
            <select id="user-role" name="role" defaultValue={editing.role}>
              <option value="user">{t('user')}</option>
              <option value="admin">{t('admin')}</option>
              <option value="root">{t('root')}</option>
            </select>
          </label>
          <label className="field" htmlFor="user-status">
            <span>{t('状态')}</span>
            <select id="user-status" name="status" defaultValue={editing.status}>
              <option value="active">{t('active')}</option>
              <option value="disabled">{t('disabled')}</option>
            </select>
          </label>
          <Button disabled={save.isPending} type="submit">
            {t('保存')}
          </Button>
          <Button variant="quiet" type="button" onClick={() => setEditing(null)}>
            {t('取消')}
          </Button>
        </form>
      )}
      <DataTable
        rows={data ?? []}
        rowKey={(row) => row.id}
        columns={[
          { label: 'ID', render: (row) => row.id },
          { label: '用户名', render: (row) => row.username },
          { label: '显示名称', render: (row) => row.display_name },
          { label: '邮箱', render: (row) => row.email },
          { label: '角色', render: (row) => t(row.role) },
          { label: '分组', render: (row) => row.group },
          { label: '状态', render: (row) => <Status value={row.status} /> },
          {
            label: '操作',
            render: (row) => (
              <Button variant="quiet" onClick={() => setEditing(row)}>
                {t('编辑')}
              </Button>
            ),
          },
        ]}
      />
      <div className="filters section">
        <Button variant="quiet" disabled={!before} onClick={() => setBefore('')}>
          {t('返回最新')}
        </Button>
        <Button
          variant="quiet"
          disabled={(data?.length ?? 0) < 50}
          onClick={() => setBefore(String(data.at(-1)?.id ?? ''))}
        >
          {t('下一页')}
        </Button>
      </div>
    </>
  )
}
