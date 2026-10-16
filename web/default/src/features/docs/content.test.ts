import assert from 'node:assert/strict'
import { test } from 'node:test'
import { COMMUNITY_LINKS } from '../../lib/company'
import {
  docArticles,
  docArticleNeedsModelCatalog,
  resolveDocArticle,
  searchDocArticles,
  type Text,
} from './content'
import { docsMessages } from './messages'
import { docSample, type DocSample } from './samples'

const languages = ['zh-CN', 'zh-HK', 'en', 'fr', 'ru', 'ja', 'vi'] as const

test('every article and visible block has complete translations and unique anchors', () => {
  const check = (text: Text) => {
    for (const language of languages)
      assert.ok(text[language]?.trim(), language)
  }
  assert.equal(new Set(docArticles.map((a) => a.slug)).size, docArticles.length)
  for (const article of docArticles) {
    check(article.title)
    check(article.summary)
    assert.equal(
      new Set(article.sections.map((s) => s.id)).size,
      article.sections.length
    )
    for (const section of article.sections) {
      check(section.title)
      for (const block of section.blocks) {
        if (block.kind === 'paragraph' || block.kind === 'note')
          check(block.text)
        if (block.kind === 'list') block.items.forEach(check)
        if (block.kind === 'table') {
          block.headers.forEach(check)
          block.rows.flat().forEach(check)
        }
        if (block.kind === 'links') {
          for (const link of block.items) {
            check(link.label)
            if (link.href.startsWith('/docs?article='))
              assert.ok(
                resolveDocArticle(
                  new URL(link.href, 'https://example.test').searchParams.get(
                    'article'
                  )!
                )
              )
          }
        }
      }
    }
  }
})

test('only articles with model request examples need the price catalog', () => {
  for (const slug of ['concurrency', 'authentication', 'errors', 'production'])
    assert.equal(
      docArticleNeedsModelCatalog(resolveDocArticle(slug)),
      false,
      slug
    )
  for (const slug of ['quickstart', 'responses-streaming', 'api-reference'])
    assert.equal(
      docArticleNeedsModelCatalog(resolveDocArticle(slug)),
      true,
      slug
    )
  assert.equal(docArticleNeedsModelCatalog(undefined), false)
  const source = resolveDocArticle('quickstart')!
  assert.equal(
    docArticleNeedsModelCatalog({
      ...source,
      sections: [
        {
          id: 'setup',
          title: source.title,
          blocks: [
            { kind: 'code', sample: 'models' },
            { kind: 'code', sample: 'environment' },
          ],
        },
      ],
    }),
    false
  )
})

test('search finds new articles and handles missing documents and empty search', () => {
  for (const slug of [
    'sdk-streaming',
    'tools-structured',
    'web-chat',
    'model-metrics',
    'support-contact',
    'concurrency',
    'responses-streaming',
  ])
    assert.equal(resolveDocArticle(slug)?.slug, slug)
  assert.ok(
    searchDocArticles('UltraFast', 'zh-HK').some((a) => a.slug === 'billing')
  )
  assert.ok(
    searchDocArticles('Discord', 'fr').some((a) => a.slug === 'support-contact')
  )
  assert.equal(searchDocArticles('   ', 'ja').length, docArticles.length)
  assert.equal(searchDocArticles('no-such-document-3b919', 'en').length, 0)
  assert.equal(resolveDocArticle('no-such-document'), undefined)
  assert.equal(resolveDocArticle(undefined, '#auth')?.slug, 'authentication')
})

test('public documentation and search contain no competitor references in any language', () => {
  assert.equal(resolveDocArticle('openrouter-migration'), undefined)
  for (const language of languages) {
    assert.equal(searchDocArticles('OpenRouter', language).length, 0)
    assert.doesNotMatch(JSON.stringify(docsMessages(language)), /openrouter/i)
    for (const article of searchDocArticles('', language)) {
      // Every translated passage, URL and navigation slug is part of the public content.
      assert.doesNotMatch(JSON.stringify(article), /openrouter/i)
      for (const section of article.sections) {
        for (const block of section.blocks) {
          if (block.kind !== 'code') continue
          for (const codeLanguage of ['curl', 'python', 'node'] as const) {
            assert.doesNotMatch(
              docSample(
                block.sample,
                codeLanguage,
                'https://example.test',
                'test-model'
              ),
              /openrouter/i
            )
          }
        }
      }
    }
  }
})

