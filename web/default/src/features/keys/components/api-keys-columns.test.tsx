import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { createInstance } from 'i18next'
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import type { ApiKey } from '../types'
import { useApiKeysColumns } from './api-keys-columns'

function GroupCells() {
  const table = useReactTable({
    // Only the group column is rendered; other token fields aren't read.
    data: [
      { group: 'market:pool:mine' },
      { group: 'market:auto' },
      { group: 'market:one' },
      { group: 'vip' },
    ] as ApiKey[],
    columns: useApiKeysColumns(),
    getCoreRowModel: getCoreRowModel(),
  })
  return (
    <>
      {table.getRowModel().rows.map((row) => {
        const cell = row
          .getAllCells()
          .find((item) => item.column.id === 'group')!
        return (
          <div key={row.id}>
            {flexRender(cell.column.columnDef.cell, cell.getContext())}
          </div>
        )
      })}
    </>
  )
}

test('API key list resolves pool names from the same cached options as the editor', async () => {
  const i18n = createInstance()
  await i18n.init({ lng: 'en', resources: { en: { translation: {} } } })
  const client = new QueryClient()
  client.setQueryData(['user-groups', undefined], {
    success: true,
    data: { vip: { ratio: 1, desc: 'VIP' } },
  })
  client.setQueryData(
    ['api-key-group-options', undefined],
    [
      {
        value: 'market:pool:mine',
        label: 'GPT 工作池',
        category: 'marketplace_pool',
        models: ['gpt-5.6'],
        member_count: 1,
      },
      {
        value: 'market:auto',
        label: 'AUTO 路由池',
        category: 'marketplace_auto',
        models: [],
      },
      {
        value: 'market:one',
        label: '渠道一',
        category: 'marketplace',
        multiplier: 2,
        models: ['gpt-5.6'],
      },
    ]
  )
  try {
    const html = renderToStaticMarkup(
      <I18nextProvider i18n={i18n}>
        <QueryClientProvider client={client}>
          <GroupCells />
        </QueryClientProvider>
      </I18nextProvider>
    )
    assert.ok(html.includes('GPT 工作池'))
    assert.ok(html.includes('AUTO 路由池'))
    assert.ok(html.includes('渠道一'))
    assert.ok(html.includes('vip'))
    assert.ok(!html.includes('市场分组'))
  } finally {
    client.clear()
  }
})
