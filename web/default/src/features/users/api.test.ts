import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { test } from 'node:test'
import { api } from '@/lib/api'
import { searchUsers } from './api'

test('user search preserves IDs, literal special characters, groups and pagination over HTTP', async () => {
  const seen: URL[] = []
  const server = createServer((req, res) => {
    seen.push(new URL(req.url!, 'http://localhost'))
    res.setHeader('Content-Type', 'application/json')
    res.end(JSON.stringify({ success: true, data: { items: [], total: 0 } }))
  })
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve))
  const address = server.address()
  assert.ok(address && typeof address !== 'string')
  const previousURL = api.defaults.baseURL
  api.defaults.baseURL = `http://127.0.0.1:${address.port}`
  try {
    for (const keyword of [' abc123 ', 'alice+dev@example.test', 'R&D #100%_!']) {
      await searchUsers({ keyword, group: 'team&one+#', p: 2, page_size: 20 })
      const url = seen.at(-1)!
      assert.equal(url.pathname, '/api/user/search')
      assert.equal(url.searchParams.get('keyword'), keyword.trim())
      assert.equal(url.searchParams.get('group'), 'team&one+#')
      assert.equal(url.searchParams.get('p'), '2')
      assert.equal(url.searchParams.get('page_size'), '20')
    }
    assert.equal(seen.length, 3)
  } finally {
    api.defaults.baseURL = previousURL
    await new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve()))
  }
})
