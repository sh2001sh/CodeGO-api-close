export function safeLocalReturn(value: unknown): string | undefined {
  if (
    typeof value !== 'string' ||
    !value ||
    value.length > 4096 ||
    value !== value.trim() ||
    /[\u0000-\u001f\u007f\\]/.test(value)
  )
    return
  let path = value.split(/[?#]/, 1)[0]
  try {
    for (let depth = 0; depth < 8; depth++) {
      if (!path.startsWith('/') || path.startsWith('//') || /[\u0000-\u001f\u007f\\]/.test(path))
        return
      const decoded = decodeURIComponent(path)
      if (decoded === path) {
        const base = 'https://local.invalid'
        const target = new URL(value, base)
        return target.origin === base && !target.pathname.startsWith('//') ? value : undefined
      }
      path = decoded
    }
  } catch {
    return
  }
}

export function oauthStartURL(
  provider: string,
  input: { bind?: boolean; returnTo?: string } = {},
): string {
  const query = new URLSearchParams()
  if (input.bind) query.set('bind', 'true')
  const returnTo = safeLocalReturn(input.returnTo)
  if (returnTo) query.set('returnTo', returnTo)
  return `/api/oauth/${encodeURIComponent(provider)}${query.size ? `?${query}` : ''}`
}

export function oauthCallbackTarget(provider: unknown, search: string): string {
  if (
    typeof provider !== 'string' ||
    !provider ||
    provider.length > 200 ||
    provider === '.' ||
    provider === '..' ||
    /[/\\\u0000-\u001f\u007f]/.test(provider)
  )
    throw new Error('外部账号回调地址无效，请重新登录。')
  const query = new URLSearchParams(search)
  if (query.has('error')) throw new Error('外部账号未完成授权，请重新登录。')
  const selected = new URLSearchParams()
  for (const key of ['state', 'code']) {
    const values = query.getAll(key)
    if (
      values.length !== 1 ||
      !values[0] ||
      values[0].length > 8192 ||
      /[\u0000-\u001f\u007f]/.test(values[0])
    )
      throw new Error('外部账号回调信息无效，请重新登录。')
    selected.set(key, values[0])
  }
  return `/api/oauth/${encodeURIComponent(provider)}?${selected}`
}
