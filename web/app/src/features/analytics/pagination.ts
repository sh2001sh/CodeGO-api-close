// Cursor-stack pagination shared by every /api/log and /api/audit list view.
// The API only exposes next_cursor (no previous), so going back means
// replaying the stack of cursors already visited.
import { useState } from 'react'

export function useCursorPager() {
  const [cursors, setCursors] = useState<string[]>([''])
  const cursor = cursors.at(-1) ?? ''
  const atStart = cursors.length <= 1
  const goNext = (next: string) => {
    if (next) setCursors([...cursors, next])
  }
  const goBack = () => setCursors(cursors.slice(0, -1))
  const reset = () => setCursors([''])
  return { cursor, atStart, goNext, goBack, reset }
}
