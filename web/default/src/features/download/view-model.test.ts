import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { RELEASE_PAGE_URL } from './lib.ts'
import type { DesktopRelease, DownloadCopy } from './types.ts'
import { buildDownloadPageViewModel } from './view-model.ts'

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
  tag_name: 'v3.16.4',
  version: '3.16.4',
  html_url: 'https://example.test/release',
  published_at: '2026-06-28T12:00:00Z',
  homebrew_url: 'https://example.test/homebrew',
  assets: [
    {
      name: 'CodeGo_3.16.4_x64_en-US.msi',
      size: 10485760,
      digest: 'sha256:windows-x64',
      browser_download_url: 'https://example.test/windows-x64.msi',
      platform: 'windows',
      arch: 'x64',
    },
    {
      name: 'CodeGo_3.16.4_arm64.dmg',
      size: 20971520,
      digest: 'sha256:macos-arm64',
      browser_download_url: 'https://example.test/macos-arm64.dmg',
      platform: 'macos',
      arch: 'arm64',
    },
    {
      name: 'CodeGo_3.16.4_x64.dmg',
      size: 20971520,
      digest: 'sha256:macos-x64',
      browser_download_url: 'https://example.test/macos-x64.dmg',
      platform: 'macos',
      arch: 'x64',
    },
    {
      name: 'CodeGo_3.16.4_x64.AppImage',
      size: 31457280,
      digest: 'sha256:linux-appimage',
      browser_download_url: 'https://example.test/linux.AppImage',
      platform: 'linux',
      arch: 'x64',
    },
  ],
}

describe('download page view model', () => {
  test('builds release-driven cards and platform recommendations for a resolved release', () => {
    const viewModel = buildDownloadPageViewModel({
      release,
      copy,
      device: { platform: 'macos', arch: 'arm64' },
      isLoading: false,
    })

    assert.equal(viewModel.currentBuildLabel, 'v3.16.4')
    assert.match(viewModel.publishedAtLabel, /^2026-06-28 /)
    assert.equal(viewModel.downloadCards.length, 4)
    assert.equal(viewModel.recommendedCard?.key, 'macos-arm64')
    assert.equal(
      viewModel.recommendedCard?.href,
      'https://example.test/macos-arm64.dmg'
    )
    assert.equal(viewModel.recommendedTrack?.key, 'macos')
    assert.equal(viewModel.verificationItems.length, 3)
    assert.equal(viewModel.errorMessage, null)
    assert.equal(viewModel.isLoading, false)
  })

  test('falls back conservatively when release data is missing or still loading', () => {
    const viewModel = buildDownloadPageViewModel({
      release: null,
      copy,
      device: { platform: 'unknown', arch: 'unknown' },
      isLoading: true,
      error: new Error('Desktop release channel unavailable'),
    })

    assert.equal(viewModel.currentBuildLabel, null)
    assert.equal(viewModel.publishedAtLabel, '-')
    assert.equal(viewModel.downloadCards[0]?.href, RELEASE_PAGE_URL)
    assert.equal(viewModel.downloadCards[1]?.href, RELEASE_PAGE_URL)
    assert.equal(viewModel.downloadCards[2]?.href, RELEASE_PAGE_URL)
    assert.equal(viewModel.downloadCards[3]?.href, RELEASE_PAGE_URL)
    assert.equal(viewModel.recommendedCard, null)
    assert.equal(viewModel.recommendedTrack?.key, 'windows')
    assert.equal(viewModel.errorMessage, 'Desktop release channel unavailable')
    assert.equal(viewModel.isLoading, true)
  })

  test('leaves mac recommendation unset when architecture cannot be inferred safely', () => {
    const viewModel = buildDownloadPageViewModel({
      release,
      copy,
      device: { platform: 'macos', arch: 'unknown' },
      isLoading: false,
    })

    assert.equal(viewModel.recommendedCard, null)
    assert.equal(viewModel.recommendedTrack?.key, 'macos')
  })
})
