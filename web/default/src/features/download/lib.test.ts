import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  buildInstallationTracks,
  buildDownloadCards,
  buildVerificationItems,
  detectDesktopArchitecture,
  detectDesktopPlatform,
  formatFileSize,
  getRecommendedDownloadCard,
} from './lib.ts'
import type { DesktopRelease, DownloadCopy } from './types.ts'

const copy: DownloadCopy = {
  windowsTitle: 'Windows',
  windowsDescription: 'windows-desc',
  windowsCta: 'Download for Windows',
  appleSiliconLabel: 'Apple Silicon',
  intelLabel: 'Intel',
  macosAppleSiliconTitle: 'macOS Apple Silicon',
  macosAppleSiliconDescription: 'mac-arm-desc',
  macosAppleSiliconCta: 'Download for Apple Silicon',
  macosIntelTitle: 'macOS Intel',
  macosIntelDescription: 'mac-intel-desc',
  macosIntelCta: 'Download for Intel Mac',
  macosFallbackCta: 'Install with Homebrew',
  linuxTitle: 'Linux',
  linuxDescription: 'linux-desc',
  linuxCta: 'Download for Linux',
  linuxFallbackCta: 'Browse Linux assets',
  releaseFallbackCta: 'Open release',
}

const release: DesktopRelease = {
  tag_name: 'v1.2.3',
  html_url: 'https://example.test/release',
  homebrew_url: 'https://example.test/homebrew',
  assets: [
    {
      name: 'CodeGo_1.2.3_arm64_en-US.msi',
      size: 7340032,
      digest: 'sha256:windows-arm64',
      browser_download_url: 'https://example.test/windows-arm64.msi',
      platform: 'windows',
      arch: 'arm64',
    },
    {
      name: 'CodeGo_1.2.3_x64_portable.zip',
      size: 6291456,
      digest: 'sha256:windows-portable',
      browser_download_url: 'https://example.test/windows-portable.zip',
      platform: 'windows',
      arch: 'x64',
    },
    {
      name: 'CodeGo_1.2.3_x64_en-US.msi',
      size: 10485760,
      digest: 'sha256:windows-x64',
      browser_download_url: 'https://example.test/windows-x64.msi',
      platform: 'windows',
      arch: 'x64',
    },
    {
      name: 'CodeGo_1.2.3_arm64.dmg',
      size: 18874368,
      digest: 'sha256:macos-arm64',
      browser_download_url: 'https://example.test/macos-arm64.dmg',
      platform: 'macos',
      arch: 'arm64',
    },
    {
      name: 'CodeGo_1.2.3_x64.dmg',
      size: 20971520,
      digest: 'sha256:macos-x64',
      browser_download_url: 'https://example.test/macos-x64.dmg',
      platform: 'macos',
      arch: 'x64',
    },
    {
      name: 'CodeGo_1.2.3_arm64.AppImage',
      size: 23068672,
      digest: 'sha256:linux-arm64',
      browser_download_url: 'https://example.test/linux-arm64.AppImage',
      platform: 'linux',
      arch: 'arm64',
    },
    {
      name: 'CodeGo_1.2.3_x64.deb',
      size: 26214400,
      digest: 'sha256:linux-deb',
      browser_download_url: 'https://example.test/linux.deb',
      platform: 'linux',
      arch: 'x64',
    },
    {
      name: 'CodeGo_1.2.3_x64.AppImage',
      size: 31457280,
      digest: 'sha256:linux-appimage',
      browser_download_url: 'https://example.test/linux.AppImage',
      platform: 'linux',
      arch: 'x64',
    },
  ],
}

