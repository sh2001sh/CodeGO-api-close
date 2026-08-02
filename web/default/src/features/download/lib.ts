/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type {
  DesktopArchitecture,
  DesktopDevice,
  DesktopPlatform,
  DesktopRelease,
  DesktopReleaseAsset,
  DownloadCard,
  DownloadCopy,
  InstallationTrack,
  VerificationItem,
} from './types'

export const RELEASES_URL = '/api/desktop/release/latest'
export const RELEASE_PAGE_URL =
  'https://github.com/sh2001sh/CodeGO/releases/latest'

function compareAssetPreference(left: number[], right: number[]) {
  for (let index = 0; index < Math.max(left.length, right.length); index += 1) {
    const delta = (left[index] ?? 0) - (right[index] ?? 0)
    if (delta !== 0) return delta
  }
  return 0
}

function getAssetPreference(
  asset: DesktopReleaseAsset,
  platform: Exclude<DesktopPlatform, 'unknown'>
) {
  const name = asset.name.toLowerCase()
  const arch = (asset.arch || '').toLowerCase()
  const isArm64 = arch.includes('arm64') || arch.includes('aarch64')

  if (platform === 'windows') {
    return [
      isArm64 ? 1 : 0,
      name.endsWith('.msi') ? 0 : name.includes('portable') ? 1 : 2,
    ]
  }

  if (platform === 'macos') {
    return [
      name.endsWith('.dmg')
        ? 0
        : name.endsWith('.zip')
          ? 1
          : name.endsWith('.tar.gz')
            ? 2
            : 3,
    ]
  }

  return [
    isArm64 ? 1 : 0,
    name.endsWith('.appimage')
      ? 0
      : name.endsWith('.deb')
        ? 1
        : name.endsWith('.rpm')
          ? 2
          : 3,
  ]
}

function normalizeDesktopArchitecture(
  arch: string | undefined
): DesktopArchitecture {
  const normalized = (arch || '').toLowerCase()
  if (normalized.includes('arm64') || normalized.includes('aarch64')) {
    return 'arm64'
  }
  if (
    normalized.includes('x64') ||
    normalized.includes('x86_64') ||
    normalized.includes('amd64')
  ) {
    return 'x64'
  }
  if (normalized.includes('universal')) {
    return 'universal'
  }
  return 'unknown'
}

function pickPreferredAsset(
  assets: DesktopReleaseAsset[],
  platform: Exclude<DesktopPlatform, 'unknown'>
) {
  return [...assets].sort((left, right) => {
    const preferenceDelta = compareAssetPreference(
      getAssetPreference(left, platform),
      getAssetPreference(right, platform)
    )
    if (preferenceDelta !== 0) return preferenceDelta
    return left.name.localeCompare(right.name)
  })[0]
}

function findAsset(
  assets: DesktopReleaseAsset[],
  platform: Exclude<DesktopPlatform, 'unknown'>,
  fallbackKeyword: string
) {
  const platformMatches = assets.filter((asset) => asset.platform === platform)
  if (platformMatches.length > 0) {
    return pickPreferredAsset(platformMatches, platform)
  }

  const keywordMatches = assets.filter((asset) =>
    asset.name.includes(fallbackKeyword)
  )
  if (keywordMatches.length > 0) {
    return pickPreferredAsset(keywordMatches, platform)
  }

  return undefined
}

function findAssetByArchitecture(
  assets: DesktopReleaseAsset[],
  platform: Exclude<DesktopPlatform, 'unknown'>,
  arch: Exclude<DesktopArchitecture, 'unknown' | 'universal'>
) {
  return pickPreferredAsset(
    assets.filter(
      (asset) =>
        asset.platform === platform &&
        normalizeDesktopArchitecture(asset.arch) === arch
    ),
    platform
  )
}

/** Format release asset sizes for compact UI display. */
export function formatFileSize(size: number) {
  if (!Number.isFinite(size) || size <= 0) return '-'
  const units = ['B', 'KB', 'MB', 'GB']
  let value = size
  let index = 0
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024
    index += 1
  }
  return `${value.toFixed(value >= 100 || index === 0 ? 0 : 1)} ${units[index]}`
}

