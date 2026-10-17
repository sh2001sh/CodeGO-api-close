import { AxiosError, type InternalAxiosRequestConfig } from 'axios'
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { test } from 'node:test'
import { api } from '@/lib/api'
import { getWalletTransfers } from './api'
import {
  restoreWalletTransferIntent,
  readWalletTransferIntent,
  pollWalletTransferIntent,
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
      fee_quota: draft.feeQuota,
      total_debit_quota: draft.amountQuota + draft.feeQuota,
      direction: 'outgoing',
      status: 'completed',
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

test('a rejection or malformed success preserves the original intent until a matching receipt', async () => {
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
    assert.deepEqual(restoreWalletTransferIntent(1, storage), draft)
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

test('safe server diagnostics survive HTTP errors without releasing the original intent', async () => {
  const previousAdapter = api.defaults.adapter
  const storage = memoryStorage()
  try {
    for (const failure of [
      {
        status: 503,
        code: 'TRANSFER_UNCONFIRMED',
        message: 'Retry the original transfer.',
      },
      {
        status: 429,
        code: 'PAYMENT_PASSWORD_LOCKED',
        message: 'Payment password temporarily locked.',
      },
    ]) {
      api.defaults.adapter = async (config) => {
        throw new AxiosError(
          'Request failed',
          'ERR_BAD_RESPONSE',
          config,
          null,
          {
            ...response(config, { success: false, ...failure }),
            status: failure.status,
          }
        )
      }
      const result = await submitWalletTransferIntent(
        1,
        failure.status === 503
          ? draft
          : { ...draft, requestId: 'new-draft-must-not-replace-pending' },
        'password',
        storage
      )
      assert.equal(result.status, 'unknown')
      if (result.status === 'unknown') {
        assert.equal(result.code, failure.code)
        assert.equal(result.message, failure.message)
      }
      assert.equal(
        restoreWalletTransferIntent(1, storage)?.requestId,
        draft.requestId
      )
    }
    for (const data of [
      { success: false, code: 'INTERNAL_ERROR', message: 'private SQL detail' },
      { success: true, code: 'TRANSFER_UNCONFIRMED', message: 'malformed' },
    ]) {
      api.defaults.adapter = async (config) => {
        throw new AxiosError(
          'Request failed',
          'ERR_BAD_RESPONSE',
          config,
          null,
          {
            ...response(config, data),
            status: 503,
          }
        )
      }
      const result = await submitWalletTransferIntent(
        1,
        draft,
        'password',
        storage
      )
      assert.equal(result.status, 'unknown')
      if (result.status === 'unknown') assert.equal(result.message, undefined)
      assert.ok(restoreWalletTransferIntent(1, storage))
    }
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

test('202, missing receipts, service errors and mismatched receipts preserve the exact original intent; GET alone completes it', async () => {
  const storage = memoryStorage()
  const previousAdapter = api.defaults.adapter
  const requests: { method?: string; url?: string }[] = []
  const controller = new AbortController()
  try {
    api.defaults.adapter = async (config) => {
      requests.push(config)
      return {
        ...response(config, {
          success: true,
          code: 'TRANSFER_PROCESSING',
          data: { status: 'processing', request_id: draft.requestId },
        }),
        status: 202,
      }
    }
    assert.equal(
      (await submitWalletTransferIntent(1, draft, 'password', storage)).status,
      'processing'
    )
    for (const status of [404, 503]) {
      api.defaults.adapter = async (config) => {
        requests.push(config)
        throw new AxiosError('Unavailable', 'ERR_BAD_RESPONSE', config, null, {
          ...response(config, {}),
          status,
        })
      }
      assert.equal(
        (await readWalletTransferIntent(1, draft, controller.signal, storage))
          .status,
        'unknown'
      )
      assert.deepEqual(restoreWalletTransferIntent(1, storage), draft)
    }
    for (const mismatch of [
      { request_id: 'other' },
      { amount_quota: 1 },
      { fee_quota: 1 },
      { total_debit_quota: 1 },
      { counterparty_external_id: 'OTHER1' },
      { direction: 'incoming' },
      { status: 'processing' },
      { balance_after: -1 },
    ]) {
      api.defaults.adapter = async (config) => {
        requests.push(config)
        return response(config, {
          ...completed(),
          data: { ...completed().data, ...mismatch },
        })
      }
      assert.equal(
        (await readWalletTransferIntent(1, draft, controller.signal, storage))
          .status,
        'unknown'
      )
      assert.deepEqual(restoreWalletTransferIntent(1, storage), draft)
    }
    for (const data of [
      { success: false, code: 'TRANSFER_UNCONFIRMED' },
      { success: false, code: 'INTERNAL_ERROR', message: 'private SQL detail' },
      {
        success: false,
        code: 'TRANSFER_REJECTED',
        data: { request_id: 'other', status: 'rejected' },
      },
    ]) {
      api.defaults.adapter = async (config) => response(config, data)
      const result = await readWalletTransferIntent(
        1,
        draft,
        controller.signal,
        storage
      )
      assert.equal(result.status, 'unknown')
      if (result.status === 'unknown') assert.equal(result.message, undefined)
      assert.deepEqual(restoreWalletTransferIntent(1, storage), draft)
    }
    for (const code of ['TRANSFER_REJECTED', 'PAYMENT_PASSWORD_LOCKED']) {
      api.defaults.adapter = async (config) =>
        response(config, {
          success: false,
          code,
          message: 'Safe rejection',
          data: { request_id: draft.requestId, status: 'rejected' },
        })
      const result = await readWalletTransferIntent(
        1,
        draft,
        controller.signal,
        storage
      )
      assert.equal(result.status, 'rejected')
      if (result.status === 'rejected')
        assert.equal(result.message, 'Safe rejection')
      assert.deepEqual(restoreWalletTransferIntent(1, storage), draft)
    }
    api.defaults.adapter = async (config) => {
      requests.push(config)
      assert.equal(config.timeout, 10_000)
      assert.equal(
        (config as unknown as Record<string, unknown>).disableDuplicate,
        true
      )
      return response(config, completed())
    }
    assert.equal(
      (await readWalletTransferIntent(1, draft, controller.signal, storage))
        .status,
      'completed'
    )
    assert.equal(restoreWalletTransferIntent(1, storage), null)
    assert.equal(
      requests.filter((request) => request.method === 'post').length,
      1
    )
    assert.ok(
      requests
        .slice(1)
        .every(
          (request) =>
            request.method === 'get' &&
            request.url === `/api/wallet/transfers/requests/${draft.requestId}`
        )
    )
  } finally {
    api.defaults.adapter = previousAdapter
  }
})

test('polling stops after its three minute bound without replaying POST or dropping intent', async () => {
  const storage = memoryStorage()
  storage.setItem('wallet-transfer-pending:1', JSON.stringify(draft))
  const previousAdapter = api.defaults.adapter
  const originalNow = Date.now
  let now = originalNow()
  Date.now = () => now
  let reads = 0
  try {
    api.defaults.adapter = async (config) => {
      assert.equal(config.method, 'get')
      reads++
      now += 180_001
      return response(config, {
        success: true,
        code: 'TRANSFER_UNCONFIRMED',
        data: { request_id: draft.requestId, status: 'unconfirmed' },
      })
    }
    await pollWalletTransferIntent(
      1,
      draft,
      new AbortController().signal,
      () => {},
      storage
    )
    assert.equal(reads, 1)
    assert.deepEqual(restoreWalletTransferIntent(1, storage), draft)
  } finally {
    Date.now = originalNow
    api.defaults.adapter = previousAdapter
  }
})

test('aborting a pending poll ignores a delayed completed receipt and preserves a newer intent', async () => {
  const storage = memoryStorage()
  storage.setItem('wallet-transfer-pending:1', JSON.stringify(draft))
  const previousAdapter = api.defaults.adapter
  const controller = new AbortController()
  let release!: () => void
  let started!: () => void
  const ready = new Promise<void>((resolve) => {
    started = resolve
  })
  const hold = new Promise<void>((resolve) => {
    release = resolve
  })
  let callbacks = 0
  try {
    api.defaults.adapter = async (config) => {
      started()
      await hold
      return response(config, completed())
    }
    const polling = pollWalletTransferIntent(
      1,
      draft,
      controller.signal,
      () => {
        callbacks++
      },
      storage
    )
    await ready
    controller.abort()
    const newer = { ...draft, requestId: 'newer-original' }
    storage.setItem('wallet-transfer-pending:1', JSON.stringify(newer))
    release()
    await polling
    assert.equal(callbacks, 0)
    assert.deepEqual(restoreWalletTransferIntent(1, storage), newer)
  } finally {
    release()
    api.defaults.adapter = previousAdapter
  }
})