describe('download lib', () => {
  test('detectDesktopPlatform resolves common user agents', () => {
    assert.equal(
      detectDesktopPlatform(
        'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36'
      ),
      'windows'
    )
    assert.equal(
      detectDesktopPlatform(
        'Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/605.1.15'
      ),
      'macos'
    )
    assert.equal(
      detectDesktopPlatform(
        'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36'
      ),
      'linux'
    )
    assert.equal(detectDesktopPlatform('curl/8.0.1'), 'unknown')
  })

  test('detectDesktopArchitecture keeps ambiguous mac user agents conservative', () => {
    assert.equal(
      detectDesktopArchitecture(
        'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36',
        'windows'
      ),
      'x64'
    )
    assert.equal(
      detectDesktopArchitecture(
        'Mozilla/5.0 (Macintosh; Apple Silicon Mac OS X 14_5) AppleWebKit/605.1.15',
        'macos'
      ),
      'arm64'
    )
    assert.equal(
      detectDesktopArchitecture(
        'Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/605.1.15',
        'macos'
      ),
      'unknown'
    )
  })

  test('buildDownloadCards binds release assets when present', () => {
    const cards = buildDownloadCards(release, copy)

    assert.equal(cards[0]?.href, 'https://example.test/windows-x64.msi')
    assert.equal(cards[1]?.href, 'https://example.test/macos-arm64.dmg')
    assert.equal(cards[2]?.href, 'https://example.test/macos-x64.dmg')
    assert.equal(cards[3]?.href, 'https://example.test/linux.AppImage')
    assert.equal(cards[0]?.fileSize, 10485760)
    assert.equal(cards[3]?.digest, 'sha256:linux-appimage')
    assert.equal(cards[1]?.arch, 'arm64')
    assert.equal(cards[2]?.archLabel, 'Intel')
    assert.equal(cards[0]?.fallback, false)
  })

  test('buildDownloadCards falls back to release and Homebrew links when assets are missing', () => {
    const partialCards = buildDownloadCards(
      {
        ...release,
        assets: [],
      },
      copy
    )

    assert.equal(partialCards[0]?.href, 'https://example.test/release')
    assert.equal(partialCards[1]?.href, 'https://example.test/homebrew')
    assert.equal(partialCards[2]?.href, 'https://example.test/homebrew')
    assert.equal(partialCards[3]?.href, 'https://example.test/release')
    assert.equal(partialCards[0]?.fallback, true)
    assert.equal(partialCards[1]?.cta, 'Install with Homebrew')
  })

  test('getRecommendedDownloadCard prefers the detected platform and falls back safely', () => {
    const cards = buildDownloadCards(release, copy)

    assert.equal(
      getRecommendedDownloadCard(cards, { platform: 'linux', arch: 'x64' })?.key,
      'linux'
    )
    assert.equal(
      getRecommendedDownloadCard(cards, { platform: 'macos', arch: 'arm64' })?.key,
      'macos-arm64'
    )
    assert.equal(
      getRecommendedDownloadCard(cards, { platform: 'macos', arch: 'unknown' }),
      null
    )
    assert.equal(
      getRecommendedDownloadCard(cards, { platform: 'unknown', arch: 'unknown' })?.key,
      'windows'
    )
    assert.equal(
      getRecommendedDownloadCard([], { platform: 'windows', arch: 'x64' }),
      null
    )
  })

  test('formatFileSize returns compact human-readable output', () => {
    assert.equal(formatFileSize(0), '-')
    assert.equal(formatFileSize(1024), '1.0 KB')
    assert.equal(formatFileSize(10485760), '10.0 MB')
  })

  test('buildInstallationTracks returns guided steps for every supported platform', () => {
    const tracks = buildInstallationTracks()

    assert.equal(tracks.length, 3)
    assert.deepEqual(
      tracks.map((track) => track.key),
      ['windows', 'macos', 'linux']
    )
    assert.ok(tracks.every((track) => track.steps.length === 3))
    assert.match(
      tracks[0]?.steps[2]?.description || '',
      /OpenCode, OpenClaw, or Hermes/
    )
    assert.match(
      tracks[1]?.steps[2]?.description || '',
      /OpenCode, OpenClaw, or Hermes/
    )
  })

  test('buildVerificationItems explains source and integrity expectations', () => {
    const items = buildVerificationItems()

    assert.equal(items.length, 3)
    assert.match(items[0]?.description || '', /Code Go/i)
    assert.match(items[1]?.description || '', /sha256/i)
  })
})
