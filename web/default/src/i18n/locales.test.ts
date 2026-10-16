import assert from 'node:assert/strict'
import { readFileSync, readdirSync } from 'node:fs'
import path from 'node:path'
import { test } from 'node:test'
import { fileURLToPath } from 'node:url'
import ts from 'typescript'
import { api } from '@/lib/api'
import { formatCurrencyFromUSD } from '@/lib/currency'
import dayjs from '@/lib/dayjs'
import { docArticles } from '@/features/docs/content'
import { docSample } from '@/features/docs/samples'
import i18n from './config'
import { getIntlLocale } from './format'
import { normalizeInterfaceLanguage, toIntlLocale } from './languages'
import en from './locales/en.json'
import fr from './locales/fr.json'
import ja from './locales/ja.json'
import ru from './locales/ru.json'
import vi from './locales/vi.json'
import zhTW from './locales/zh-TW.json'
import zh from './locales/zh.json'
import { STATIC_I18N_KEYS } from './static-keys'

const resources = { en, zh, 'zh-TW': zhTW, fr, ru, ja, vi } as const

const locales = ['zh-TW', 'zh', 'en', 'fr', 'ru', 'ja', 'vi'] as const
const placeholders = (text: string) =>
  [...text.matchAll(/\{\{\s*([^{}]+?)\s*\}\}/g)].map((match) => match[1]).sort()

test('all seven dictionaries contain complete strings and preserve interpolation', () => {
  const english: Record<string, string> = resources.en.translation
  const keys = Object.keys(english).sort()
  for (const locale of locales) {
    const dictionary: Record<string, string> = resources[locale].translation
    assert.deepEqual(Object.keys(dictionary).sort(), keys, locale)
    for (const key of keys) {
      assert.ok(dictionary[key].trim(), `${locale}: ${key}`)
      assert.deepEqual(
        placeholders(dictionary[key]),
        placeholders(english[key]),
        `${locale}: ${key}`
      )
      if (['en', 'fr', 'ru', 'vi'].includes(locale)) {
        assert.doesNotMatch(
          dictionary[key],
          /[\u3400-\u9fff]/u,
          `${locale}: ${key}`
        )
      }
      if (locale === 'ja') {
        assert.doesNotMatch(
          dictionary[key],
          /[额总载败帐钱请订显认试购奖构]/u,
          `${locale}: ${key}`
        )
      }
    }
  }
})

test('every literal interface translation and registered dynamic label exists in every locale', () => {
  const keys = new Set<string>(STATIC_I18N_KEYS)
  function inspectFile(filename: string): void {
    const source = ts.createSourceFile(
      filename,
      readFileSync(filename, 'utf8'),
      ts.ScriptTarget.Latest,
      true
    )
    function visit(node: ts.Node): void {
      if (
        ts.isCallExpression(node) &&
        node.arguments.length &&
        (node.expression.getText(source) === 't' ||
          node.expression.getText(source).endsWith('.t'))
      ) {
        const first = node.arguments[0]
        if (ts.isStringLiteralLike(first) && first.text) keys.add(first.text)
      }
      if (
        ts.isJsxAttribute(node) &&
        node.name.getText(source) === 'i18nKey' &&
        node.initializer
      ) {
        const value = ts.isJsxExpression(node.initializer)
          ? node.initializer.expression
          : node.initializer
        if (value && ts.isStringLiteralLike(value) && value.text)
          keys.add(value.text)
      }
      ts.forEachChild(node, visit)
    }
    visit(source)
  }
  function walk(directory: string): void {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const filename = path.join(directory, entry.name)
      if (entry.isDirectory()) walk(filename)
      else if (/\.tsx?$/.test(entry.name) && !entry.name.includes('.test.'))
        inspectFile(filename)
    }
  }
  walk(fileURLToPath(new URL('..', import.meta.url)))
  for (const locale of locales) {
    const dictionary: Record<string, string> = resources[locale].translation
    const missing = [...keys].filter((key) => !dictionary[key]?.trim())
    assert.deepEqual(missing, [], locale)
  }
  assert.ok(
    keys.size > new Set(STATIC_I18N_KEYS).size,
    'AST traversal must discover literal keys beyond the registered dynamic labels'
  )
})

