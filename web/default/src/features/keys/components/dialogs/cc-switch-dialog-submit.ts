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
  DesktopImportLinkRequest,
  DesktopImportLinkResponse,
} from '../../api'
import type { ApiResponse } from '../../types'
import type { DesktopImportApp } from './cc-switch-dialog-config'
import type { WindowLike } from './cc-switch-dialog-open'

export type DesktopImportSubmitResult =
  | {
      message: string
      tone: 'error' | 'warning'
    }
  | {
      tone: 'success'
    }

export interface DesktopImportSubmitInput {
  app: DesktopImportApp
  tokenId: number | null
  name: string
  models: Record<string, string>
  target: 'codego' | 'ccswitch'
}

export interface DesktopImportSubmitDependencies {
  createDesktopImportLink: (
    data: DesktopImportLinkRequest
  ) => Promise<ApiResponse<DesktopImportLinkResponse>>
  openDesktopImportDeepLink: (windowLike: WindowLike, deepLink: string) => void
  t: (key: string) => string
  windowLike: WindowLike
}

/**
 * Validates the website-side desktop import request and launches the deep link.
 */
export async function submitDesktopImportRequest(
  input: DesktopImportSubmitInput,
  dependencies: DesktopImportSubmitDependencies
): Promise<DesktopImportSubmitResult> {
  const openErrorMessage =
    input.target === 'ccswitch'
      ? dependencies.t('Failed to open CC Switch')
      : dependencies.t('Failed to open Code Go Desktop')

  if (!input.models.model) {
    return {
      tone: 'warning',
      message: dependencies.t('Please select a primary model'),
    }
  }

  if (!input.tokenId) {
    return {
      tone: 'error',
      message: dependencies.t('Token not found'),
    }
  }

  try {
    const result = await dependencies.createDesktopImportLink({
      target: input.target,
      tool: input.app,
      token_id: input.tokenId,
      name: input.name,
      model: input.models.model,
      haiku_model: input.models.haikuModel || undefined,
      sonnet_model: input.models.sonnetModel || undefined,
      opus_model: input.models.opusModel || undefined,
      enabled: true,
    })

    if (!result.success || !result.data?.deep_link) {
      return {
        tone: 'error',
        message: result.message || openErrorMessage,
      }
    }

    dependencies.openDesktopImportDeepLink(
      dependencies.windowLike,
      result.data.deep_link
    )

    return {
      tone: 'success',
    }
  } catch {
    return {
      tone: 'error',
      message: openErrorMessage,
    }
  }
}
