import { createInstance } from 'i18next'
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import type { LogOtherData } from '../types'
import { BillingQuotaSourceBadge } from './billing-quota-source'

const i18n = createInstance()
await i18n.init({
  lng: 'en',
  resources: { en: { translation: {} } },
  initImmediate: false,
})

function renderSource(other: unknown): string {
  return renderToStaticMarkup(
    <I18nextProvider i18n={i18n}>
      <BillingQuotaSourceBadge other={other as LogOtherData | null} />
    </I18nextProvider>
  )
}

describe('usage log quota sources', () => {
  test('renders permanent package consumption without crashing the log page', () => {
    const html = renderSource({
      billing_source: 'subscription',
      billing_quota_category: 'credit_pack',
    })
    assert.ok(html.includes('Permanent plan quota'))
    assert.ok(html.includes('<svg'))
    assert.ok(!html.includes('GPT plan quota'))
  })

  test('keeps historical wallet and subscription source labels distinct', () => {
    for (const [category, label] of [
      ['universal', 'Universal quota'],
      ['gpt', 'Official GPT quota'],
      ['subscription', 'GPT plan quota'],
    ]) {
      assert.ok(
        renderSource({ billing_quota_category: category }).includes(label)
      )
    }
    assert.ok(
      renderSource({ billing_source: 'claude_wallet' }).includes(
        'Universal quota'
      )
    )
    assert.ok(
      renderSource({ billing_source: 'wallet' }).includes('Official GPT quota')
    )
    assert.ok(
      renderSource({ billing_source: 'subscription' }).includes(
        'GPT plan quota'
      )
    )
    assert.ok(
      renderSource({
        billing_source: 'subscription',
        subscription_billing_version: 'credit_pack',
      }).includes('Permanent plan quota')
    )
  })

  test('renders an unknown source for future or malformed persisted categories', () => {
    for (const category of [
      'future_source',
      '__proto__',
      17,
      { source: 'credit_pack' },
    ]) {
      assert.ok(
        renderSource({ billing_quota_category: category }).includes('Unknown')
      )
    }
  })

  test('does not invent a source when metadata is absent', () => {
    for (const other of [null, undefined, {}, { billing_source: '' }]) {
      assert.equal(renderSource(other), '')
    }
  })
})