function payload(sample: DocSample, model = 'test-model') {
  const code = docSample(sample, 'curl', 'https://example.test/', model)
  assert.ok(!code.includes('/v1/v1'))
  return JSON.parse(
    code.split("<<'CODEGO_JSON'\n")[1].split('\nCODEGO_JSON')[0]
  )
}

test('copyable billing examples send the billed priority and ultrafast protocol values', () => {
  assert.equal(payload('responses-fast').service_tier, 'priority')
  assert.equal(payload('responses-ultrafast').service_tier, 'ultrafast')
  assert.equal(payload('quickstart').service_tier, undefined)
  assert.equal(payload('quickstart').stream, undefined)
  assert.equal(payload('streaming').stream_options.include_usage, true)
})

test('JSON request examples preserve special model IDs and have a useful empty fallback', () => {
  const model = '模型/"test"\nCODEGO_JSON\n$(echo unsafe)\\path'
  assert.equal(payload('tools', model).model, model)
  assert.equal(payload('responses', '   ').model, 'YOUR_MODEL_ID')
  assert.equal(payload('embeddings').input, 'Hello')
  const structured = payload('structured').response_format.json_schema
  assert.equal(structured.strict, true)
  assert.equal(structured.schema.additionalProperties, false)
  const messages = payload('tool-result').messages
  assert.equal(messages[1].tool_calls[0].id, messages[2].tool_call_id)
})

test('all SDK samples use the correct API resource and never fall back to curl', () => {
  for (const language of ['python', 'node'] as const) {
    for (const [sample, method] of [
      ['models', 'models.list'],
      ['quickstart', 'chat.completions.create'],
      ['streaming', 'chat.completions.create'],
      ['tools', 'chat.completions.create'],
      ['structured', 'chat.completions.create'],
      ['vision', 'chat.completions.create'],
      ['responses', 'responses.create'],
      ['responses-ultrafast', 'responses.create'],
      ['responses-streaming', 'responses.create'],
      ['embeddings', 'embeddings.create'],
    ] as const) {
      const code = docSample(
        sample,
        language,
        'https://example.test/',
        'test-model'
      )
      assert.ok(code.includes(`client.${method}`), `${language} ${sample}`)
      assert.ok(code.includes('CODEGO_API_KEY'))
      assert.ok(!code.includes('curl '))
      assert.ok(code.includes('https://example.test/v1'))
    }
  }
})

test('Responses streams request the protocol and detect incomplete or failed termination', () => {
  assert.equal(payload('responses-streaming').stream, true)
  const curl = docSample(
    'responses-streaming',
    'curl',
    'https://example.test',
    'test-model'
  )
  assert.ok(curl.startsWith('curl -N '))
  for (const language of ['python', 'node'] as const) {
    const code = docSample(
      'responses-streaming',
      language,
      'https://example.test',
      'test-model'
    )
    for (const event of [
      'response.output_text.delta',
      'response.completed',
      'response.failed',
      'response.incomplete',
    ])
      assert.ok(code.includes(event), `${language} ${event}`)
    assert.ok(code.includes('Stream ended without response.completed'))
  }
})

test('setup examples are local commands and concurrency guidance covers failure boundaries', () => {
  for (const language of ['curl', 'python', 'node'] as const) {
    const code = docSample(
      'environment',
      language,
      'https://example.test',
      'test-model'
    )
    assert.ok(code.includes('CODEGO_API_KEY'))
    assert.ok(!code.includes('/chat/completions'))
  }
  for (const language of languages) {
    assert.ok(
      searchDocArticles('Retry-After', language).some(
        (article) => article.slug === 'concurrency'
      )
    )
    const article = resolveDocArticle('concurrency')!
    assert.ok(article.sections.some((section) => section.id === 'queue'))
  }
})

test('community channels exactly match the supplied public URLs', () => {
  assert.ok(
    COMMUNITY_LINKS.some(
      (link) => link.href === 'https://x.com/9U58nYrVzG38231'
    )
  )
  assert.ok(
    COMMUNITY_LINKS.some((link) => link.href === 'https://discord.gg/EWEWtZ23')
  )
})
