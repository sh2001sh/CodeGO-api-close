import { useTranslation } from '../../lib/i18n'
import type { PublicCatalogModel } from '../../lib/public-catalog'

const unitLabels: Record<string, string> = {
  request: '每次请求',
  image: '每张图片',
  audio_character: '每个音频字符',
  audio_second: '每秒音频',
  video_second: '每秒视频',
}

export function ModelGroupPrice(props: { group: PublicCatalogModel['groups'][number] }) {
  const { t } = useTranslation()
  const price = props.group.price
  if (!price) return <span className="subtle">{t('暂未提供价格')}</span>
  if (price.mode === 'expression' || price.mode === 'tiered_expr')
    return (
      <details>
        <summary>{t('按动态规则计价')}</summary>
        <p className="subtle">{t('表达式以 credits / 百万 tokens 为单位，再乘以当前分组倍率。')}</p>
        <code dir="ltr">{price.expression}</code>
      </details>
    )
  if (price.mode === 'per_request')
    return (
      <span className="tabular">
        <bdi dir="ltr">{price.per_unit} credits</bdi> / {t(unitLabels[price.unit] ?? price.unit)}
      </span>
    )
  return (
    <dl className="model-price-lines">
      {[
        ['输入 credits / 百万 tokens', price.input_per_million],
        ['输出 credits / 百万 tokens', price.output_per_million],
        ['缓存读取 credits / 百万 tokens', price.cache_read_per_million],
        ['缓存写入 credits / 百万 tokens', price.cache_write_per_million],
      ].map(([label, value]) => (
        <div key={label}>
          <dt className="subtle">{t(label)}</dt>
          <dd className="tabular" dir="ltr">
            {value}
          </dd>
        </div>
      ))}
    </dl>
  )
}
