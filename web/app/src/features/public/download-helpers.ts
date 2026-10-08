// OS detection and byte formatting for the desktop download page.
export type DesktopPlatform = 'macos' | 'windows' | 'linux'

const platformLabel: Record<DesktopPlatform, string> = {
  macos: 'macOS',
  windows: 'Windows',
  linux: 'Linux',
}

export function platformName(platform: string): string {
  return platformLabel[platform as DesktopPlatform] ?? platform
}

/** Best-effort client OS guess from the UA string; falls back to macOS. */
export function detectPlatform(): DesktopPlatform {
  const ua = navigator.userAgent
  if (/windows/i.test(ua)) return 'windows'
  if (/linux/i.test(ua) && !/android/i.test(ua)) return 'linux'
  return 'macos'
}

export function formatBytes(size: number | string | bigint | undefined): string {
  if (size === undefined) return '—'
  const bytes = Number(size)
  if (!Number.isFinite(bytes) || bytes <= 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`
}
