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
import { getDesktopAuthorizationStatus } from './-authorize-state.ts'

export interface DesktopAuthorizeSession {
  session_id: string
  user_code: string
  device_name: string
  platform: string
  app_version: string
  status: string
  created_at: number
  approved_at: number
  expires_at: number
  permissions: string[]
}

export interface DesktopAuthorizeViewModel {
  canReview: boolean
  errorMessage: string | null
  noticeKey: string | null
  noticeTone: 'danger' | 'none' | 'success'
  platformLabel: string
  primaryActionKey: string | null
  secondaryActionKey: string | null
  status: ReturnType<typeof getDesktopAuthorizationStatus>
  titleKey: string
}

/**
 * Formats the desktop platform row shown on the authorization page.
 */
export function formatDesktopAuthorizePlatform(
  session:
    | Pick<DesktopAuthorizeSession, 'platform' | 'app_version'>
    | null
    | undefined,
  fallback = 'Unknown'
): string {
  if (!session) {
    return fallback
  }

  const label = [session.platform, session.app_version]
    .filter(Boolean)
    .join(' · ')
  return label || fallback
}

/**
 * Builds the route-level view model for the desktop authorization page.
 */
export function buildDesktopAuthorizeViewModel(input: {
  error?: Error | null
  isLoading: boolean
  session?: DesktopAuthorizeSession | null
}): DesktopAuthorizeViewModel {
  if (input.isLoading) {
    return {
      canReview: false,
      errorMessage: null,
      noticeKey: null,
      noticeTone: 'none',
      platformLabel: 'Unknown',
      primaryActionKey: null,
      secondaryActionKey: null,
      status: 'pending',
      titleKey: 'Authorization request',
    }
  }

  if (input.error) {
    return {
      canReview: false,
      errorMessage:
        input.error.message || 'Desktop authorization session not found',
      noticeKey: null,
      noticeTone: 'none',
      platformLabel: 'Unknown',
      primaryActionKey: null,
      secondaryActionKey: null,
      status: 'unknown',
      titleKey: 'Authorization request',
    }
  }

  const status = getDesktopAuthorizationStatus(input.session?.status)
  if (status === 'approved') {
    return {
      canReview: false,
      errorMessage: null,
      noticeKey:
        'This desktop can now access your Code Go account. You can revoke it later from your profile security settings.',
      noticeTone: 'success',
      platformLabel: formatDesktopAuthorizePlatform(input.session),
      primaryActionKey: null,
      secondaryActionKey: null,
      status,
      titleKey: 'Desktop approved',
    }
  }

  if (status === 'rejected') {
    return {
      canReview: false,
      errorMessage: null,
      noticeKey:
        'This desktop request was rejected. Return to the desktop app if you want to start a new authorization session.',
      noticeTone: 'danger',
      platformLabel: formatDesktopAuthorizePlatform(input.session),
      primaryActionKey: null,
      secondaryActionKey: null,
      status,
      titleKey: 'Authorization request',
    }
  }

  if (status === 'expired') {
    return {
      canReview: false,
      errorMessage: null,
      noticeKey:
        'This authorization request has expired. Return to the desktop app and start again.',
      noticeTone: 'danger',
      platformLabel: formatDesktopAuthorizePlatform(input.session),
      primaryActionKey: null,
      secondaryActionKey: null,
      status,
      titleKey: 'Session expired',
    }
  }

  if (status === 'unknown') {
    return {
      canReview: false,
      errorMessage: null,
      noticeKey:
        'This desktop request is in an unknown state. Return to the desktop app and start a new authorization session.',
      noticeTone: 'danger',
      platformLabel: formatDesktopAuthorizePlatform(input.session),
      primaryActionKey: null,
      secondaryActionKey: null,
      status,
      titleKey: 'Authorization request',
    }
  }

  return {
    canReview: true,
    errorMessage: null,
    noticeKey: null,
    noticeTone: 'none',
    platformLabel: formatDesktopAuthorizePlatform(input.session),
    primaryActionKey: 'Approve desktop',
    secondaryActionKey: 'Reject desktop',
    status,
    titleKey: 'Authorization request',
  }
}
