/* Copyright (C) 2023-2026 QuantumNous. AGPL-3.0-or-later. */
// Isolated synthetic contract checks; production backend capacity is measured separately.
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const output = path.resolve(
  root,
  '../../output/playwright/concurrency-docs/frontend'
)
await fs.mkdir(output, { recursive: true })
const configPath = path.join(output, 'contract-config.json')
await fs.writeFile(
  configPath,
  JSON.stringify({ scale: 50000, mode: 'normal', delayMs: 0, target: '*' })
)
const child = spawn(
  process.execPath,
  [path.join(root, 'scripts/page-stress-fixture.mjs')],
  {
    cwd: root,
    env: {
      ...process.env,
      STRESS_PORT: '3198',
      STRESS_OUTPUT: output,
      STRESS_CONFIG_PATH: configPath,
    },
    stdio: 'pipe',
  }
)
const base = 'http://127.0.0.1:3198'
const results = []
async function read(endpoint, options) {
  const started = performance.now()
  const response = await fetch(base + endpoint, options)
  const raw = await response.text()
  return {
    status: response.status,
    data: JSON.parse(raw),
    bytes: Buffer.byteLength(raw),
    elapsedMs: Number((performance.now() - started).toFixed(2)),
  }
}
try {
  let ready = false
  for (let attempt = 0; attempt < 40; attempt += 1) {
    try {
      ready = (await fetch(base + '/__stress/health')).ok
    } catch {
      /* bounded local startup */
    }
    if (ready) break
    await new Promise((resolve) => setTimeout(resolve, 50))
  }
  assert(ready, 'Synthetic contract fixture did not start')
  for (const [page, keyword, expectedRows, expectedTotal] of [
    [1, '', 50, 50000],
    [500, '', 50, 50000],
    [1000, '', 50, 50000],
    [1001, '', 0, 50000],
    [1, 'STRESS-ELIGIBLE-049999', 1, 1],
    [1, 'no-such-invoice-unique', 0, 0],
  ]) {
    const result = await read(
      `/api/invoices/eligible-orders?p=${page}&page_size=50&keyword=${encodeURIComponent(keyword)}`
    )
    assert.equal(result.data.success, true)
    assert.equal(result.data.data.total, expectedTotal)
    assert.equal(result.data.data.items.length, expectedRows)
    assert.equal(result.data.data.page_size, 50)
    if (page === 1000)
      assert.equal(
        result.data.data.items.at(-1).trade_no,
        'STRESS-ELIGIBLE-049999'
      )
    results.push({
      scenario: 'invoice server page',
      page,
      keyword,
      rows: expectedRows,
      total: expectedTotal,
      bytes: result.bytes,
      elapsedMs: result.elapsedMs,
      pass: true,
    })
  }
  const indexes = await read('/api/marketplace/channels/mine?view=index')
  assert.equal(indexes.data.data.length, 50000)
  assert(!('model_prices' in indexes.data.data[0]))
  assert(!('model_verification_results' in indexes.data.data[0]))
  results.push({
    scenario: 'owner compact index',
    rows: 50000,
    bytes: indexes.bytes,
    elapsedMs: indexes.elapsedMs,
    fields: Object.keys(indexes.data.data[0]),
    pass: true,
  })
  const ids = Array.from(
    { length: 50 },
    (_, index) => `ids=${91001 + 49950 + index}`
  ).join('&')
  const details = await read('/api/marketplace/channels/mine?' + ids)
  assert.equal(details.data.data.length, 50)
  assert.equal(details.data.data.at(-1).id, '141000')
  assert('model_prices' in details.data.data[0])
  results.push({
    scenario: 'owner selected page details',
    rows: 50,
    bytes: details.bytes,
    elapsedMs: details.elapsedMs,
    pass: true,
  })
  const excess = await read(
    '/api/marketplace/channels/mine?' + ids + '&ids=91001'
  )
  assert.equal(excess.status, 200)
  assert.equal(excess.data.success, false)
  results.push({
    scenario: 'owner 51 ids rejected using API error envelope',
    pass: true,
  })
  const concurrent = await Promise.all(
    [1, 300, 700, 1000].map((page) =>
      read(`/api/invoices/eligible-orders?p=${page}&page_size=50&keyword=`)
    )
  )
  assert(
    concurrent.every(
      (result) =>
        result.data.data.items.length === 50 && result.data.data.total === 50000
    )
  )
  results.push({
    scenario: 'four concurrent independent invoice pages',
    rowsEach: 50,
    errors: 0,
    pass: true,
    scope:
      'Synthetic fixture isolation and contract only; no backend throughput claim',
  })
  const blocked = await read('/api/invoices/requests', {
    method: 'POST',
    body: '{}',
  })
  assert.equal(blocked.status, 405)
  results.push({ scenario: 'invoice mutation blocked', pass: true })
  await fs.writeFile(
    path.join(output, 'fixture-contract-results.json'),
    JSON.stringify(
      {
        synthetic: true,
        pass: true,
        scope: 'Fixture contract verification; real service tests are separate',
        results,
      },
      null,
      2
    )
  )
  process.stdout.write(
    JSON.stringify({ pass: true, cases: results.length, output }) + '\n'
  )
} finally {
  child.kill()
}
