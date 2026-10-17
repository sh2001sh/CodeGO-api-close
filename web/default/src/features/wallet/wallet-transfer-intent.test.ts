import type { InternalAxiosRequestConfig } from 'axios'
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { test } from 'node:test'
import { api } from '@/lib/api'
import { getWalletTransfers } from './api'
import {
  restoreWalletTransferIntent,
  submitWalletTransferIntent,
  type WalletTransferIntent,
} from './wallet-transfer-intent'

const draft: WalletTransferIntent = {
  requestId: 'transfer-test-original',
  recipient: { external_id: 'ABC123', display_name_masked: 'A***' },
  amountQuota: 500_000,
  feeQuota: 5_000,
  quotaPerUSD: 500_000,
}

function memoryStorage() {
  const entries = new Map<string, string>()
  return {
    getItem: (key: string) => entries.get(key) ?? null,
    setItem: (key: string, value: string) => {
      entries.set(key, value)
    },
    removeItem: (key: string) => {
      entries.delete(key)
    },
  }
}

function response(config: InternalAxiosRequestConfig, data: unknown) {
  return { data, config, status: 200, statusText: 'OK', headers: {} }
}

function completed() {
  return {
    success: true,
    data: {
      request_id: draft.requestId,
      counterparty_external_id: draft.recipient.external_id,
      amount_quota: draft.amountQuota,
      balance_after: 0,
    },
  }
}

test('a lost response survives remount, password rejection and changed draft without duplicating the transfer', async () => {
  const storage = memoryStorage()
  const previousAdapter = api.defaults.adapter
  const requests: Record<string, unknown>[] = []
  let committed = 0
  api.defaults.adapter = async (config) => {
    const body = JSON.parse(config.data)
    requests.push(body)
    if (requests.length === 1) {
      committed++
      throw new Error('response lost after commit')
    }
    if (requests.length === 2) {
      return response(config, { success: false, message: 'Incorrect password' })
    }
    return response(config, completed())
  }
  try {
    assert.equal(
      (await submitWalletTransferIntent(1, draft, 'first', storage)).status,
      'unknown'
    )
    assert.deepEqual(restoreWalletTransferIntent(1, storage), draft)
    assert.equal(restoreWalletTransferIntent(2, storage), null)
    assert.doesNotMatch(
      storage.getItem('wallet-transfer-pending:1')!,
      /first|payment_password/
    )

    const changed = { ...draft, requestId: 'different', amountQuota: 1_000_000 }
    const rejected = await submitWalletTransferIntent(
      1,
      changed,
      'wrong',
      storage
    )
    assert.equal(rejected.status, 'rejected')
    assert.deepEqual(restoreWalletTransferIntent(1, storage), draft)
    const result = await submitWalletTransferIntent(
      1,
      changed,
      'correct',
      storage
    )
    assert.equal(result.status, 'completed')
    assert.equal(committed, 1)
    assert.equal(restoreWalletTransferIntent(1, storage), null)
    assert.deepEqual(
      requests.map(({ payment_password: _password, ...body }) => body),
      Array(3).fill({
        recipient_external_id: 'ABC123',
        amount_quota: 500_000,
        request_id: draft.requestId,
      })
    )
    assert.deepEqual(
      requests.map((body) => body.payment_password),
      ['first', 'wrong', 'correct']
    )
  } finally {
    api.defaults.adapter = previousAdapter
  }
})

test('a first definitive rejection releases the draft, while a malformed success preserves it', async () => {
  const storage = memoryStorage()
  const previousAdapter = api.defaults.adapter
  try {
    api.defaults.adapter = async (config) =>
      response(config, { success: false, message: 'Insufficient funds' })
    const rejected = await submitWalletTransferIntent(
      1,
      draft,
      'password',
      storage
    )
    assert.equal(rejected.status, 'rejected')
    assert.equal(restoreWalletTransferIntent(1, storage), null)
    api.defaults.adapter = async (config) =>
      response(config, { success: true, data: {} })
    assert.equal(
      (await submitWalletTransferIntent(1, draft, 'password', storage)).status,
      'unknown'
    )
    assert.deepEqual(restoreWalletTransferIntent(1, storage), draft)
  } finally {
    api.defaults.adapter = previousAdapter
  }
})

test('storage failure prevents sending and cannot turn a completed transfer into a failure', async () => {
  const previousAdapter = api.defaults.adapter
  let sent = 0
  api.defaults.adapter = async (config) => {
    sent++
    return response(config, completed())
  }
  try {
    const storage = memoryStorage()
    await assert.rejects(
      submitWalletTransferIntent(1, draft, 'password', {
        ...storage,
        setItem() {
          throw new Error('storage unavailable')
        },
      }),
      /storage unavailable/
    )
    assert.equal(sent, 0)
    const result = await submitWalletTransferIntent(1, draft, 'password', {
      ...storage,
      removeItem() {
        throw new Error('storage unavailable')
      },
    })
    assert.equal(result.status, 'completed')
    if (result.status === 'completed') assert.equal(result.storageError, true)
    assert.equal(sent, 1)
  } finally {
    api.defaults.adapter = previousAdapter
  }
})

test('a delayed completion from an unmounted form cannot erase a newer pending transfer', async () => {
  const previousAdapter = api.defaults.adapter
  const storage = memoryStorage()
  const newer = { ...draft, requestId: 'newer-transfer' }
  api.defaults.adapter = async (config) => {
    storage.setItem('wallet-transfer-pending:1', JSON.stringify(newer))
    return response(config, completed())
  }
  try {
    assert.equal(
      (await submitWalletTransferIntent(1, draft, 'password', storage)).status,
      'completed'
    )
    assert.deepEqual(restoreWalletTransferIntent(1, storage), newer)
  } finally {
    api.defaults.adapter = previousAdapter
  }
})

test(
  'a stalled overview and transfer actually time out and leave the transfer retryable',
  { timeout: 40_000 },
  async () => {
    const server = createServer(() => {})
    await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve))
    const address = server.address()
    assert.ok(address && typeof address !== 'string')
    const previousBaseURL = api.defaults.baseURL
    const previousAdapter = api.defaults.adapter
    api.defaults.baseURL = `http://127.0.0.1:${address.port}`
    api.defaults.adapter = 'http'
    const storage = memoryStorage()
    const start = Date.now()
    try {
      const [overview, transfer] = await Promise.allSettled([
        getWalletTransfers(),
        submitWalletTransferIntent(1, draft, 'password', storage),
      ])
      assert.equal(overview.status, 'rejected')
      if (overview.status === 'rejected')
        assert.match(String(overview.reason), /timeout of 15000ms/)
      assert.equal(transfer.status, 'fulfilled')
      if (transfer.status === 'fulfilled')
        assert.equal(transfer.value.status, 'unknown')
      assert.deepEqual(restoreWalletTransferIntent(1, storage), draft)
      assert.ok(Date.now() - start >= 29_000)
      assert.ok(Date.now() - start < 36_000)
    } finally {
      api.defaults.baseURL = previousBaseURL
      api.defaults.adapter = previousAdapter
      server.closeAllConnections()
      if (server.listening) {
        await new Promise<void>((resolve, reject) =>
          server.close((error) => (error ? reject(error) : resolve()))
        )
      }
    }
  }
)
