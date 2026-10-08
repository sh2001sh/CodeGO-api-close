import { Fragment, type ReactNode } from 'react'

export type MarkdownBlock =
  { kind: 'code'; content: string; language: string } | { kind: 'markup'; node: ReactNode }

/** Allow URLs that cannot execute script or embed credentials. */
export function safeMarkdownURL(value: string): string | undefined {
  const url = value.trim()
  if (!url || /[\u0000-\u0020\u007f]/.test(url)) return undefined
  if (url.startsWith('#')) return url
  if (url.startsWith('/') && !url.startsWith('//') && !url.includes('\\')) return url
  try {
    const parsed = new URL(url)
    if (!['https:', 'http:', 'mailto:'].includes(parsed.protocol)) return undefined
    if (parsed.username || parsed.password) return undefined
    return url
  } catch {
    return undefined
  }
}

function closingBracket(source: string, start: number, open: string, close: string) {
  let depth = 1
  for (let index = start; index < source.length; index++) {
    if (source[index] === '\\') {
      index++
      continue
    }
    if (source[index] === open) depth++
    if (source[index] === close) {
      depth--
      if (depth === 0) return index
    }
  }
  return -1
}

export function markdownInline(source: string, depth = 0, allowLinks = true): ReactNode {
  if (depth > 12) return source
  const nodes: ReactNode[] = []
  let plain = ''
  const flush = () => {
    if (plain) {
      nodes.push(plain)
      plain = ''
    }
  }
  for (let index = 0; index < source.length;) {
    if (
      source[index] === '\\' &&
      index + 1 < source.length &&
      /[\\`*{}\[\]()#+.!_>~-]/.test(source[index + 1])
    ) {
      plain += source[index + 1]
      index += 2
      continue
    }
    if (source[index] === '`') {
      const ticks = source.slice(index).match(/^`+/)![0]
      const end = source.indexOf(ticks, index + ticks.length)
      if (end !== -1) {
        flush()
        nodes.push(
          <code key={index}>{source.slice(index + ticks.length, end).replace(/\n/g, ' ')}</code>,
        )
        index = end + ticks.length
        continue
      }
      plain += ticks
      index += ticks.length
      continue
    }
    // Images are readable links, never automatic third-party image requests.
    const image = source.startsWith('![', index)
    if (allowLinks && (image || source[index] === '[')) {
      const start = index + (image ? 2 : 1)
      const labelEnd = closingBracket(source, start, '[', ']')
      if (labelEnd === -1) {
        plain += source.slice(index)
        break
      }
      if (labelEnd !== -1 && source[labelEnd + 1] === '(') {
        const urlEnd = closingBracket(source, labelEnd + 2, '(', ')')
        if (urlEnd === -1) {
          plain += source.slice(index)
          break
        }
        if (urlEnd !== -1) {
          const label = source.slice(start, labelEnd)
          const destination = source
            .slice(labelEnd + 2, urlEnd)
            .trim()
            .replace(/\s+"[^"]*"$/, '')
            .replace(/^<(.+)>$/, '$1')
          const href = safeMarkdownURL(destination)
          flush()
          nodes.push(
            href ? (
              <a key={index} href={href} target="_blank" rel="noopener noreferrer">
                {markdownInline(label, depth + 1, false)}
              </a>
            ) : (
              <Fragment key={index}>{markdownInline(label, depth + 1, false)}</Fragment>
            ),
          )
          index = urlEnd + 1
          continue
        }
      }
      plain += source.slice(index, labelEnd + 1)
      index = labelEnd + 1
      continue
    }
    const marker = ['**', '__', '~~', '*', '_'].find((value) => source.startsWith(value, index))
    if (marker && !(marker.includes('_') && /[\p{L}\p{N}]/u.test(source[index - 1] ?? ''))) {
      const end = source.indexOf(marker, index + marker.length)
      if (end > index + marker.length) {
        flush()
        const inner = markdownInline(
          source.slice(index + marker.length, end),
          depth + 1,
          allowLinks,
        )
        nodes.push(
          marker === '~~' ? (
            <del key={index}>{inner}</del>
          ) : marker.length === 2 ? (
            <strong key={index}>{inner}</strong>
          ) : (
            <em key={index}>{inner}</em>
          ),
        )
        index = end + marker.length
        continue
      }
    }
    if (allowLinks && source[index] === '<') {
      const end = source.indexOf('>', index + 1)
      if (end === -1) {
        plain += source.slice(index)
        break
      }
      const url = end === -1 ? undefined : safeMarkdownURL(source.slice(index + 1, end))
      if (url) {
        flush()
        nodes.push(
          <a key={index} href={url} target="_blank" rel="noopener noreferrer">
            {url}
          </a>,
        )
        index = end + 1
        continue
      }
    }
    if (source[index] === '\n') {
      flush()
      nodes.push(<br key={index} />)
      index++
      continue
    }
    plain += source[index++]
  }
  flush()
  return nodes
}

function cells(line: string) {
  const source = line
    .trim()
    .replace(/^\|/, '')
    .replace(/(?<!\\)\|$/, '')
  const columns: string[] = []
  let cell = '',
    ticks = false
  for (let i = 0; i < source.length; i++) {
    if (source[i] === '\\' && source[i + 1] === '|') {
      cell += '|'
      i++
      continue
    }
    if (source[i] === '`') ticks = !ticks
    if (source[i] === '|' && !ticks) {
      columns.push(cell.trim())
      cell = ''
    } else cell += source[i]
  }
  columns.push(cell.trim())
  return columns
}

const fence = /^ {0,3}(`{3,}|~{3,})([^`]*)$/
const heading = /^ {0,3}(#{1,6})\s+(.+?)(?:\s+#+)?$/
const listItem = /^(\s*)([-+*]|\d+[.)])\s+(.*)$/
const divider = /^ {0,3}(?:\*\s*){3,}$|^ {0,3}(?:-\s*){3,}$|^ {0,3}(?:_\s*){3,}$/
const startsBlock = (line: string) =>
  fence.test(line) ||
  heading.test(line) ||
  listItem.test(line) ||
  /^ {0,3}>/.test(line) ||
  divider.test(line)

/** Common message Markdown with unfinished fences during streaming. No raw HTML. */
export function markdownBlocks(content: string, depth = 0): MarkdownBlock[] {
  if (depth > 12) return [{ kind: 'markup', node: <p>{content}</p> }]
  const lines = content.replace(/\r\n?/g, '\n').split('\n')
  const result: MarkdownBlock[] = []
  const markup = (node: ReactNode) => result.push({ kind: 'markup', node })
  for (let index = 0; index < lines.length;) {
    if (!lines[index].trim()) {
      index++
      continue
    }
    const open = lines[index].match(fence)
    if (open) {
      const code: string[] = []
      const close = new RegExp(`^ {0,3}${open[1][0]}{${open[1].length},}\\s*$`)
      index++
      while (index < lines.length && !close.test(lines[index])) code.push(lines[index++])
      if (index < lines.length) index++
      result.push({ kind: 'code', content: code.join('\n'), language: open[2].trim().slice(0, 40) })
      continue
    }
    if (/^ {4}\S/.test(lines[index])) {
      const code: string[] = []
      while (index < lines.length && (/^ {4}/.test(lines[index]) || !lines[index].trim()))
        code.push(lines[index++].slice(4))
      result.push({ kind: 'code', content: code.join('\n').trimEnd(), language: '' })
      continue
    }
    const title = lines[index].match(heading)
    if (title) {
      const Tag = `h${Math.min(6, title[1].length + 1)}` as 'h2' | 'h3' | 'h4' | 'h5' | 'h6'
      markup(<Tag>{markdownInline(title[2])}</Tag>)
      index++
      continue
    }
    if (index + 1 < lines.length && /^ {0,3}(?:=+|-+)\s*$/.test(lines[index + 1])) {
      const Tag = lines[index + 1].trim().startsWith('=') ? 'h2' : 'h3'
      markup(<Tag>{markdownInline(lines[index])}</Tag>)
      index += 2
      continue
    }
    if (divider.test(lines[index])) {
      markup(<hr />)
      index++
      continue
    }
    if (/^ {0,3}>/.test(lines[index])) {
      const quoted: string[] = []
      while (index < lines.length && /^ {0,3}>/.test(lines[index]))
        quoted.push(lines[index++].replace(/^ {0,3}> ?/, ''))
      markup(<blockquote>{renderNested(markdownBlocks(quoted.join('\n'), depth + 1))}</blockquote>)
      continue
    }
    const item = lines[index].match(listItem)
    if (item) {
      const ordered = /^\d/.test(item[2]),
        indent = item[1].length
      const items: ReactNode[] = [],
        start = ordered ? parseInt(item[2], 10) : undefined
      while (index < lines.length) {
        const current = lines[index].match(listItem)
        if (!current || current[1].length !== indent || /^\d/.test(current[2]) !== ordered) break
        const body = [current[3]],
          continuation = current[0].length - current[3].length
        index++
        while (index < lines.length) {
          const next = lines[index].match(listItem)
          if (next && next[1].length <= indent) break
          if (!lines[index].trim()) {
            if ((lines[index + 1]?.match(/^\s*/)?.[0].length ?? 0) <= indent) break
            body.push('')
            index++
            continue
          }
          const leading = lines[index].match(/^\s*/)?.[0].length ?? 0
          if (leading <= indent) break
          body.push(lines[index++].slice(Math.min(continuation, leading)))
        }
        const task = body[0].match(/^\[([ xX])\]\s+(.*)$/)
        if (task) body[0] = task[2]
        items.push(
          <li key={items.length}>
            {task && (
              <input type="checkbox" checked={task[1] !== ' '} disabled aria-label={task[2]} />
            )}
            {renderNested(markdownBlocks(body.join('\n'), depth + 1))}
          </li>,
        )
      }
      markup(ordered ? <ol start={start}>{items}</ol> : <ul>{items}</ul>)
      continue
    }
    if (index + 1 < lines.length && lines[index].includes('|')) {
      const separators = cells(lines[index + 1]),
        headers = cells(lines[index])
      if (
        separators.length === headers.length &&
        separators.every((cell) => /^:?-{3,}:?$/.test(cell))
      ) {
        index += 2
        const rows: string[][] = []
        while (index < lines.length && lines[index].trim() && lines[index].includes('|'))
          rows.push(cells(lines[index++]))
        markup(
          <div className="table-scroll" tabIndex={0}>
            <table>
              <thead>
                <tr>
                  {headers.map((cell, i) => (
                    <th key={i} scope="col">
                      {markdownInline(cell)}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map((row, i) => (
                  <tr key={i}>
                    {headers.map((_, j) => (
                      <td key={j}>{markdownInline(row[j] ?? '')}</td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>,
        )
        continue
      }
    }
    const paragraph = [lines[index++]]
    while (index < lines.length && lines[index].trim() && !startsBlock(lines[index])) {
      if (index + 1 < lines.length && /^ {0,3}(?:=+|-+)\s*$/.test(lines[index + 1])) break
      paragraph.push(lines[index++])
    }
    markup(<p>{markdownInline(paragraph.join('\n'))}</p>)
  }
  return result
}

function renderNested(blocks: MarkdownBlock[]) {
  return blocks.map((block, index) =>
    block.kind === 'code' ? (
      <pre key={index} dir="ltr">
        <code>{block.content}</code>
      </pre>
    ) : (
      <Fragment key={index}>{block.node}</Fragment>
    ),
  )
}
