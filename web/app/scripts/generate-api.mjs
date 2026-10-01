import { writeFile } from 'node:fs/promises'
import { generateTypes } from './api-types.mjs'

await writeFile(new URL('../src/lib/api.generated.ts', import.meta.url), await generateTypes())
console.log('Generated OpenAPI client types with lossless int64 support')
