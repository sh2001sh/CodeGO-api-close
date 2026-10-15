import type { AuthUser } from '@/stores/auth-store'

type AuthSessionResponse = {
  success?: boolean
  data?: AuthUser | null
}

/** Validate the server session before trusting a user restored from localStorage. */
export async function loadActiveAuthSession(
  getSession: () => Promise<AuthSessionResponse>
): Promise<AuthUser | null> {
  try {
    const response = await getSession()
    const userID = Number(response?.data?.id)
    if (!response?.success || !Number.isSafeInteger(userID) || userID <= 0) {
      return null
    }
    return response.data ?? null
  } catch {
    return null
  }
}
