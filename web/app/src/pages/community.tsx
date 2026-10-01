import { useState } from 'react'
import { useMutation, useSuspenseQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { useTranslation } from '../lib/i18n'
import { Button, ErrorMessage, PageHeader } from '../components/ui'

export default function CommunityPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const { data } = useSuspenseQuery(
    resourceOptions(
      'community',
      (signal) =>
        api
          .GET('/api/community/sellers', { signal, params: { query: { page, page_size: 20 } } })
          .then((result) => unwrap(result)),
      [page],
    ),
  )
  const rate = useMutation({
    mutationFn: (input: { id: string; stars: number }) =>
      api.POST('/api/community/channels/{id}/rating', {
        params: { path: { id: input.id } },
        body: { stars: input.stars },
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['community'] }),
  })
  return (
    <>
      <PageHeader title="社区" />
      <ErrorMessage error={rate.error} />
      <div className="catalog-grid">
        {data.items?.map((seller) => (
          <article className="product" key={seller.sub}>
            <h2>{seller.display_name || seller.username}</h2>
            <dl>
              <div>
                <dt>{t('渠道数')}</dt>
                <dd>{seller.channel_count}</dd>
              </div>
              <div>
                <dt>{t('评分')}</dt>
                <dd>
                  {BigInt(seller.rating_count) > 0n ? seller.average_score.toFixed(1) : '—'} ·{' '}
                  {seller.rating_count}
                </dd>
              </div>
            </dl>
            {seller.channels?.map((channel) => (
              <section key={channel.id}>
                <h3>{channel.name}</h3>
                <span>{channel.provider}</span>
                <form
                  className="filters"
                  onSubmit={(event) => {
                    event.preventDefault()
                    rate.mutate({
                      id: channel.id,
                      stars: Number(new FormData(event.currentTarget).get('stars')),
                    })
                  }}
                >
                  <label className="field" htmlFor={`rating-${channel.id}`}>
                    <span>{t('评分')}</span>
                    <select
                      id={`rating-${channel.id}`}
                      name="stars"
                      defaultValue={channel.viewer_stars || 5}
                    >
                      {[1, 2, 3, 4, 5].map((stars) => (
                        <option key={stars} value={stars}>
                          {stars} / 5
                        </option>
                      ))}
                    </select>
                  </label>
                  <Button variant="quiet" type="submit" disabled={rate.isPending}>
                    {t('提交评分')}
                  </Button>
                </form>
              </section>
            ))}
          </article>
        ))}
      </div>
      {!data.items?.length && <div className="empty-state">{t('暂无渠道主')}</div>}
      <div className="filters section">
        <Button variant="quiet" disabled={page === 1} onClick={() => setPage(page - 1)}>
          {t('上一页')}
        </Button>
        <Button
          variant="quiet"
          disabled={page * 20 >= Number(data.total)}
          onClick={() => setPage(page + 1)}
        >
          {t('下一页')}
        </Button>
      </div>
    </>
  )
}
