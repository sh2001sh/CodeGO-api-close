import { DataTable } from '../../components/data-table'
import type { Schema } from '../../lib/types'

function value(price: Schema['JSONValue'], key: string): string {
  if (!price || typeof price !== 'object' || !(key in price)) return '—'
  const item = Object.entries(price).find(([name]) => name === key)?.[1]
  return typeof item === 'number' || typeof item === 'string' ? String(item) : '—'
}

export function MarketPrices(props: { prices: Schema['JSONValue'] }) {
  const rows =
    props.prices && typeof props.prices === 'object' && !Array.isArray(props.prices)
      ? Object.entries(props.prices).map(([model, price]) => ({ model, price }))
      : []
  return (
    <DataTable
      rows={rows}
      rowKey={(row) => row.model}
      empty="此渠道未发布单独模型价格。"
      columns={[
        { label: '模型', render: (row) => row.model },
        {
          label: '计费方式',
          render: (row) => (value(row.price, 'billing_mode') === 'per_call' ? '按次' : '按 tokens'),
        },
        {
          label: '输入 credits / 百万 tokens',
          render: (row) => value(row.price, 'input_price_per_million'),
          numeric: true,
        },
        {
          label: '输出 credits / 百万 tokens',
          render: (row) => value(row.price, 'output_price_per_million'),
          numeric: true,
        },
        {
          label: '每次 credits',
          render: (row) => value(row.price, 'price_per_call'),
          numeric: true,
        },
        {
          label: '缓存读取 credits / 百万 tokens',
          render: (row) => value(row.price, 'cache_read_price_per_million'),
          numeric: true,
        },
        {
          label: '缓存写入 credits / 百万 tokens',
          render: (row) => value(row.price, 'cache_write_price_per_million'),
          numeric: true,
        },
      ]}
    />
  )
}
