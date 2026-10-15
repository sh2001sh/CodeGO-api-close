import { createInstance } from 'i18next'
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import { GroupBadge } from './group-badge'

const i18n = createInstance()
await i18n.init({
  lng: 'en',
  resources: { en: { translation: {} } },
  initImmediate: false,
})

function renderBadge(group: string, label?: string) {
  return renderToStaticMarkup(
    <I18nextProvider i18n={i18n}>
      <GroupBadge group={group} label={label} />
    </I18nextProvider>
  )
}

describe('API key group badges', () => {
  test('shows the configured routing pool name', () => {
    const html = renderBadge('market:pool:mine', 'GPT 工作池')
    assert.ok(html.includes('GPT 工作池'))
    assert.ok(!html.includes('市场分组'))
  })
  test('distinguishes unknown pools and default Auto from market groups', () => {
    assert.ok(renderBadge('market:pool:deleted').includes('路由池 (deleted)'))
    assert.ok(renderBadge('market:auto').includes('AUTO 路由池'))
    assert.ok(renderBadge('market:group', '分组 A').includes('分组 A'))
    assert.ok(renderBadge('market:unknown').includes('市场分组'))
    assert.ok(renderBadge('vip').includes('vip'))
  })
})
