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
export const DESKTOP_IMPORT_APP_CONFIGS = {
  claude: {
    label: 'Claude Code',
    defaultName: 'My Claude',
    modelFields: [
      { key: 'model', labelKey: 'Primary Model', required: true },
      { key: 'haikuModel', labelKey: 'Haiku Model', required: false },
      { key: 'sonnetModel', labelKey: 'Sonnet Model', required: false },
      { key: 'opusModel', labelKey: 'Opus Model', required: false },
    ],
  },
  codex: {
    label: 'Codex',
    defaultName: 'My Codex',
    modelFields: [{ key: 'model', labelKey: 'Primary Model', required: true }],
  },
  gemini: {
    label: 'Gemini CLI',
    defaultName: 'My Gemini',
    modelFields: [{ key: 'model', labelKey: 'Primary Model', required: true }],
  },
  opencode: {
    label: 'OpenCode',
    defaultName: 'My OpenCode',
    modelFields: [{ key: 'model', labelKey: 'Primary Model', required: true }],
  },
  openclaw: {
    label: 'OpenClaw',
    defaultName: 'My OpenClaw',
    modelFields: [{ key: 'model', labelKey: 'Primary Model', required: true }],
  },
  hermes: {
    label: 'Hermes',
    defaultName: 'My Hermes',
    modelFields: [{ key: 'model', labelKey: 'Primary Model', required: true }],
  },
} as const

export type DesktopImportApp = keyof typeof DESKTOP_IMPORT_APP_CONFIGS
