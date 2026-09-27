import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { Converter } from 'opencc-js'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const source = path.join(root, 'src/i18n/locales/zh.json')
const target = path.join(root, 'src/i18n/locales/zh-TW.json')
const convert = Converter({ from: 'cn', to: 'twp' })

function convertValues(value) {
  if (typeof value === 'string') {
    const translated = convert(value)
    return value.includes('万象') ? translated.replaceAll('永珍', '萬象') : translated
  }
  if (Array.isArray(value)) return value.map(convertValues)
  if (value && typeof value === 'object') {
    return Object.fromEntries(
      Object.entries(value).map(([key, entry]) => [key, convertValues(entry)])
    )
  }
  return value
}

const simplified = JSON.parse(await fs.readFile(source, 'utf8'))
await fs.writeFile(
  target,
  `${JSON.stringify(convertValues(simplified), null, 2)}\n`,
  'utf8'
)
