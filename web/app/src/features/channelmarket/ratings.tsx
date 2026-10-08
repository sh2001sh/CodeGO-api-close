import { useEffect, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { Star } from 'lucide-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { resourceOptions } from '../../lib/queries'
import { useTranslation } from '../../lib/i18n'
import { Button, ErrorMessage, Loading } from '../../components/ui'

export function RatingSummary(props: {
  average_score: number
  rating_count: number | string | bigint
}) {
  const { t } = useTranslation()
  const count = String(props.rating_count)
  return (
    <span className="market-rating-summary">
      <Star size={15} aria-hidden />
      <strong>
        {count === '0' ? t('暂无评分') : `${(props.average_score / 2).toFixed(1)} / 5`}
      </strong>
      <span className="subtle">
        {count} {t('位评价用户')}
      </span>
    </span>
  )
}

export const groupRatingOptions = (id: string) =>
  resourceOptions(
    'market-rating',
    (signal) =>
      api
        .GET('/api/marketplace/groups/{id}/rating', { params: { path: { id } }, signal })
        .then(unwrap),
    [id],
  )

const reasonLabels: Record<string, string> = {
  login_required: '登录后可评价服务',
  self_rating: '渠道主不能评价自己的服务',
  usage_required: '真实使用此分组后可以评价，服务失败的调用也可符合资格。',
}

export function GroupRatings(props: { id: string; signedIn: boolean; returnTo: string }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const summary = useQuery(groupRatingOptions(props.id))
  const [stars, setStars] = useState(0)
  const [saved, setSaved] = useState(false)
  useEffect(() => {
    setStars(summary.data?.channel.viewer_stars ?? 0)
  }, [summary.data?.channel.viewer_stars])
  const submit = useMutation({
    mutationFn: () =>
      api
        .POST('/api/marketplace/groups/{id}/rating', {
          params: { path: { id: props.id } },
          body: { stars },
        })
        .then(unwrap),
    onMutate: () => setSaved(false),
    onSuccess: () => {
      setSaved(true)
      void client.invalidateQueries({ queryKey: ['market-rating', props.id] })
      void client.invalidateQueries({ queryKey: ['market-shops'] })
      void client.invalidateQueries({ queryKey: ['market-shop'] })
      void client.invalidateQueries({ queryKey: ['market-groups'] })
      void client.invalidateQueries({ queryKey: ['public-groups'] })
    },
  })
  return (
    <section className="market-ratings" aria-label={t('用户评价')}>
      <h3>{t('用户评价')}</h3>
      <ErrorMessage error={summary.error ?? submit.error} />
      {summary.isPending && <Loading />}
      {!props.signedIn && (
        <Link
          to="/sign-in"
          search={{ returnTo: props.returnTo }}
          className="button button-secondary"
        >
          {t('登录后可评价服务')}
        </Link>
      )}
      {summary.data && (
        <>
          <RatingSummary {...summary.data.channel} />
          <p className="subtle">{t('每位用户对每个分组保留一条评分；再次提交会更新原评分。')}</p>
          {props.signedIn && summary.data.can_rate ? (
            <form
              onSubmit={(event) => {
                event.preventDefault()
                if (stars >= 1 && stars <= 5 && !submit.isPending) submit.mutate()
              }}
            >
              <fieldset disabled={submit.isPending}>
                <legend>{t('我的评分')}</legend>
                <div className="market-rating-stars">
                  {[1, 2, 3, 4, 5].map((value) => (
                    <label
                      key={value}
                      className="market-rating-option"
                      data-selected={stars === value}
                    >
                      <input
                        type="radio"
                        name={`rating-${props.id}`}
                        value={value}
                        checked={stars === value}
                        onChange={() => {
                          setStars(value)
                          setSaved(false)
                        }}
                      />
                      <Star size={18} aria-hidden fill={value <= stars ? 'currentColor' : 'none'} />
                      <span>{value} / 5</span>
                    </label>
                  ))}
                </div>
              </fieldset>
              <Button
                type="submit"
                disabled={!stars || submit.isPending}
                loading={submit.isPending}
              >
                {t(summary.data.channel.viewer_stars ? '更新评分' : '提交评分')}
              </Button>
            </form>
          ) : props.signedIn ? (
            <p className="subtle">
              {t(
                reasonLabels[summary.data.eligibility_reason] ??
                  '暂不符合评分条件，请真实使用此分组后再评价。',
              )}
            </p>
          ) : null}
        </>
      )}
      {saved && (
        <p className="notice" role="status">
          {t('评分已保存，社区展示将同步更新。')}
        </p>
      )}
    </section>
  )
}