test('every documentation heading and body has all seven language versions', () => {
  let passages = 0
  function inspect(value: unknown): void {
    if (!value || typeof value !== 'object') return
    if ('en' in value) {
      const text = value as Record<string, string>
      passages++
      for (const locale of locales) {
        const language = toIntlLocale(locale) as string
        assert.equal(typeof text[language], 'string', `${language}: ${text.en}`)
        assert.ok(text[language].trim(), `${language}: ${text.en}`)
        assert.deepEqual(placeholders(text[language]), placeholders(text.en))
        if (['fr', 'ru', 'ja', 'vi'].includes(locale) && text.en.length > 45) {
          assert.notEqual(
            text[language],
            text.en,
            `${language} must have a translated body`
          )
        }
      }
      return
    }
    Object.values(value).forEach(inspect)
  }
  inspect(docArticles)
  assert.ok(docArticles.length >= 10)
  assert.ok(passages >= 200)
})

test('Hong Kong default and aliases resolve without unsupported locale errors', () => {
  assert.equal(normalizeInterfaceLanguage(null), 'zh-TW')
  for (const value of ['zh-HK', 'zh-Hant', 'zh_TW']) {
    assert.equal(normalizeInterfaceLanguage(value), 'zh-TW')
    assert.equal(toIntlLocale(value), 'zh-HK')
  }
  assert.equal(toIntlLocale('zh'), 'zh-CN')
  assert.equal(toIntlLocale('unsupported'), 'en')
  assert.equal(toIntlLocale('not_a_locale'), 'en')
})

test('switching locale updates numeric and relative-time display without changing credit value', async () => {
  for (const locale of locales) {
    await i18n.changeLanguage(locale)
    const expected = new Intl.NumberFormat(toIntlLocale(locale), {
      maximumFractionDigits: 2,
    }).format(1234.5)
    assert.equal(getIntlLocale(), toIntlLocale(locale))
    assert.equal(formatCurrencyFromUSD(1234.5), `${expected} credits`)
    assert.equal(
      dayjs.locale(),
      ({ 'zh-TW': 'zh-tw', zh: 'zh-cn' } as Record<string, string>)[locale] ??
        locale
    )
    assert.ok(i18n.t('invoice.ordersFailed').trim())
  }
  await i18n.changeLanguage('zh-TW')
})

test('documentation examples preserve model IDs, endpoint and environment-variable names', () => {
  for (const language of ['curl', 'python', 'node'] as const) {
    const example = docSample(
      'quickstart',
      language,
      'https://api.example.test',
      'vendor/model-v1'
    )
    assert.ok(example.includes('vendor/model-v1'))
    assert.ok(example.includes('CODEGO_API_KEY'))
    assert.ok(example.includes('https://api.example.test/v1'))
  }
  assert.ok(
    docSample('quickstart', 'curl', 'https://api.example.test', '  ').includes(
      'YOUR_MODEL_ID'
    )
  )
})

test('switching language during an in-flight request preserves each request locale', async () => {
  const originalAdapter = api.defaults.adapter
  const pending: { language: string; finish: () => void }[] = []
  let onStart = () => {}
  const started = () =>
    new Promise<void>((resolve) => {
      onStart = resolve
    })
  api.defaults.adapter = (config) =>
    new Promise((resolve) => {
      pending.push({
        language: String(config.headers.get('Accept-Language')),
        finish: () =>
          resolve({
            data: { success: true },
            status: 200,
            statusText: 'OK',
            headers: {},
            config,
          }),
      })
      onStart()
    })
  try {
    await i18n.changeLanguage('en')
    let requestStarted = started()
    const english = api.get('/i18n-request-test')
    await requestStarted
    assert.equal(
      api.get('/i18n-request-test'),
      english,
      'same-language calls still share the request'
    )
    await i18n.changeLanguage('ja')
    requestStarted = started()
    const japanese = api.get('/i18n-request-test')
    await requestStarted
    assert.deepEqual(
      pending.map((request) => request.language),
      ['en', 'ja']
    )
    pending.forEach((request) => request.finish())
    await Promise.all([english, japanese])
  } finally {
    pending.forEach((request) => request.finish())
    api.defaults.adapter = originalAdapter
    await i18n.changeLanguage('zh-TW')
  }
})
