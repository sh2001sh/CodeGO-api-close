import { api } from './api'

// Compile-time regression checks: the client must preserve schema boundaries.
if (false) {
  // @ts-expect-error unsupported URL
  void api.GET('/api/made-up')
  // @ts-expect-error method not exposed for wallet
  void api.POST('/api/wallet')
  // @ts-expect-error required path parameter is absent
  void api.DELETE('/api/token/{id}')
  void api.POST('/api/billing/adjustments', {
    // @ts-expect-error adjustment integer cannot be a boolean
    body: { account_id: 1, amount_micro: true, operation_id: 'op', reason: 'test' },
  })
  void api.POST('/api/user/login', {
    // @ts-expect-error unknown request payload property
    body: { username: 'test', password: 'password', role: 'root' },
  })
}
