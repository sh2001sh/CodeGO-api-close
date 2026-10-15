import { describe, expect, it } from 'vitest'
import { ccSwitchImportURL } from './client-config'

describe('CC Switch import', () => {
  it('accepts provider imports emitted by the control API', () => {
    const link = 'ccswitch://v1/import?resource=provider&app=codex&name=CodeGo&apiKey=test'
    expect(ccSwitchImportURL(link)).toBe(link)
  })

  it.each([
    'javascript:alert(1)',
    'https://example.test/v1/import?resource=provider&app=codex',
    'codego://v1/import?resource=provider&app=codex',
    'ccswitch://other/import?resource=provider&app=codex',
    'ccswitch://v1/delete?resource=provider&app=codex',
    'ccswitch://v1/import?resource=unknown&app=codex',
    'ccswitch://v1/import?resource=provider&app=unknown',
  ])('rejects an invalid import target', (link) => {
    expect(() => ccSwitchImportURL(link)).toThrow('导入链接无效')
  })
})
