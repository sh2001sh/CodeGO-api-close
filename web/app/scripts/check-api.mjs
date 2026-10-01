import { readFile } from 'node:fs/promises'
import { generateTypes } from './api-types.mjs'

const expected = await generateTypes()
const actual = await readFile(new URL('../src/lib/api.generated.ts', import.meta.url), 'utf8')
if (actual.replace(/\r\n/g, '\n') !== expected.replace(/\r\n/g, '\n'))
  throw new Error('API types are stale. Run bun run generate:api.')
console.log('Generated API types match the OpenAPI schema')
