function isLocalHost(hostname: string): boolean {
  return ['localhost', '127.0.0.1', '0.0.0.0'].includes(hostname)
}

function getWindowOrigin(): string {
  if (typeof window === 'undefined') return ''
  return window.location.origin
}

function hasProtocol(value: string): boolean {
  return /^[a-z][a-z\d+\-.]*:\/\//i.test(value)
}

export function normalizePublicServerAddress(value?: string): string {
  const raw = (value || getWindowOrigin()).trim()
  let base = raw.replace(/\/+$/, '').replace(/\/v1$/i, '')
  if (base && !hasProtocol(base)) {
    base = `https://${base}`
  }

  try {
    const url = new URL(base)
    if (url.protocol === 'http:' && !isLocalHost(url.hostname)) {
      url.protocol = 'https:'
    }
    return url.toString().replace(/\/+$/, '')
  } catch {
    if (
      /^http:\/\//i.test(base) &&
      !/^http:\/\/(localhost|127\.0\.0\.1|0\.0\.0\.0)([:/]|$)/i.test(base)
    ) {
      return base.replace(/^http:\/\//i, 'https://')
    }
    return base
  }
}

export function getConfiguredServerAddress(fallback?: string): string {
  try {
    const raw = localStorage.getItem('status')
    if (raw) {
      const status = JSON.parse(raw) as { server_address?: string }
      if (status.server_address) {
        return normalizePublicServerAddress(status.server_address)
      }
    }
  } catch {
    /* empty */
  }

  return normalizePublicServerAddress(fallback || getWindowOrigin())
}
