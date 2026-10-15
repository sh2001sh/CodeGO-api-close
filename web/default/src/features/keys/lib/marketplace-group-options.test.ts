/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { toApiKeyGroupOptions } from './marketplace-group-options'

describe('API key group option metadata', () => {
  test('preserves named and empty pool names, token groups and model lists', () => {
    const options = toApiKeyGroupOptions([
      {
        value: 'market:pool:one',
        label: 'GPT 工作池',
        category: 'marketplace_pool',
        models: ['gpt-5.6'],
      },
      {
        value: 'market:pool:empty',
        label: 'Empty pool',
        category: 'marketplace_pool',
        models: [],
      },
      {
        value: 'market:auto',
        label: 'AUTO 路由池',
        category: 'marketplace_auto',
        models: ['claude'],
      },
    ])
    const labels = Object.fromEntries(
      options.map((option) => [option.value, option.label])
    )
    assert.equal(labels['market:pool:one'], 'GPT 工作池')
    assert.equal(labels['market:pool:empty'], 'Empty pool')
    assert.equal(labels['market:auto'], 'AUTO 路由池')
    assert.deepEqual(options[0].models, ['gpt-5.6'])
    assert.equal(options[0].ratio, '动态')
  })
  test('keeps personal prices and subscription metadata without fabricating statistics', () => {
    const [option] = toApiKeyGroupOptions([
      {
        value: 'market:group',
        label: 'Test',
        category: 'marketplace',
        multiplier: 0.7,
        subscription_enabled: true,
        subscription_multiplier: 1.4,
        models: ['gpt-5.6'],
      },
    ])
    assert.equal(option.ratio, 0.7)
    assert.equal(option.subscriptionRatio, 1.4)
    assert.equal(option.subscriptionEnabled, true)
    assert.equal(option.successRate, undefined)
    assert.deepEqual(toApiKeyGroupOptions([]), [])
  })
})
