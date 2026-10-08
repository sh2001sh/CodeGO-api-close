import { useTranslation } from '../../lib/i18n'
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { resourceOptions } from '../../lib/queries'
import { date } from '../../lib/format'
import { DataTable } from '../../components/data-table'
import { Button, ErrorMessage, Loading, Status } from '../../components/ui'

type ReviewStatus = 'unreviewed' | 'acknowledged' | 'resolved' | 'false_positive'
type Review = { id: string; review_status: ReviewStatus; review_note: string }
function knownStatus(value: string): ReviewStatus {
  if (value === 'acknowledged' || value === 'resolved' || value === 'false_positive') return value
  return 'unreviewed'
}

export const securityOptions = (
  admin = false,
  page = 1,
  status: ReviewStatus | '' = 'unreviewed',
) => {
  const params = { query: { page, page_size: 20, review_status: status || undefined } }
  return admin
    ? resourceOptions(
        'market-admin-security',
        (signal) =>
          api.GET('/api/marketplace/admin/security-audit/events', { signal, params }).then(unwrap),
        [page, status],
      )
    : resourceOptions(
        'market-security',
        (signal) =>
          api.GET('/api/marketplace/security-audit/events', { signal, params }).then(unwrap),
        [page, status],
      )
}

export function MarketSecurity(props: { admin?: boolean }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [status, setStatus] = useState<ReviewStatus | ''>('unreviewed')
  const [page, setPage] = useState(1)
  const [review, setReview] = useState<Review | null>(null)
  const list = useQuery(securityOptions(props.admin, page, status))
  const resolve = useMutation({
    mutationFn: (input: Review) => {
      const body = { review_status: input.review_status, review_note: input.review_note }
      const params = { path: { id: input.id } }
      return props.admin
        ? api.PATCH('/api/marketplace/admin/security-audit/events/{id}', { params, body })
        : api.PATCH('/api/marketplace/security-audit/events/{id}', { params, body })
    },
    onSuccess: () => {
      setReview(null)
      return client.invalidateQueries({
        queryKey: [props.admin ? 'market-admin-security' : 'market-security'],
      })
    },
  })
  const exportPath = props.admin
    ? '/api/marketplace/admin/security-audit/events/export'
    : '/api/marketplace/security-audit/events/export'
  return (
    <section className="section" aria-label={t('安全审计')}>
      <div className="page-header">
        <h2>{t('安全审计')}</h2>
        <a
          className="button button-quiet"
          href={`${exportPath}?${new URLSearchParams(status ? { review_status: status } : {})}`}
          download
        >
          {t('导出筛选结果')}
        </a>
      </div>
      <label className="field filters" htmlFor="audit-filter">
        <span>{t('审核状态')}</span>
        <select
          id="audit-filter"
          value={status}
          onChange={(event) => {
            setStatus(event.target.value ? knownStatus(event.target.value) : '')
            setPage(1)
          }}
        >
          <option value="unreviewed">{t('待审核')}</option>
          <option value="acknowledged">{t('已确认')}</option>
          <option value="resolved">{t('已处理')}</option>
          <option value="false_positive">{t('误报')}</option>
          <option value="">{t('全部')}</option>
        </select>
      </label>
      <ErrorMessage error={resolve.error ?? list.error} />
      {list.isFetching && <Loading />}
      {resolve.isSuccess && <p role="status">{t('审核已保存。')}</p>}
      {review && (
        <form
          className="form-panel section"
          onSubmit={(event) => {
            event.preventDefault()
            resolve.mutate(review)
          }}
        >
          <p className="full-width">
            {t('审核事件')} {review.id}
          </p>
          <label className="field" htmlFor="audit-review-status">
            <span>{t('审核结果')}</span>
            <select
              id="audit-review-status"
              value={review.review_status}
              onChange={(event) =>
                setReview({ ...review, review_status: knownStatus(event.target.value) })
              }
            >
              <option value="unreviewed">{t('待审核')}</option>
              <option value="acknowledged">{t('已确认')}</option>
              <option value="resolved">{t('已处理')}</option>
              <option value="false_positive">{t('误报')}</option>
            </select>
          </label>
          <label className="field full-width" htmlFor="audit-review-note">
            <span>{t('审核备注')}</span>
            <textarea
              id="audit-review-note"
              rows={3}
              maxLength={1000}
              value={review.review_note}
              onChange={(event) => setReview({ ...review, review_note: event.target.value })}
            />
          </label>
          <Button type="submit" disabled={resolve.isPending}>
            {t('保存审核')}
          </Button>
          <Button variant="quiet" disabled={resolve.isPending} onClick={() => setReview(null)}>
            {t('取消')}
          </Button>
        </form>
      )}
      <DataTable
        rows={list.data?.items ?? []}
        rowKey={(row) => row.id}
        columns={[
          { label: '时间', render: (row) => date(row.created_at) },
          { label: '渠道', render: (row) => row.marketplace_channel_id || row.channel_id || '—' },
          { label: '风险', render: (row) => row.risk_code },
          { label: '来源', render: (row) => row.source },
          { label: '决策', render: (row) => row.decision },
          { label: '审核状态', render: (row) => <Status value={row.review_status} /> },
          { label: '审核备注', render: (row) => row.review_note || '—' },
          {
            label: '操作',
            render: (row) => (
              <Button
                variant="quiet"
                disabled={resolve.isPending}
                onClick={() => {
                  resolve.reset()
                  setReview({
                    id: row.id,
                    review_status: knownStatus(row.review_status),
                    review_note: row.review_note,
                  })
                }}
              >
                {t('审核')}
              </Button>
            ),
          },
        ]}
      />
      <nav className="pagination" aria-label={t('安全审计分页')}>
        <Button
          variant="quiet"
          disabled={page === 1 || list.isFetching}
          onClick={() => setPage(page - 1)}
        >
          {t('上一页')}
        </Button>
        <span>
          {t('第')} {page} {t('页，共')} {String(list.data?.total ?? 0)} {t('条')}
        </span>
        <Button
          variant="quiet"
          disabled={list.isFetching || BigInt(page) * 20n >= BigInt(list.data?.total ?? 0)}
          onClick={() => setPage(page + 1)}
        >
          {t('下一页')}
        </Button>
        <Button variant="quiet" disabled={list.isFetching} onClick={() => void list.refetch()}>
          {t('刷新')}
        </Button>
      </nav>
    </section>
  )
}