/** Infer the user's desktop platform from a browser user agent string. */
export function detectDesktopPlatform(userAgent: string): DesktopPlatform {
  const normalized = userAgent.toLowerCase()
  if (normalized.includes('windows')) return 'windows'
  if (
    normalized.includes('mac os') ||
    normalized.includes('macintosh') ||
    normalized.includes('darwin')
  ) {
    return 'macos'
  }
  if (normalized.includes('linux') || normalized.includes('x11')) return 'linux'
  return 'unknown'
}

/** Infer the user's desktop CPU architecture from a browser user agent string. */
export function detectDesktopArchitecture(
  userAgent: string,
  platform: DesktopPlatform
): DesktopArchitecture {
  const normalized = userAgent.toLowerCase()

  if (
    normalized.includes('arm64') ||
    normalized.includes('aarch64') ||
    normalized.includes('apple silicon')
  ) {
    return 'arm64'
  }

  if (
    normalized.includes('win64') ||
    normalized.includes('x64') ||
    normalized.includes('x86_64') ||
    normalized.includes('amd64')
  ) {
    return 'x64'
  }

  if (platform === 'macos' && normalized.includes('intel mac os x')) {
    return 'unknown'
  }

  if (normalized.includes('universal')) {
    return 'universal'
  }

  return 'unknown'
}

/** Map a desktop release payload into the download cards rendered by the page. */
export function buildDownloadCards(
  release: DesktopRelease | null | undefined,
  copy: DownloadCopy
): DownloadCard[] {
  const assets = release?.assets ?? []
  const releasePageURL = release?.html_url || RELEASE_PAGE_URL
  const windows = findAsset(assets, 'windows', '-Windows.msi')
  const macosUniversal = findAsset(assets, 'macos', '-macOS.dmg')
  const macosArm64 =
    findAssetByArchitecture(assets, 'macos', 'arm64') ?? macosUniversal
  const macosX64 =
    findAssetByArchitecture(assets, 'macos', 'x64') ?? macosUniversal
  const linux = findAsset(assets, 'linux', '-Linux-x86_64.AppImage')
  const macosFallbackURL = release?.homebrew_url || releasePageURL
  const windowsArch = normalizeDesktopArchitecture(windows?.arch)
  const linuxArch = normalizeDesktopArchitecture(linux?.arch)

  return [
    {
      key: 'windows',
      platform: 'windows',
      title: copy.windowsTitle,
      description: copy.windowsDescription,
      href: windows?.browser_download_url || releasePageURL,
      fileName: windows?.name,
      fileSize: windows?.size,
      digest: windows?.digest,
      arch: windowsArch === 'unknown' ? undefined : windowsArch,
      cta: windows ? copy.windowsCta : copy.releaseFallbackCta,
      fallback: !windows,
    },
    {
      key: 'macos-arm64',
      platform: 'macos',
      title: copy.macosAppleSiliconTitle,
      description: copy.macosAppleSiliconDescription,
      href: macosArm64?.browser_download_url || macosFallbackURL,
      fileName: macosArm64?.name,
      fileSize: macosArm64?.size,
      digest: macosArm64?.digest,
      arch: 'arm64',
      archLabel: copy.appleSiliconLabel,
      cta: macosArm64
        ? copy.macosAppleSiliconCta
        : release?.homebrew_url
          ? copy.macosFallbackCta
          : copy.releaseFallbackCta,
      fallback: !macosArm64,
    },
    {
      key: 'macos-x64',
      platform: 'macos',
      title: copy.macosIntelTitle,
      description: copy.macosIntelDescription,
      href: macosX64?.browser_download_url || macosFallbackURL,
      fileName: macosX64?.name,
      fileSize: macosX64?.size,
      digest: macosX64?.digest,
      arch: 'x64',
      archLabel: copy.intelLabel,
      cta: macosX64
        ? copy.macosIntelCta
        : release?.homebrew_url
          ? copy.macosFallbackCta
          : copy.releaseFallbackCta,
      fallback: !macosX64,
    },
    {
      key: 'linux',
      platform: 'linux',
      title: copy.linuxTitle,
      description: copy.linuxDescription,
      href: linux?.browser_download_url || releasePageURL,
      fileName: linux?.name,
      fileSize: linux?.size,
      digest: linux?.digest,
      arch: linuxArch === 'unknown' ? 'x64' : linuxArch,
      cta: linux ? copy.linuxCta : copy.linuxFallbackCta,
      fallback: !linux,
    },
  ]
}

