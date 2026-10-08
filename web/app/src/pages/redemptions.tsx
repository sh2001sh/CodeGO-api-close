import { useTranslation } from '../lib/i18n'
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import type { Schema } from '../lib/types'
import { resourceOptions } from '../lib/queries'
import { credits, date, toMicroCredits } from '../lib/format'
import { DataTable } from '../components/data-table'
import { Button, ErrorMessage, Field, Loading, PageHeader, Status } from '../components/ui'
import { errorFrom, positiveID } from '../features/commerce/amounts'

function redemptionBenefit(code: Schema['RedemptionCode'], t: (key: string) => string) {
  if (code.redeem_type === 'subscription')
    return `${t('订阅')} · ${code.plan_title || `${t('套餐')} ${code.plan_id}`}`
  if (code.redeem_type === 'blind_box') return `${t('盲盒')} × ${code.blind_box_quantity}`
  return credits(code.credits)
}

export const redemptionsOptions = (before = '') =>
  resourceOptions(
    'redemptions',
    (signal) =>
      api
        .GET('/api/redemption/', {
          signal,
          params: { query: { before: before || undefined, limit: 50 } },
        })
        .then((result) => unwrap(result)),
    [before],
  )

export default function RedemptionsPage() {
  const { t } = useTranslation()
  useSuspenseQuery(redemptionsOptions())
  const [before, setBefore] = useState('')
  const [error, setError] = useState<Error | null>(null)
  const [issued, setIssued] = useState<Schema['RedemptionCode'] | null>(null)
  const [revoking, setRevoking] = useState<Schema['RedemptionCode'] | null>(null)
  const [copied, setCopied] = useState(false)
  const [redeemType, setRedeemType] = useState<'credits' | 'subscription' | 'blind_box'>('credits')
  const queryClient = useQueryClient()
  const list = useQuery(redemptionsOptions(before))
  const plans = useQuery({
    ...resourceOptions('admin-plans', (signal) =>
      api.GET('/api/subscription/admin/plans', { signal }).then(unwrap),
    ),
    enabled: redeemType === 'subscription',
  })
  const issue = useMutation({
    mutationFn: (body: Schema['IssueRedemptionInput']) =>
      api.POST('/api/redemption/', { body }).then((result) => unwrap(result)),
    onSuccess: (code) => {
      setIssued(code)
      setCopied(false)
      setBefore('')
      void queryClient.invalidateQueries({ queryKey: ['redemptions'] })
    },
  })
  const revoke = useMutation({
    mutationFn: (id: Schema['RedemptionCode']['id']) =>
      api.DELETE('/api/redemption/{id}', { params: { path: { id } } }),
    onSuccess: () => {
      setRevoking(null)
      void queryClient.invalidateQueries({ queryKey: ['redemptions'] })
    },
  })
  return (
    <>
      <PageHeader
        title="兑换码管理"
        action={
          <Button variant="quiet" disabled={list.isFetching} onClick={() => void list.refetch()}>
            {t('刷新')}
          </Button>
        }
      />
      <ErrorMessage error={error ?? issue.error ?? revoke.error ?? plans.error} />
      <section className="section">
        <h2>{t('发行兑换码')}</h2>
        <form
          className="form-panel"
          onSubmit={(event) => {
            event.preventDefault()
            setError(null)
            try {
              const form = new FormData(event.currentTarget)
              const expiry = String(form.get('redemption-expiry') ?? '')
              const expiresAt = expiry ? new Date(expiry) : null
              if (
                expiresAt &&
                (Number.isNaN(expiresAt.getTime()) || expiresAt.getTime() <= Date.now())
              )
                throw new Error('到期时间必须晚于当前时间')
              const base = {
                name: String(form.get('redemption-name') ?? '').trim(),
                expires_at: expiresAt?.toISOString() ?? null,
              }
              let body: Schema['IssueRedemptionInput']
              if (redeemType === 'credits')
                body = {
                  ...base,
                  redeem_type: 'credits',
                  credits: BigInt(toMicroCredits(String(form.get('redemption-credits') ?? ''))),
                }
              else if (redeemType === 'subscription')
                body = {
                  ...base,
                  redeem_type: 'subscription',
                  credits: 0,
                  plan_id: BigInt(positiveID(String(form.get('redemption-plan') ?? ''))),
                }
              else {
                const quantity = String(form.get('redemption-boxes') ?? '')
                if (!/^\d+$/.test(quantity) || Number(quantity) < 1 || Number(quantity) > 100)
                  throw new Error('盲盒数量须为 1 至 100 的整数')
                body = {
                  ...base,
                  redeem_type: 'blind_box',
                  credits: 0,
                  blind_box_quantity: Number(quantity),
                }
              }
              issue.mutate(body)
            } catch (cause) {
              setError(errorFrom(cause))
            }
          }}
        >
          <Field name="redemption-name" label="名称" required maxLength={200} />
          <label className="field" htmlFor="redemption-type">
            <span>{t('兑换类型')}</span>
            <select
              id="redemption-type"
              value={redeemType}
              onChange={(event) => {
                const value = event.target.value
                if (value === 'credits' || value === 'subscription' || value === 'blind_box') {
                  setRedeemType(value)
                  setError(null)
                }
              }}
            >
              <option value="credits">{t('钱包额度')}</option>
              <option value="subscription">{t('订阅套餐')}</option>
              <option value="blind_box">{t('盲盒')}</option>
            </select>
          </label>
          {redeemType === 'credits' && (
            <Field name="redemption-credits" label="额度 credits" required placeholder="100" />
          )}
          {redeemType === 'subscription' && (
            <label className="field" htmlFor="redemption-plan">
              <span>{t('兑换套餐')}</span>
              <select
                id="redemption-plan"
                name="redemption-plan"
                required
                disabled={plans.isPending}
              >
                <option value="">{plans.isPending ? t('正在加载套餐…') : t('请选择套餐')}</option>
                {(plans.data ?? []).map((plan) => (
                  <option key={String(plan.id)} value={String(plan.id)}>
                    {plan.name}
                  </option>
                ))}
              </select>
            </label>
          )}
          {redeemType === 'blind_box' && (
            <Field name="redemption-boxes" label="盲盒数量" required placeholder="1 至 100" />
          )}
          <Field name="redemption-expiry" label="到期时间（可选）" type="datetime-local" />
          <Button
            disabled={
              issue.isPending ||
              !!issued?.key ||
              (redeemType === 'subscription' && !plans.data?.length)
            }
            type="submit"
          >
            {issue.isPending ? t('发行中…') : t('发行兑换码')}
          </Button>
        </form>
        {issued?.key && (
          <div className="section" role="status">
            <p>{t('兑换码仅在发行时显示一次，请立即保存。')}</p>
            <code className="secret-value">{issued.key}</code>
            <div className="row-actions">
              <Button
                variant="quiet"
                onClick={async () => {
                  try {
                    await navigator.clipboard.writeText(issued.key ?? '')
                    setCopied(true)
                  } catch (cause) {
                    setError(errorFrom(cause))
                  }
                }}
              >
                {copied ? t('已复制') : t('复制兑换码')}
              </Button>
              <Button variant="quiet" onClick={() => setIssued(null)}>
                {t('已保存，关闭')}
              </Button>
            </div>
          </div>
        )}
      </section>
      {revoking && (
        <section className="section">
          <p>
            {t('撤销兑换码“')}
            {revoking.name}”（{redemptionBenefit(revoking, t)}
            {t('）后将无法兑换。')}
          </p>
          <div className="row-actions">
            <Button
              variant="danger"
              disabled={revoke.isPending}
              onClick={() => revoke.mutate(revoking.id)}
            >
              {t('确认撤销')}
            </Button>
            <Button variant="quiet" disabled={revoke.isPending} onClick={() => setRevoking(null)}>
              {t('取消')}
            </Button>
          </div>
        </section>
      )}
      <ErrorMessage error={list.error} />
      {list.isFetching && <Loading />}
      <DataTable
        rows={list.data ?? []}
        rowKey={(row) => row.id}
        empty="暂无兑换码。填写上方表单发行第一张。"
        columns={[
          { label: '名称', render: (row) => row.name },
          { label: '兑换权益', render: (row) => redemptionBenefit(row, t) },
          {
            label: '状态',
            render: (row) => (
              <Status
                value={
                  row.state === 'used' ? '已兑换' : row.state === 'revoked' ? '已撤销' : row.state
                }
              />
            ),
          },
          { label: '到期时间', render: (row) => date(row.expires_at) },
          { label: '兑换用户', render: (row) => (row.claimed_by ? String(row.claimed_by) : '—') },
          {
            label: '操作',
            render: (row) =>
              row.state === 'active' ? (
                <Button
                  variant="danger"
                  onClick={() => {
                    revoke.reset()
                    setRevoking(row)
                  }}
                >
                  {t('撤销')}
                </Button>
              ) : (
                '—'
              ),
          },
        ]}
      />
      <nav className="pagination" aria-label={t('兑换码分页')}>
        <Button variant="quiet" disabled={!before || list.isFetching} onClick={() => setBefore('')}>
          {t('返回首页')}
        </Button>
        <Button
          variant="quiet"
          disabled={list.isFetching || (list.data?.length ?? 0) < 50}
          onClick={() => setBefore(String(list.data?.at(-1)?.id ?? ''))}
        >
          {t('下一页')}
        </Button>
      </nav>
    </>
  )
}
