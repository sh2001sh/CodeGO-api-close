import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api, unwrap } from '../../lib/api'
import { resourceOptions } from '../../lib/queries'
import { useTranslation } from '../../lib/i18n'
import { EmptyState, ErrorMessage, Field, Loading, Status } from '../../components/ui'
import { MarketForm, text } from './form'

export const myShopOptions = () =>
  resourceOptions('market-shop-mine', (signal) =>
    api.GET('/api/marketplace/shop/mine', { signal }).then(unwrap),
  )

export function ShopSettings() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const shop = useQuery(myShopOptions())
  const [saved, setSaved] = useState(false)
  const save = useMutation({
    mutationFn: (fields: FormData) =>
      api
        .PATCH('/api/marketplace/shop/mine', {
          body: { name: text(fields, 'shop-name'), description: text(fields, 'shop-description') },
        })
        .then(unwrap),
    onMutate: () => setSaved(false),
    onSuccess: () => {
      setSaved(true)
      void client.invalidateQueries({ queryKey: ['market-shop-mine'] })
    },
  })
  return (
    <section className="shop-settings section">
      <h2>{t('店铺资料')}</h2>
      <p className="subtle">{t('店铺名称和简介审核通过后展示；审核期间保留原有公开资料。')}</p>
      <ErrorMessage error={shop.error ?? save.error} />
      {shop.isPending && <Loading />}
      {shop.data && (
        <>
          <p>
            {t('当前公开名称')}：<strong>{shop.data.name}</strong> · {t('店铺 ID')}{' '}
            <code dir="ltr">{shop.data.id}</code>
          </p>
          <p>
            {t('审核状态')}：<Status value={shop.data.review_status ?? 'approved'} />
          </p>
          {shop.data.review_reason && <p className="notice">{shop.data.review_reason}</p>}
          <MarketForm
            key={shop.data.updated_at}
            pending={save.isPending}
            submit="提交店铺资料审核"
            onSubmit={(fields) => save.mutate(fields)}
          >
            <Field
              name="shop-name"
              label="店铺名称"
              maxLength={40}
              defaultValue={
                shop.data.review_status === 'pending' || shop.data.review_status === 'rejected'
                  ? (shop.data.submitted_name ?? '')
                  : shop.data.name === `渠道店铺 #${shop.data.id}`
                    ? ''
                    : shop.data.name
              }
              hint="使用清晰的服务名称，不得包含广告、联系方式、外链、冒充或违规内容。"
            />
            <p className="subtle">
              {t('名称可留空以恢复默认名称；自定义名称为 2–40 字，简介最多 200 字。')}
            </p>
            <label className="field" htmlFor="shop-description">
              <span>{t('店铺简介')}</span>
              <textarea
                id="shop-description"
                name="shop-description"
                maxLength={200}
                rows={3}
                defaultValue={shop.data.submitted_description ?? shop.data.description}
              />
            </label>
          </MarketForm>
        </>
      )}
      {!shop.isPending && !shop.error && !shop.data && (
        <EmptyState title="提交渠道后可设置店铺资料" />
      )}
      {saved && (
        <p className="notice" role="status">
          {t('店铺资料已提交审核')}
        </p>
      )}
    </section>
  )
}
