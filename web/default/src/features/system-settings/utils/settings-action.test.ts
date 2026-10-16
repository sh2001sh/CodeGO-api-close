import assert from 'node:assert/strict'
import { afterEach, test } from 'node:test'
import { toast } from 'sonner'
import {
  handleSettingsAction,
  notifySettingsSaveError,
} from './settings-action'

const originalToastError = toast.error
afterEach(() => {
  toast.error = originalToastError
})

test('an already reported mutation failure resolves at the event boundary without a second toast', async () => {
  const messages: unknown[] = []
  toast.error = (message) => {
    messages.push(message)
    return 0
  }
  const failure = new Error('Setting rejected')
  let reachedSuccess = false
  const mutation = async () => {
    notifySettingsSaveError(failure)
    throw failure
  }
  const save = handleSettingsAction(async () => {
    await mutation()
    reachedSuccess = true
  })

  await save()
  assert.deepEqual(messages, ['Setting rejected'])
  assert.equal(reachedSuccess, false)
})

test('unexpected failures receive an error message and a subsequent successful attempt can complete', async () => {
  const messages: unknown[] = []
  toast.error = (message) => {
    messages.push(message)
    return 0
  }
  let fail = true
  let completed = false
  const save = handleSettingsAction(async () => {
    if (fail) throw new Error('Invalid configuration')
    completed = true
  })

  await save()
  assert.equal(completed, false)
  fail = false
  await save()
  assert.equal(completed, true)
  assert.deepEqual(messages, ['Invalid configuration'])
})
