import { describe, expect, it } from 'vitest'
import { docArticles, legacyDocLinks, resolveDocArticle, searchDocArticles } from './content'
import { docSample } from './samples'
import { docsMessages } from './messages'
import { languages } from '../../lib/locales'
import { traditionalText } from './traditional'

describe('documentation discovery', () => {
  it('provides complete Hong Kong Traditional Chinese text and searchable titles', () => {
    const check = (value: unknown) => {
      if (!value || typeof value !== 'object') return
      const entry = value as Record<string, unknown>
      if (typeof entry['zh-CN'] === 'string') {
        expect(traditionalText[entry['zh-CN']]).toBeDefined()
        expect(entry['zh-HK']).toBe(traditionalText[entry['zh-CN']])
      }
      for (const child of Object.values(entry)) check(child)
    }
    for (const article of docArticles) check(article)
    expect(searchDocArticles('密鑰', 'zh-HK').map((article) => article.slug)).toContain(
      'authentication',
    )
    expect(resolveDocArticle('quickstart')?.title['zh-HK']).toBe('完成第一次調用')
  })
  it('preserves every legacy chapter and treats invalid explicit slugs as missing', () => {
    for (const [hash, slug] of Object.entries(legacyDocLinks)) {
      expect(resolveDocArticle(undefined, `#${hash}`)?.slug).toBe(slug)
    }
    expect(resolveDocArticle('billing', '#auth')?.slug).toBe('billing')
    expect(resolveDocArticle('unknown-article', '#auth')).toBeUndefined()
    expect(resolveDocArticle()?.slug).toBe('quickstart')
  })
  it('finds Chinese content and English terms, and returns an empty result for unmatched queries', () => {
    expect(searchDocArticles('站内钱包', 'zh-CN').map((article) => article.slug)).toContain(
      'supplier-revenue',
    )
    expect(searchDocArticles('route pools', 'en').map((article) => article.slug)).toContain(
      'route-pools',
    )
    expect(searchDocArticles('  429  ', 'en').map((article) => article.slug)).toContain('errors')
    expect(searchDocArticles('does-not-exist-anywhere', 'en')).toEqual([])
    expect(searchDocArticles('', 'en')).toHaveLength(docArticles.length)
  })
  it('has unique article and section targets and valid internal article links', () => {
    const slugs = docArticles.map((article) => article.slug)
    expect(new Set(slugs).size).toBe(slugs.length)
    expect(new Set(docArticles.map((article) => article.group)).size).toBe(7)
    for (const article of docArticles) {
      expect(new Set(article.sections.map((section) => section.id)).size).toBe(
        article.sections.length,
      )
      for (const section of article.sections)
        for (const block of section.blocks) {
          if (block.kind === 'links')
            for (const link of block.items) {
              if (link.href.startsWith('/docs?'))
                expect(
                  resolveDocArticle(
                    new URLSearchParams(link.href.split('?')[1]).get('article') ?? undefined,
                  ),
                ).toBeDefined()
            }
        }
    }
  })
  it('ships nonempty interface labels and explicit fallback notices for all nine languages', () => {
    const expected = Object.keys(docsMessages('en')).sort()
    for (const locale of languages) {
      const dictionary = docsMessages(locale.code)
      expect(Object.keys(dictionary).sort()).toEqual(expected)
      expect(Object.values(dictionary).every((value) => value.trim().length > 0)).toBe(true)
    }
  })
})

describe('copyable API examples', () => {
  it('uses an available model instead of an obsolete fixed ID and never inserts a key', () => {
    for (const language of ['curl', 'python', 'node'] as const) {
      const sample = docSample('quickstart', language, 'https://codego.example', 'current-model')
      expect(sample).toContain('current-model')
      expect(sample).toContain('CODEGO_API_KEY')
      expect(sample).not.toContain('gpt-4o-mini')
      expect(sample).not.toContain('\n+')
    }
    expect(docSample('quickstart', 'curl', 'https://codego.example', '')).toContain('YOUR_MODEL_ID')
  })
  it('produces valid JSON for model names with quotes or line breaks without interpolating a shell command', () => {
    const model = 'model\"\n$(echo secret)'
    const sample = docSample('quickstart', 'curl', 'https://codego.example', model)
    const json = sample.split("<<'CODEGO_JSON'\n")[1].split('\nCODEGO_JSON')[0]
    expect(JSON.parse(json)).toEqual({
      model,
      messages: [{ role: 'user', content: 'Hello' }],
      stream: true,
    })
    expect(docSample('responses', 'curl', 'https://codego.example', 'responses-model')).toContain(
      '/v1/responses',
    )
    expect(docSample('embeddings', 'curl', 'https://codego.example', 'embedding-model')).toContain(
      '/v1/embeddings',
    )
    expect(docSample('models', 'curl', 'https://codego.example', '')).not.toContain('YOUR_MODEL_ID')
  })
})
