import { readdir, readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { promisify } from 'node:util'
import { brotliCompress, gzip, constants } from 'node:zlib'

const br = promisify(brotliCompress)
const gz = promisify(gzip)
async function compress(dir) {
  for (const item of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, item.name)
    if (item.isDirectory()) await compress(path)
    else if (/\.(?:js|css|html|svg|json)$/.test(item.name)) {
      const content = await readFile(path)
      await writeFile(
        `${path}.br`,
        await br(content, { params: { [constants.BROTLI_PARAM_QUALITY]: 11 } }),
      )
      await writeFile(`${path}.gz`, await gz(content, { level: 9 }))
    }
  }
}
await compress('dist')
console.log('Brotli and gzip static assets generated')
