import assert from 'node:assert/strict'
import { test } from 'node:test'
import { getTokenComposition } from './token-composition'

test('cached input is counted once in the screenshot example', () => {
  const result = getTokenComposition([
    {
      prompt_tokens: 14051000,
      completion_tokens: 51000,
      other: '{"cache_tokens":13670000}',
    },
  ])
  assert.deepEqual(result, {
    cacheHit: 13670000,
    cacheMiss: 381000,
    output: 51000,
  })
  assert.equal(
    Object.values(result).reduce((a, b) => a + b),
    14102000
  )
})

test('clamps cache per request and keeps cache creation in uncached input', () => {
  assert.deepEqual(
    getTokenComposition([
      { prompt_tokens: 10, other: '{"cache_tokens":1000}' },
      {
        prompt_tokens: 100,
        other: '{"cache_tokens":20,"cache_creation_tokens":40}',
      },
    ]),
    { cacheHit: 30, cacheMiss: 80, output: 0 }
  )
})

test('empty and malformed metadata never produce negative or NaN segments', () => {
  assert.deepEqual(getTokenComposition([]), {
    cacheHit: 0,
    cacheMiss: 0,
    output: 0,
  })
  for (const other of [
    '{',
    'null',
    '{"cache_tokens":-5}',
    '{"cache_tokens":"NaN"}',
  ]) {
    assert.deepEqual(
      getTokenComposition([
        { prompt_tokens: 10, completion_tokens: -1, other },
      ]),
      { cacheHit: 0, cacheMiss: 10, output: 0 }
    )
  }
  assert.deepEqual(
    getTokenComposition([{ prompt_tokens: NaN, completion_tokens: Infinity }]),
    { cacheHit: 0, cacheMiss: 0, output: 0 }
  )
})

test('Anthropic input excludes cache reads and writes in stored logs', () => {
  for (const marker of [{ usage_semantic: 'anthropic' }, { claude: true }]) {
    assert.deepEqual(
      getTokenComposition([
        {
          prompt_tokens: 10,
          completion_tokens: 5,
          other: JSON.stringify({
            ...marker,
            cache_tokens: 200,
            cache_creation_tokens: 60,
            cache_creation_tokens_5m: 20,
            cache_creation_tokens_1h: 40,
            cache_write_tokens: 60,
          }),
        },
      ]),
      { cacheHit: 200, cacheMiss: 70, output: 5 }
    )
  }
})

test('converted OpenAI logs honor explicit total input', () => {
  assert.deepEqual(
    getTokenComposition([
      {
        prompt_tokens: 10,
        other: '{"input_tokens_total":270,"cache_tokens":200}',
      },
    ]),
    { cacheHit: 200, cacheMiss: 70, output: 0 }
  )
})
