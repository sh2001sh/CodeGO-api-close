import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { resourceOptions } from '../../lib/queries'
import { useTranslation } from '../../lib/i18n'
import { Button, EmptyState, ErrorMessage, Field, Loading } from '../../components/ui'
import { MarketForm, text } from './form'

export function ShopReview() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const shops = useQuery(
    resourceOptions('market-shops-admin', (signal) =>
      api.GET('/api/marketplace/admin/shops', { signal }).then(unwrap),
    ),
  )
  const [selected, setSelected] = useState('')
  const [approved, setApproved] = useState(true)
  const review = useMutation({
    mutationFn: (reason: string) =>
      api.POST('/api/marketplace/admin/shops/{id}/review', {
        params: { path: { id: selected } },
        body: { approved, reason },
      }),
    onSuccess: () => {
      setSelected('')
      void client.invalidateQueries({ queryKey: ['market-shops-admin'] })
      void client.invalidateQueries({ queryKey: ['market-shops'] })
      void client.invalidateQueries({ queryKey: ['market-shop'] })
      void client.invalidateQueries({ queryKey: ['market-groups'] })
      void client.invalidateQueries({ queryKey: ['public-groups'] })
    },
  })
  const pending = shops.data?.filter((shop) => shop.review_status === 'pending') ?? []
  return (
    <details className="section shop-review">
      <summary>
        {t('店铺资料审核')} {pending.length > 0 && <span>({pending.length})</span>}
      </summary>
      <ErrorMessage error={shops.error ?? review.error} />
      {shops.isPending && <Loading />}
      {pending.map((shop) => (
        <article key={shop.id}>
          <h3>{shop.submitted_name || `${t('恢复系统名称')} · ${t('渠道店铺')} #${shop.id}`}</h3>
          <p className="subtle">
            {t('店铺 ID')} <code dir="ltr">{shop.id}</code> · {t('当前公开名称')}：{shop.name}
          </p>
          <p className="market-shop-description">
            {shop.submitted_description ?? shop.description}
          </p>
          {selected === shop.id ? (
            <MarketForm
              pending={review.isPending}
              submit="提交审核结果"
              onSubmit={(fields) => review.mutate(text(fields, 'shop-review-reason'))}
            >
              <label className="field" htmlFor="shop-review-result">
                <span>{t('审核结果')}</span>
                <select
                  id="shop-review-result"
                  value={approved ? 'approved' : 'rejected'}
                  onChange={(event) => setApproved(event.target.value === 'approved')}
                >
                  <option value="approved">{t('通过')}</option>
                  <option value="rejected">{t('拒绝')}</option>
                </select>
              </label>
              <Field
                name="shop-review-reason"
                label="审核说明"
                maxLength={500}
                required={!approved}
              />
              <Button variant="quiet" onClick={() => setSelected('')}>
                {t('取消')}
              </Button>
            </MarketForm>
          ) : (
            <Button
              variant="secondary"
              onClick={() => {
                setSelected(shop.id)
                setApproved(true)
              }}
            >
              {t('审核店铺资料')}
            </Button>
          )}
        </article>
      ))}
      {!shops.isPending && !shops.error && !pending.length && <EmptyState title="暂无待审核店铺" />}
    </details>
  )
}
