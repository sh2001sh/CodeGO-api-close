import { readFile, writeFile } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const scriptDirectory = dirname(fileURLToPath(import.meta.url))
const projectDirectory = resolve(scriptDirectory, '..')
const sourcePath = resolve(projectDirectory, 'public/sitemap.xml')
const outputPath = resolve(projectDirectory, 'dist/sitemap.xml')
const lastModified = new Date().toISOString().slice(0, 10)

const sitemap = await readFile(sourcePath, 'utf8')
const output = sitemap.replace(
  /(\s*<loc>[^<]+<\/loc>\r?\n)(\s*<changefreq>)/g,
  `$1    <lastmod>${lastModified}</lastmod>\n$2`
)

await writeFile(outputPath, output, 'utf8')
