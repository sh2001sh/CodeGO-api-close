import { readFile } from 'node:fs/promises'
import { join } from 'node:path'
import { gzipSync } from 'node:zlib'

const html = await readFile('dist/index.html', 'utf8')
const scripts = [...html.matchAll(/<script[^>]+src="([^"]+\.js)"/g)].map((match) => match[1])
if (!scripts.length) throw new Error('Build has no entry scripts')
let total = 0
for (const script of scripts)
  total += gzipSync(await readFile(join('dist', script.replace(/^\//, '')))).length
const limit = 300 * 1024
console.log(`Entry JavaScript gzip: ${(total / 1024).toFixed(2)} KB / 300 KB`)
if (total > limit) throw new Error('Entry JavaScript exceeds the 300 KB budget')
