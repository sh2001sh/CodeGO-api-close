export const clientTools = [
  { value: 'codex', label: 'Codex' },
  { value: 'claude', label: 'Claude Code' },
  { value: 'gemini', label: 'Gemini CLI' },
  { value: 'opencode', label: 'OpenCode' },
  { value: 'openclaw', label: 'OpenClaw' },
  { value: 'hermes', label: 'Hermes' },
] as const

export function ccSwitchImportURL(value: string): string {
  const link = new URL(value)
  if (
    link.protocol !== 'ccswitch:' ||
    link.hostname !== 'v1' ||
    link.pathname !== '/import' ||
    link.searchParams.get('resource') !== 'provider' ||
    !clientTools.some((tool) => tool.value === link.searchParams.get('app'))
  ) {
    throw new Error('导入链接无效')
  }
  return link.href
}
