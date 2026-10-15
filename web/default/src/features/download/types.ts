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
export type DesktopPlatform = 'windows' | 'macos' | 'linux' | 'unknown'
export type DesktopArchitecture = 'arm64' | 'x64' | 'universal' | 'unknown'
export type SupportedDesktopPlatform = Exclude<DesktopPlatform, 'unknown'>

export type DesktopDevice = {
  platform: DesktopPlatform
  arch: DesktopArchitecture
}

export type DesktopReleaseAsset = {
  name: string
  size: number
  digest?: string
  browser_download_url: string
  platform?: string
  arch?: string
  tauri_target?: string
}

export type DesktopRelease = {
  tag_name: string
  html_url: string
  version?: string
  published_at?: string
  notes?: string
  homebrew_url?: string
  assets: DesktopReleaseAsset[]
}

export type DownloadCard = {
  key: string
  platform: SupportedDesktopPlatform
  title: string
  description: string
  href: string
  fileName?: string
  fileSize?: number
  digest?: string
  arch?: Exclude<DesktopArchitecture, 'unknown'>
  archLabel?: string
  cta: string
  fallback: boolean
}

export type DownloadCopy = {
  windowsTitle: string
  windowsDescription: string
  windowsCta: string
  appleSiliconLabel: string
  intelLabel: string
  macosAppleSiliconTitle: string
  macosAppleSiliconDescription: string
  macosAppleSiliconCta: string
  macosIntelTitle: string
  macosIntelDescription: string
  macosIntelCta: string
  macosFallbackCta: string
  linuxTitle: string
  linuxDescription: string
  linuxCta: string
  linuxFallbackCta: string
  releaseFallbackCta: string
}

export type InstallationStep = {
  title: string
  description: string
}

export type InstallationTrack = {
  key: Exclude<DesktopPlatform, 'unknown'>
  title: string
  badge?: string
  steps: InstallationStep[]
}

export type VerificationItem = {
  title: string
  description: string
}
