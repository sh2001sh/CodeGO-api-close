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
import {
  buildDownloadCards,
  buildInstallationTracks,
  buildVerificationItems,
  getRecommendedDownloadCard,
} from './lib.ts'
import type {
  DesktopDevice,
  DesktopRelease,
  DownloadCard,
  DownloadCopy,
  InstallationTrack,
  VerificationItem,
} from './types.ts'

export type DownloadPageViewModel = {
  currentBuildLabel: string | null
  publishedAtLabel: string
  downloadCards: DownloadCard[]
  recommendedCard: DownloadCard | null
  installationTracks: InstallationTrack[]
  recommendedTrack: InstallationTrack | null
  verificationItems: VerificationItem[]
  errorMessage: string | null
  isLoading: boolean
}

type BuildDownloadPageViewModelArgs = {
  release?: DesktopRelease | null
  copy: DownloadCopy
  device: DesktopDevice
  isLoading: boolean
  error?: unknown
}

function formatPublishedAt(value?: string) {
  if (!value) return '-'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return '-'

  const pad = (input: number) => String(input).padStart(2, '0')

  return (
    [
      parsed.getFullYear(),
      pad(parsed.getMonth() + 1),
      pad(parsed.getDate()),
    ].join('-') +
    ` ${pad(parsed.getHours())}:${pad(parsed.getMinutes())}:${pad(
      parsed.getSeconds()
    )}`
  )
}

export function buildDownloadPageViewModel({
  release,
  copy,
  device,
  isLoading,
  error,
}: BuildDownloadPageViewModelArgs): DownloadPageViewModel {
  const downloadCards = buildDownloadCards(release, copy)
  const recommendedCard = release
    ? getRecommendedDownloadCard(downloadCards, device)
    : null
  const installationTracks = buildInstallationTracks()
  const recommendedTrack =
    installationTracks.find((track) => track.key === device.platform) ??
    installationTracks[0] ??
    null

  return {
    currentBuildLabel: release?.tag_name || null,
    publishedAtLabel: formatPublishedAt(release?.published_at),
    downloadCards,
    recommendedCard,
    installationTracks,
    recommendedTrack,
    verificationItems: buildVerificationItems(),
    errorMessage: error instanceof Error ? error.message : null,
    isLoading,
  }
}
