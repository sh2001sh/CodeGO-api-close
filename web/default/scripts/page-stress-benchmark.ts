/*
Copyright (C) 2023-2026 QuantumNous
SPDX-License-Identifier: AGPL-3.0-or-later
*/
// Bun runs the production TypeScript functions; no surrogate implementation.
// This measures data processing and SSR, not browser frame time or backend capacity.
import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import {
  processChartData,
  processUserChartData,
} from '../src/features/dashboard/lib/charts'
import {
  aggregateUsageByBucket,
  buildOverviewUsageTimeline,
} from '../src/features/dashboard/lib/overview-usage'
import type { QuotaDataItem } from '../src/features/dashboard/types'
import {
  FILTER_ALL,
  QUOTA_TYPES,
  ENDPOINT_TYPES,
  SORT_OPTIONS,
} from '../src/features/pricing/constants'
import {
  extractAllTags,
  filterAndSortModels,
} from '../src/features/pricing/lib/filters'
import { availablePricingModels } from '../src/features/pricing/lib/merge-pricing-models'
import type { PricingModel } from '../src/features/pricing/types'

const repo = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  '../../..'
)
const output = path.join(repo, 'output/playwright/page-stress')
await fs.mkdir(output, { recursive: true })
const rounds = 7
const rows: Array<Record<string, unknown>> = []
function measure(
  name: string,
  scale: number,
  run: () => unknown,
  describe: (value: unknown) => unknown
) {
  run()
  const times: number[] = []
  const memoryBefore = process.memoryUsage().heapUsed
  let value: unknown
  for (let round = 0; round < rounds; round += 1) {
    const start = performance.now()
    value = run()
    times.push(performance.now() - start)
  }
  const sorted = [...times].sort((left, right) => left - right)
  const row = {
    name,
    scale,
    rounds,
    medianMs: Number(sorted[Math.floor(sorted.length / 2)].toFixed(3)),
    maxMs: Number(sorted.at(-1)?.toFixed(3)),
    heapDeltaMiB: Number(
      ((process.memoryUsage().heapUsed - memoryBefore) / 1024 / 1024).toFixed(3)
    ),
    result: describe(value),
  }
  rows.push(row)
  process.stdout.write(JSON.stringify(row) + '\n')
}
const now = Math.floor(Date.now() / 1000)
for (const scale of [0, 1000, 10000, 50000]) {
  const models = Array.from(
    { length: scale },
    (_, index): PricingModel => ({
      id: index + 1,
      model_name: `stress-${String(index).padStart(6, '0')}-reasoning`,
      vendor_id: (index % 4) + 1,
      vendor_name: ['OpenAI', 'Anthropic', 'Google', 'DeepSeek'][index % 4],
      quota_type: 0,
      model_ratio: (index % 100) / 20,
      completion_ratio: 4,
      enable_groups: ['default'],
      supported_endpoint_types: ['openai'],
      tags: 'text,reasoning,vision',
      description:
        index % 29 === 0
          ? '超长目录标题 '.repeat(40)
          : 'Synthetic catalogue model',
    })
  )
  const filters = {
    search: 'stress-',
    vendor: FILTER_ALL,
    group: FILTER_ALL,
    quotaType: QUOTA_TYPES.ALL,
    endpointType: ENDPOINT_TYPES.ALL,
    tag: FILTER_ALL,
    sortBy: SORT_OPTIONS.NAME,
  }
  measure(
    'catalog-availability-merge',
    scale,
    () =>
      availablePricingModels(
        models,
        models,
        models.map((model) => model.model_name)
      ),
    (value) => ({ models: (value as unknown[]).length })
  )
  measure(
    'catalog-search-sort',
    scale,
    () => filterAndSortModels(models, filters),
    (value) => ({ matches: (value as unknown[]).length })
  )
  measure(
    'catalog-search-no-match',
    scale,
    () => filterAndSortModels(models, { ...filters, search: 'does-not-exist' }),
    (value) => ({ matches: (value as unknown[]).length })
  )
  measure(
    'catalog-tags',
    scale,
    () => extractAllTags(models),
    (value) => ({ uniqueTags: (value as unknown[]).length })
  )
  const usage = Array.from(
    { length: scale },
    (_, index): QuotaDataItem => ({
      created_at: now - (index % (86400 * 30)),
      quota: index + 1,
      model_name: `stress-model-${index % 100}`,
      username: `stress-user-${index % 100}`,
      count: (index % 100) + 1,
      token_used: index + 1200,
    })
  )
  measure(
    'overview-usage-aggregation',
    scale,
    () =>
      aggregateUsageByBucket(
        usage as Array<{ created_at: number; quota: number }>,
        3600,
        24
      ),
    (value) => ({ buckets: (value as unknown[]).length })
  )
  measure(
    'overview-24h-timeline',
    scale,
    () =>
      buildOverviewUsageTimeline(
        usage as Array<{ created_at: number; quota: number }>,
        now - 86400,
        now,
        3600
      ),
    (value) => ({ buckets: (value as unknown[]).length })
  )
  measure(
    'model-chart-processing',
    scale,
    () => processChartData(usage, 'hour'),
    (value) => ({ specs: Object.keys(value as object).length })
  )
  measure(
    'user-chart-processing',
    scale,
    () => processUserChartData(usage, 'hour'),
    (value) => ({ specs: Object.keys(value as object).length })
  )
}
await fs.writeFile(
  path.join(output, 'processing-benchmark.json'),
  JSON.stringify(
    {
      synthetic: true,
      runtime: Bun.version,
      scope:
        'Production pure functions on synthetic data; excludes browser paint and network',
      rounds,
      rows,
    },
    null,
    2
  )
)
