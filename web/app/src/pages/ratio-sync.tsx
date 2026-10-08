import { useTranslation } from '../lib/i18n'
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import type { Schema } from '../lib/types'
import { credits } from '../lib/format'
import { ratioPrice } from '../features/ratio-prices'
import { Button, ErrorMessage, Field, Loading, PageHeader } from '../components/ui'
import { DataTable } from '../components/data-table'

export default function RatioSyncPage() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const channels = useQuery(
    resourceOptions('ratio-channels', (signal) =>
      api.GET('/api/ratio_sync/channels', { signal }).then(unwrap),
    ),
  )
  const prices = useQuery(
    resourceOptions('catalog-prices', (signal) =>
      api.GET('/api/catalog/prices', { signal }).then(unwrap),
    ),
  )
  const [selected, setSelected] = useState<string[]>([])
  const [draft, setDraft] = useState<Schema['CatalogPrice'][]>([])
  const [error, setError] = useState<Error | null>(null)
  const fetch = useMutation({
    mutationFn: (body: Schema['RatioSyncInput']) =>
      api.POST('/api/ratio_sync/fetch', { body }).then(unwrap),
    onSuccess: () => setDraft([]),
  })
  const apply = useMutation({
    mutationFn: () => api.POST('/api/ratio_sync/apply', { body: { prices: draft } }).then(unwrap),
    onSuccess: () => {
      setDraft([])
      void client.invalidateQueries({ queryKey: ['catalog-prices'] })
    },
  })
  return (
    <>
      <PageHeader title="上游倍率同步" />
      <p>{t('先获取上游差异，再核对要应用的价格。获取差异不会修改现有价格。')}</p>
      <ErrorMessage error={error ?? channels.error ?? prices.error ?? fetch.error ?? apply.error} />
      {channels.isPending && <Loading />}
      <form
        className="form-panel"
        onSubmit={(event) => {
          event.preventDefault()
          setError(null)
          const form = new FormData(event.currentTarget)
          const baseURL = String(form.get('upstream-url')).trim()
          fetch.mutate({
            channel_ids: baseURL ? [] : selected,
            upstreams: baseURL
              ? [
                  {
                    name: String(form.get('upstream-name')).trim() || '自定义上游',
                    base_url: baseURL,
                    endpoint: String(form.get('upstream-endpoint')).trim() || '/api/pricing',
                  },
                ]
              : [],
            timeout: Number(form.get('timeout')),
          })
        }}
      >
        <fieldset className="full-width">
          <legend>{t('选择已有渠道或价格预设')}</legend>
          {channels.data?.map((channel) => (
            <label className="row-actions" key={String(channel.id)}>
              <input
                type="checkbox"
                checked={selected.includes(String(channel.id))}
                onChange={(event) =>
                  setSelected((old) =>
                    event.target.checked
                      ? [...old, String(channel.id)]
                      : old.filter((id) => id !== String(channel.id)),
                  )
                }
              />
              {channel.name}
            </label>
          ))}
        </fieldset>
        <Field name="upstream-name" label="自定义上游名称" />
        <Field
          name="upstream-url"
          label="自定义上游地址"
          type="url"
          placeholder="https://provider.example"
        />
        <Field name="upstream-endpoint" label="价格接口路径" defaultValue="/api/pricing" />
        <Field name="timeout" label="超时（秒）" type="number" min={1} required defaultValue={10} />
        <Button type="submit" disabled={fetch.isPending}>
          {fetch.isPending ? t('获取中…') : t('获取价格差异')}
        </Button>
      </form>
      {fetch.data && (
        <>
          <DataTable
            rows={fetch.data.test_results}
            rowKey={(item) => item.name}
            columns={[
              { label: '来源', render: (item) => item.name },
              {
                label: '结果',
                render: (item) =>
                  item.status === 'success' ? t('获取成功') : (item.error ?? t('获取失败')),
              },
            ]}
          />
          {Object.entries(fetch.data.differences).map(([model, fields]) => (
            <section className="section" key={model}>
              <h2>{model}</h2>
              <DataTable
                rows={Object.entries(fields)}
                rowKey={([field]) => field}
                columns={[
                  { label: '倍率项', render: ([field]) => field },
                  { label: '当前值', render: ([, item]) => String(item.current ?? t('未设置')) },
                  {
                    label: '上游值',
                    render: ([, item]) =>
                      Object.entries(item.upstreams).map(([name, value]) => (
                        <p key={name}>
                          {name}：{String(value ?? t('未提供'))}
                          {item.confidence[name] === false && t('（需人工核对）')}
                        </p>
                      )),
                  },
                ]}
              />
              <div className="row-actions">
                {fetch.data.test_results
                  .filter((result) => result.status === 'success')
                  .map((source) => (
                    <Button
                      key={source.name}
                      variant="quiet"
                      onClick={() => {
                        setError(null)
                        apply.reset()
                        try {
                          const next = ratioPrice(
                            model,
                            fields,
                            source.name,
                            prices.data?.find((price) => price.model === model),
                          )
                          setDraft((old) => [...old.filter((price) => price.model !== model), next])
                        } catch (cause) {
                          setError(cause instanceof Error ? cause : new Error('价格无法转换'))
                        }
                      }}
                    >
                      {t('采用')} {source.name}
                    </Button>
                  ))}
              </div>
            </section>
          ))}
          {!Object.keys(fetch.data.differences).length && (
            <p className="empty-state">{t('未发现可比较的价格差异，请同时检查上游获取结果。')}</p>
          )}
        </>
      )}
      {draft.length > 0 && (
        <section className="section">
          <h2>{t('待应用的价格')}</h2>
          <DataTable
            rows={draft}
            rowKey={(price) => price.model}
            columns={[
              { label: '模型', render: (price) => price.model },
              { label: '输入 / 百万 token', render: (price) => credits(price.input_per_mtok) },
              { label: '输出 / 百万 token', render: (price) => credits(price.output_per_mtok) },
              { label: '按次', render: (price) => credits(price.per_request) },
              {
                label: '操作',
                render: (price) => (
                  <Button
                    variant="quiet"
                    disabled={apply.isPending}
                    onClick={() =>
                      setDraft((old) => old.filter((item) => item.model !== price.model))
                    }
                  >
                    {t('移除')}
                  </Button>
                ),
              },
            ]}
          />
          <Button disabled={apply.isPending} onClick={() => apply.mutate()}>
            {t('确认应用')} {draft.length} {t('个模型价格')}
          </Button>
        </section>
      )}
      {apply.isSuccess && (
        <p role="status">
          {t('已更新')} {apply.data.updated} {t('个模型价格。')}
        </p>
      )}
    </>
  )
}
