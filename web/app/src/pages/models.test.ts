import { createElement, type ReactNode } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const state = vi.hoisted(() => ({
  search: {} as { q?: string; vendor?: string },
  items: Array.from({ length: 80 }, (_, index) => ({
    name: `model-${index}`,
    vendor: index % 2 === 0 ? 'OpenAI' : 'Anthropic',
    groups: Array.from({ length: 40 }, (_, group) => ({
      slug: `group-${group}`,
      name: `Hidden group ${group}`,
      verified: false,
      multiplier: '1',
      price: null,
    })),
  })),
}))

vi.mock('@tanstack/react-query', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-query')>()),
  useQuery: () => ({ data: state.items, isError: false, isPending: false }),
}))
vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => vi.fn(),
  useSearch: () => state.search,
  Link: ({ to, children }: { to: string; children: ReactNode }) =>
    createElement('a', { href: to }, children),
}))

import ModelsPage from './models'

describe('public model catalog', () => {
  beforeEach(() => {
    state.search = {}
  })

  it('bounds the initial render to 24 rows and leaves group details unmounted', () => {
    const html = renderToStaticMarkup(createElement(ModelsPage))
    expect(html.match(/class="model-row"/g)).toHaveLength(24)
    expect(html).not.toContain('Hidden group')
    expect(html).not.toContain('<details open')
    expect(html).toContain('下一页')
    expect(html).toContain('1 / 4')
  })

  it('applies both query and vendor filters before pagination', () => {
    state.search = { q: 'model-1', vendor: 'OpenAI' }
    const html = renderToStaticMarkup(createElement(ModelsPage))
    expect(html.match(/class="model-row"/g)).toHaveLength(5)
    expect(html).toContain('model-10')
    expect(html).not.toContain('>model-11<')
    expect(html).not.toContain('下一页')
  })

  it('shows an empty result without rendering stale rows', () => {
    state.search = { q: 'unlisted-model' }
    const html = renderToStaticMarkup(createElement(ModelsPage))
    expect(html).toContain('没有匹配的模型')
    expect(html).not.toContain('class="model-row"')
  })
})
