/*
Copyright (C) 2023-2026 QuantumNous
SPDX-License-Identifier: AGPL-3.0-or-later
*/
// Verifies fixture pagination/edge semantics on a separate server from browser QA.
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const output = path.resolve(root, '../../output/playwright/page-stress')
await fs.mkdir(output, { recursive: true })
const configPath = path.join(output, 'contract-config.json')
const configure = (input) =>
  fs.writeFile(
    configPath,
    JSON.stringify({
      scale: 1000,
      mode: 'normal',
      delayMs: 0,
      target: '*',
      ...input,
    })
  )
await configure({})
const child = spawn(
  process.execPath,
  [path.join(root, 'scripts/page-stress-fixture.mjs')],
  {
    cwd: root,
    env: {
      ...process.env,
      STRESS_PORT: '3098',
      STRESS_CONFIG_PATH: configPath,
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  }
)
let childLogs = ''
child.stdout.on('data', (chunk) => {
  childLogs += chunk
})
child.stderr.on('data', (chunk) => {
  childLogs += chunk
})
const base = 'http://127.0.0.1:3098'
const results = []
async function read(endpoint, options) {
  const start = performance.now()
  const response = await fetch(base + endpoint, options)
  const raw = await response.text()
  const totalMs = performance.now() - start
  const parseStart = performance.now()
  const body = JSON.parse(raw)
  return {
    status: response.status,
    body,
    bytes: Buffer.byteLength(raw),
    totalMs: Number(totalMs.toFixed(2)),
    parseMs: Number((performance.now() - parseStart).toFixed(2)),
  }
}
try {
  let ready = false
  for (let attempt = 0; attempt < 40; attempt += 1) {
    try {
      ready = (await fetch(base + '/__stress/health')).ok
    } catch {
      /* bounded startup retry */
    }
    if (ready) break
    await new Promise((resolve) => setTimeout(resolve, 50))
  }
  assert(ready, childLogs || 'Fixture did not start')
  const endpoints = [
    '/api/token/',
    '/api/channel',
    '/api/user/',
    '/api/models/',
    '/api/redemption/',
    '/api/log/self',
    '/api/task/self',
    '/api/marketplace/groups',
    '/api/marketplace/shops',
    '/api/invoices/requests',
    '/api/blind-box/history',
    '/api/wallet/transfers',
  ]
  for (const scale of [0, 1000, 10000, 50000]) {
    await configure({ scale, mode: scale === 0 ? 'empty' : 'normal' })
    for (const endpoint of endpoints) {
      const samples = []
      for (const page of [
        ...new Set([
          1,
          Math.max(1, Math.ceil(scale / 40)),
          Math.max(1, Math.ceil(scale / 20)),
          Math.ceil(scale / 20) + 1,
        ]),
      ]) {
        const result = await read(
          `${endpoint}?p=${page}&page=${page}&page_size=20`
        )
        assert.equal(result.status, 200)
        const data =
          endpoint === '/api/wallet/transfers'
            ? result.body.data.history
            : result.body.data
        const items = data.items || data.records
        const expected = Math.max(0, Math.min(20, scale - (page - 1) * 20))
        assert.equal(data.total, scale, `${endpoint} total`)
        assert.equal(items.length, expected, `${endpoint} page ${page}`)
        assert.equal(data.page, page)
        assert.equal(data.page_size, 20)
        samples.push({
          page,
          rows: items.length,
          totalMs: result.totalMs,
          bytes: result.bytes,
        })
      }
      results.push({
        endpoint,
        scale,
        pagination: 'verified first/middle/final/out-of-range',
        samples,
      })
    }
    const catalogue = await read('/api/pricing')
    assert.equal(catalogue.body.data.length, scale)
    assert.equal(catalogue.body.available_models.length, scale)
    results.push({
      endpoint: '/api/pricing',
      scale,
      rows: catalogue.body.data.length,
      bytes: catalogue.bytes,
      totalMs: catalogue.totalMs,
      parseMs: catalogue.parseMs,
    })
  }
  await configure({ scale: 50000 })
  const beforeConcurrent = performance.now()
  const concurrent = await Promise.all(
    Array.from({ length: 12 }, (_, index) =>
      read(`/api/token/?p=${index + 1}&size=20`)
    )
  )
  assert(
    concurrent.every(
      (result) =>
        result.body.data.items.length === 20 && result.body.data.total === 50000
    )
  )
  results.push({
    scenario: '12 concurrent independent paged reads, fixture transport only',
    totalMs: Number((performance.now() - beforeConcurrent).toFixed(2)),
    maxMs: Math.max(...concurrent.map((result) => result.totalMs)),
    errors: 0,
  })
  await configure({ mode: 'failure', target: '/api/pricing' })
  assert.equal((await read('/api/pricing')).status, 503)
  assert.equal((await read('/api/token/?size=20')).status, 200)
  results.push({
    scenario: 'targeted 503 preserves unrelated endpoint',
    passed: true,
  })
  await configure({ delayMs: 750, target: '/api/pricing' })
  const delayed = await read('/api/pricing')
  assert(delayed.totalMs >= 700)
  results.push({
    scenario: '750ms targeted delay',
    totalMs: delayed.totalMs,
    passed: true,
  })
  for (const endpoint of [
    '/api/blind-box/inventory/open',
    '/api/wallet/transfers',
    '/api/subscription/epay/pay',
    '/pg/images/generations',
  ]) {
    assert.equal(
      (await read(endpoint, { method: 'POST', body: '{}' })).status,
      405
    )
  }
  results.push({
    scenario: 'open / transfer / purchase / inference mutations blocked',
    passed: true,
  })
  const health = await read('/__stress/health')
  await fs.writeFile(
    path.join(output, 'fixture-contract-results.json'),
    JSON.stringify(
      {
        synthetic: true,
        scope:
          'Fixture contract and transport verification, not production backend load capacity or browser rendering',
        passed: true,
        fixtureMemory: health.body.fixtureMemory,
        results,
      },
      null,
      2
    )
  )
  console.log(
    JSON.stringify({
      passed: true,
      paginationEndpointScaleCases: endpoints.length * 4,
      scales: [0, 1000, 10000, 50000],
      catalogue: results.filter((result) => result.endpoint === '/api/pricing'),
      concurrent: results.find((result) =>
        result.scenario?.startsWith('12 concurrent')
      ),
      output,
    })
  )
} finally {
  child.kill()
}
