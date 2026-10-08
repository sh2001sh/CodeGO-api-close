import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { useTranslation } from '../lib/i18n'
import { groupFavoritesOptions } from '../lib/public-catalog'
import {
  Button,
  CopyButton,
  EmptyState,
  ErrorMessage,
  Loading,
  PageHeader,
  Status,
} from '../components/ui'
import { DataTable } from '../components/data-table'

export default function GroupFavoritesPage() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [page, setPage] = useState(1)
  const [message, setMessage] = useState('')
  const favorites = useQuery(groupFavoritesOptions(page))
  const remove = useMutation({
    mutationFn: (group_id: string) =>
      api
        .PUT('/api/marketplace/group-favorites', { body: { group_id, favorite: false } })
        .then(unwrap),
    onSuccess: async () => {
      setMessage('已取消分组收藏')
      if (favorites.data?.items.length === 1 && page > 1) setPage(page - 1)
      await client.invalidateQueries({ queryKey: ['group-favorites'] })
    },
  })
  const pagination = favorites.data?.pagination
  return (
    <>
      <PageHeader
        title="分组收藏"
        description="保存常用分组，随时查看模型与价格，再进入市场选择路由。"
        action={
          <Link to="/channel-market" className="button button-secondary">
            {t('浏览渠道市场')}
          </Link>
        }
      />
      <ErrorMessage error={favorites.error ?? remove.error} />
      {favorites.isError && (
        <Button variant="secondary" onClick={() => void favorites.refetch()}>
          {t('重试')}
        </Button>
      )}
      {message && (
        <p role="status" className="notice">
          {t(message)}
        </p>
      )}
      {favorites.isPending && <Loading rows={5} />}
      {favorites.data &&
        (favorites.data.items.length === 0 ? (
          <EmptyState
            title="尚未收藏分组"
            description="在渠道市场点击收藏分组，之后可在这里快速找到。"
            action={
              <Link to="/channel-market" className="button button-primary">
                {t('浏览渠道市场')}
              </Link>
            }
          />
        ) : (
          <>
            <p className="subtle">
              {t('已收藏 {count} 个分组', { count: pagination?.total ?? 0 })}
            </p>
            <DataTable
              rows={favorites.data.items}
              rowKey={(group) => group.group_id}
              columns={[
                {
                  label: '分组',
                  render: (group) => (
                    <div>
                      <strong>{group.system_display_name || t('暂不可访问的分组')}</strong>
                      <div className="actions subtle">
                        <span>{t('分组 ID')}</span>
                        <code dir="ltr">{group.id}</code>
                        <CopyButton value={group.id} label="复制分组 ID" />
                      </div>
                    </div>
                  ),
                },
                {
                  label: '模型',
                  render: (group) =>
                    group.available ? (
                      <span>
                        {group.declared_models?.slice(0, 3).map((name) => (
                          <code key={name} className="badge">
                            {name}
                          </code>
                        ))}
                        {(group.declared_models?.length ?? 0) > 3 && (
                          <span> +{group.declared_models!.length - 3}</span>
                        )}
                      </span>
                    ) : (
                      '—'
                    ),
                },
                {
                  label: '计费倍率',
                  render: (group) =>
                    group.available && group.multiplier != null ? (
                      <span dir="ltr" className="tabular">
                        {String(group.multiplier)}×
                      </span>
                    ) : (
                      '—'
                    ),
                },
                {
                  label: '状态',
                  render: (group) =>
                    group.available ? (
                      <Status value="active" />
                    ) : (
                      <span className="subtle">{t('暂不可访问')}</span>
                    ),
                },
                {
                  label: '操作',
                  render: (group) => (
                    <div className="actions">
                      {group.available && (
                        <Link
                          to="/channel-market"
                          search={{ group: group.id }}
                          className="button button-secondary"
                        >
                          {t('查看分组')}
                        </Link>
                      )}
                      <Button
                        variant="quiet"
                        disabled={remove.isPending}
                        onClick={() => remove.mutate(group.group_id)}
                      >
                        {t('取消收藏')}
                      </Button>
                    </div>
                  ),
                },
              ]}
              empty="尚未收藏分组"
            />
            {pagination && pagination.total > pagination.page_size && (
              <nav className="actions" aria-label={t('分组收藏分页')}>
                <Button
                  variant="secondary"
                  disabled={page <= 1 || favorites.isFetching}
                  onClick={() => setPage(page - 1)}
                >
                  {t('上一页')}
                </Button>
                <span className="subtle">
                  {t('第 {page} 页，共 {total} 个分组', { page, total: pagination.total })}
                </span>
                <Button
                  variant="secondary"
                  disabled={page * pagination.page_size >= pagination.total || favorites.isFetching}
                  onClick={() => setPage(page + 1)}
                >
                  {t('下一页')}
                </Button>
              </nav>
            )}
          </>
        ))}
    </>
  )
}
