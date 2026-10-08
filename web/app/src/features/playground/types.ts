// Shared types for the playground chat workstream.
export type ChatRole = 'system' | 'user' | 'assistant'

export interface ChatMessage {
  id: string
  role: ChatRole
  content: string
  reasoning: string
  status: 'streaming' | 'complete' | 'error'
  errorMessage?: string
}

export interface UsageInfo {
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
}

export interface ChatCompletionDelta {
  role?: ChatRole
  content?: string
  reasoning_content?: string
}

export interface ChatCompletionChunk {
  choices?: { delta?: ChatCompletionDelta; finish_reason?: string | null }[]
  usage?: UsageInfo
}

export interface PlaygroundSettings {
  group?: string
  model: string
  temperature: string
  maxTokens: string
  systemPrompt: string
}

export const defaultSettings: PlaygroundSettings = {
  group: '',
  model: '',
  temperature: '',
  maxTokens: '',
  systemPrompt: '',
}
