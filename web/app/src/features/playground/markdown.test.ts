import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { MarkdownLite } from './markdown-lite'
import { markdownBlocks, safeMarkdownURL } from './markdown-parser'

const render = (content: string) => renderToStaticMarkup(createElement(MarkdownLite, { content }))

describe('readable and safe assistant Markdown', () => {
  it('renders headings, nested lists, quotes, table cells and inline emphasis', () => {
    const html = render(
      '# Results\n\n- **first**\n  - nested `code`\n- second\n\n> quote\n\n| Model | Price |\n| --- | ---: |\n| a\\|b | ~~old~~ |',
    )
    expect(html).toContain('<h2>Results</h2>')
    expect(html).toContain('<strong>first</strong>')
    expect(html.match(/<ul>/g)).toHaveLength(2)
    expect(html).toContain('<code>code</code>')
    expect(html).toContain('<blockquote>')
    expect(html).toContain('a|b')
    expect(html).toContain('<del>old</del>')
    expect(html).toContain('<th scope="col">Model</th>')
  })
  it('keeps a streaming unfinished fence readable and does not execute its content', () => {
    const blocks = markdownBlocks('Before\n\n```html\n<script>alert(1)</script>\nconst answer =')
    expect(blocks.at(-1)).toEqual({
      kind: 'code',
      language: 'html',
      content: '<script>alert(1)</script>\nconst answer =',
    })
    const html = render('```html\n<script>alert(1)</script>')
    expect(html).toContain('&lt;script&gt;')
    expect(html).not.toContain('<script>')
  })
  it('rejects script, data, protocol-relative, credential and obfuscated destinations', () => {
    for (const url of [
      'javascript:alert(1)',
      'data:text/html,x',
      '//attacker.test',
      'https://secret@example.test',
      'java\nscript:alert(1)',
      '\\attacker.test',
      'vbscript:x',
    ])
      expect(safeMarkdownURL(url)).toBeUndefined()
    const html = render(
      '<img src=x onerror=alert(1)>\n\n[unsafe](javascript:alert(1)) [safe](https://example.test/path) ![image](https://example.test/x.png)',
    )
    expect(html).not.toContain('<img')
    expect(html).not.toContain('href="javascript:')
    expect(html).toContain('rel="noopener noreferrer"')
    expect(html).toContain('href="https://example.test/path"')
    expect(html).toContain('&lt;img')
  })
  it('supports safe document anchors and escaped literal syntax', () => {
    expect(safeMarkdownURL('/docs?article=errors#429')).toBe('/docs?article=errors#429')
    expect(safeMarkdownURL('#section')).toBe('#section')
    expect(render('\\*literal\\* and ``a ` b``')).toContain('*literal* and <code>a ` b</code>')
  })
  it('handles malformed long bracket and fence runs without recursive expansion', () => {
    const value = '['.repeat(100_000) + 'x'
    expect(markdownBlocks(value)).toHaveLength(1)
    expect(markdownBlocks('`'.repeat(100_000))).toHaveLength(1)
    expect(markdownBlocks('[a]('.repeat(30_000))).toHaveLength(1)
    expect(markdownBlocks('<'.repeat(100_000))).toHaveLength(1)
    expect(markdownBlocks('['.repeat(10_000) + ']'.repeat(10_000))).toHaveLength(1)
  })
})
