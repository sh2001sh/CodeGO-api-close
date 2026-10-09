/** Exact decimal display; scaled PPM can itself contain a fractional decimal. */
export function factorText(value: unknown, decimalPlaces = 0): string {
  if (typeof value !== 'string' && typeof value !== 'bigint' && typeof value !== 'number')
    return '—'
  if (
    typeof value === 'number' &&
    (!Number.isFinite(value) || (Number.isInteger(value) && !Number.isSafeInteger(value)))
  )
    return '—'
  const raw = String(value)
  // Bound work before expanding an exponent supplied by the server.
  if (raw.length > 4096) return '—'
  const match = /^(\d+)(?:\.(\d+))?(?:[eE]([+-]?\d+))?$/.exec(raw)
  if (!match) return '—'
  const exponent = Number(match[3] ?? 0)
  if (!Number.isSafeInteger(exponent) || Math.abs(exponent) > 4096) return '—'
  const fraction = match[2] ?? ''
  const digits = `${match[1]}${fraction}`
  if (!/[1-9]/.test(digits)) return '0'
  const point = match[1].length + exponent - decimalPlaces
  const whole = (point > 0 ? digits.slice(0, point).padEnd(point, '0') : '0').replace(
    /^0+(?=\d)/,
    '',
  )
  const decimals = (point < 0 ? '0'.repeat(-point) + digits : digits.slice(point)).replace(
    /0+$/,
    '',
  )
  return `${whole}${decimals ? `.${decimals}` : ''}`
}

export const ppmFactorText = (value: unknown): string => factorText(value, 6)