/** Pick the most relevant download card for the detected platform. */
export function getRecommendedDownloadCard(
  cards: DownloadCard[],
  device: DesktopDevice
) {
  const platformCards = cards.filter((card) => card.platform === device.platform)
  if (platformCards.length === 0) return cards[0] ?? null

  const exactCard =
    device.arch === 'unknown'
      ? null
      : platformCards.find((card) => card.arch === device.arch)

  if (exactCard) return exactCard
  if (device.platform === 'macos' && platformCards.length > 1) return null
  return platformCards[0] ?? null
}

/** Build installation guidance tracks for each supported desktop platform. */
export function buildInstallationTracks(): InstallationTrack[] {
  return [
    {
      key: 'windows',
      title: 'Windows',
      steps: [
        {
          title: 'Run the MSI installer',
          description:
            'Open the downloaded `.msi`, finish the setup wizard, and allow the app to register its desktop protocol handler.',
        },
        {
          title: 'Authorize from your browser',
          description:
            'Launch Code Go Desktop, copy the device code if needed, and approve the request from the website instead of entering your password locally.',
        },
        {
          title: 'Apply tool configs',
          description:
            'Use the desktop dashboard to write Codex, Claude Code, Gemini CLI, OpenCode, OpenClaw, or Hermes settings with backup and restore support before restarting the target tool.',
        },
      ],
    },
    {
      key: 'macos',
      title: 'macOS',
      badge: 'DMG / Homebrew',
      steps: [
        {
          title: 'Install from DMG or Homebrew',
          description:
            'Mount the `.dmg` and move the app into Applications, or install the cask from Homebrew if you prefer a package-managed workflow.',
        },
        {
          title: 'Approve the desktop session',
          description:
            'Open Code Go Desktop, let it open the browser authorization page, and confirm the device request shown on the website.',
        },
        {
          title: 'Configure your local tools',
          description:
            'Use the desktop app to preview and apply Codex, Claude Code, Gemini CLI, OpenCode, OpenClaw, or Hermes config changes, then reopen the affected terminal session.',
        },
      ],
    },
    {
      key: 'linux',
      title: 'Linux',
      badge: 'AppImage / .deb / .rpm',
      steps: [
        {
          title: 'Pick the package that matches your distro',
          description:
            'Use the AppImage for the fastest start, or choose `.deb` / `.rpm` assets from the release page when you want a distro-native install path.',
        },
        {
          title: 'Make the binary executable',
          description:
            'If you install from AppImage, grant execute permission before launching so the desktop protocol and updater hooks can register correctly.',
        },
        {
          title: 'Finish browser auth and tool setup',
          description:
            'Authorize the device from the website and then let Code Go Desktop write the tool config you want to activate locally.',
        },
      ],
    },
  ]
}

/** Explain how users should verify what they are downloading and where it came from. */
export function buildVerificationItems(): VerificationItem[] {
  return [
    {
      title: 'Release source',
      description:
        'Code Go Desktop serves its release metadata and updater manifest from the current Code Go deployment, so the desktop app and the website resolve the same release channel.',
    },
    {
      title: 'Digest check',
      description:
        'When the release channel publishes a `sha256:` digest for an asset, compare it with the downloaded file before running the installer.',
    },
    {
      title: 'Fallback behavior',
      description:
        'If a platform-specific asset is missing, the page falls back to the configured release details page or optional Homebrew entry instead of inventing an unverifiable mirror.',
    },
  ]
}
